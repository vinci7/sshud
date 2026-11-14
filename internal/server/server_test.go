package server

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/store"
)

func testUI() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<title>sshud</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

func TestMeta(t *testing.T) {
	h := New(Options{Hosts: testHosts, Demo: true, Version: "test", UI: testUI()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Version string
		Demo    bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Demo || got.Version != "test" {
		t.Fatalf("got %+v", got)
	}
}

func TestSPAFallback(t *testing.T) {
	h := New(Options{Hosts: testHosts, UI: testUI()})
	for _, path := range []string{"/", "/machines/abc", "/assets/app.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/api/nope: status = %d", rec.Code)
	}
}

func TestStream(t *testing.T) {
	h := hub.New(stubSource{})
	srv := httptest.NewServer(New(Options{Hosts: testHosts, UI: testUI(), Hub: h, Machines: &fakeAdmin{list: []store.Machine{{ID: "a", Name: "a"}, {ID: "b", Name: "b"}}}}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/stream?level=detail&id=b")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %s", ct)
	}
	sc := bufio.NewScanner(res.Body)
	var got []hub.Update
	for sc.Scan() && len(got) < 2 {
		line := sc.Text()
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var u hub.Update
			if err := json.Unmarshal([]byte(data), &u); err != nil {
				t.Fatal(err)
			}
			got = append(got, u)
		}
	}
	if len(got) != 2 || got[0].Status != hub.StatusConnecting || got[1].Status != hub.StatusOK || got[1].Machine != "b" {
		t.Errorf("updates = %+v", got)
	}

	res2, _ := http.Get(srv.URL + "/api/stream?id=nope")
	if res2.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id status = %d", res2.StatusCode)
	}
	res2.Body.Close()
}

type stubSource struct{}

func (stubSource) Probe(ctx context.Context, id string, level hub.Level) (string, error) {
	return "@@hostname\n" + id + "\n", nil
}

func TestStaticGzipAndCaching(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":        {Data: []byte("<title>sshud</title>")},
		"assets/app-1.js":   {Data: []byte(strings.Repeat("console.log(1);", 200))},
		"assets/font.woff2": {Data: []byte("binary")},
	}
	h := New(Options{Hosts: testHosts, UI: ui})
	r := httptest.NewRequest("GET", "/assets/app-1.js", nil)
	r.Header.Set("Accept-Encoding", "gzip, br")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Content-Type"), "javascript") || w.Body.Len() >= 3000 {
		t.Errorf("js: enc=%q type=%q len=%d", w.Header().Get("Content-Encoding"), w.Header().Get("Content-Type"), w.Body.Len())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); len(b) != 3000 {
		t.Errorf("decompressed %d bytes", len(b))
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("assets cache = %q", w.Header().Get("Cache-Control"))
	}
	// fonts are already compressed; index.html must revalidate
	r = httptest.NewRequest("GET", "/assets/font.woff2", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") != "" {
		t.Error("woff2 should not be gzipped")
	}
	r = httptest.NewRequest("GET", "/m/abc", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(w.Body.String(), "<title>") {
		t.Errorf("route: %d %q %q", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
	}
}
