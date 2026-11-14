package server

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
)

// static serves the embedded UI. Text assets are gzip-compressed once and
// kept in memory (the embedded files never change), hashed /assets/ files
// are cached by browsers for a year, and unknown paths fall back to
// index.html for client-side routing.
type static struct {
	ui    fs.FS
	files http.Handler

	mu   sync.Mutex
	gzip map[string][]byte
}

func newStatic(ui fs.FS) *static {
	return &static{ui: ui, files: http.FileServerFS(ui), gzip: map[string][]byte{}}
}

func compressible(name string) bool {
	switch path.Ext(name) {
	case ".js", ".css", ".html", ".svg", ".json", ".txt", ".map":
		return true
	}
	return false
}

func (s *static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if fi, err := fs.Stat(s.ui, name); err != nil || fi.IsDir() {
		name = "index.html" // client-side route
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache") // index.html must pick up new asset names
	}
	w.Header().Add("Vary", "Accept-Encoding")

	if compressible(name) && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		if body, err := s.gzipped(name); err == nil {
			ct := mime.TypeByExtension(path.Ext(name))
			if ct == "" {
				ct = "application/octet-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Content-Encoding", "gzip")
			if r.Method == http.MethodHead {
				return
			}
			w.Write(body)
			return
		}
	}
	r = r.Clone(r.Context())
	r.URL.Path = "/" + name
	if name == "index.html" {
		r.URL.Path = "/" // FileServer redirects /index.html to /
	}
	s.files.ServeHTTP(w, r)
}

func (s *static) gzipped(name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.gzip[name]; ok {
		return b, nil
	}
	raw, err := fs.ReadFile(s.ui, name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(raw)
	zw.Close()
	s.gzip[name] = buf.Bytes()
	return s.gzip[name], nil
}
