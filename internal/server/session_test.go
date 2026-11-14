package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinci7/sshud/internal/auth"
)

type memSettings map[string]string

func (m memSettings) Setting(k string) (string, error) { return m[k], nil }
func (m memSettings) SetSetting(k, v string) error     { m[k] = v; return nil }

func TestLoginGuardsTheAPI(t *testing.T) {
	a := auth.New(memSettings{})
	h := New(Options{Hosts: testHosts, UI: testUI(), Admin: &fakeAdmin{}, Auth: a, Machines: &fakeAdmin{}})

	if rec := do(t, h, "GET", "/api/meta", ""); !strings.Contains(rec.Body.String(), `"setup":true`) || !strings.Contains(rec.Body.String(), `"loggedIn":false`) {
		t.Errorf("meta before setup: %s", rec.Body)
	}
	for _, p := range []string{"/api/machines", "/api/admin/machines", "/api/stream"} {
		if rec := do(t, h, "GET", p, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without login: %d", p, rec.Code)
		}
	}
	if rec := do(t, h, "POST", "/api/admin/machines/m1/trust", `{"fingerprint":"SHA256:new"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("trust without login: %d", rec.Code)
	}
	// the UI itself stays reachable so it can show the login page
	if rec := do(t, h, "GET", "/machines", ""); rec.Code != http.StatusOK {
		t.Errorf("UI: %d", rec.Code)
	}

	rec := do(t, h, "POST", "/api/setup", `{"password":"correct horse"}`)
	cookie := rec.Result().Cookies()
	if rec.Code != http.StatusNoContent || len(cookie) != 1 {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "POST", "/api/setup", `{"password":"hijacked!!"}`); rec.Code != http.StatusForbidden {
		t.Errorf("second setup allowed: %d", rec.Code)
	}

	r := httptest.NewRequest("GET", "/api/admin/machines", nil)
	r.AddCookie(cookie[0])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("with session: %d", w.Code)
	}

	if rec := do(t, h, "POST", "/api/login", `{"password":"nope"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad login: %d", rec.Code)
	}

	// a cross-site form post is refused even with a valid cookie
	r = httptest.NewRequest("POST", "/api/admin/machines", strings.NewReader(`{}`))
	r.Host = "board.example"
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(cookie[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-origin write: %d", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := New(Options{Hosts: testHosts, UI: testUI()})
	for _, p := range []string{"/", "/api/meta"} {
		rec := do(t, h, "GET", p, "")
		if rec.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s headers = %v", p, rec.Header())
		}
	}
	if rec := do(t, h, "GET", "/api/meta", ""); rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("API responses must not be cached")
	}
}

// testHosts are the host names the tests' requests use.
var testHosts = []string{"example.com", "board.example"}

func TestHostCheck(t *testing.T) {
	h := New(Options{UI: testUI(), Demo: true, Hosts: []string{"Dash.Home.Example"}, Machines: &fakeAdmin{}})
	for host, want := range map[string]int{
		"localhost":              http.StatusOK,
		"localhost:8080":         http.StatusOK,
		"app.localhost":          http.StatusOK,
		"127.0.0.1:8080":         http.StatusOK,
		"192.0.2.10":             http.StatusOK,
		"[::1]:8080":             http.StatusOK,
		"[2001:db8::1]":          http.StatusOK,
		"dash.home.example":      http.StatusOK,
		"DASH.home.example:443":  http.StatusOK,
		"evil.example":           http.StatusMisdirectedRequest,
		"evil.example:8080":      http.StatusMisdirectedRequest,
		"dash.home.example.evil": http.StatusMisdirectedRequest,
		"localhost.evil.example": http.StatusMisdirectedRequest,
		"":                       http.StatusMisdirectedRequest,
	} {
		for _, p := range []string{"/", "/api/meta", "/api/machines"} {
			r := httptest.NewRequest("GET", p, nil)
			r.Host = host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != want {
				t.Errorf("Host %q %s: %d, want %d", host, p, rec.Code, want)
			}
			if want != http.StatusOK && !strings.Contains(rec.Body.String(), "SSHUD_HOSTS") {
				t.Errorf("Host %q: unhelpful error %q", host, rec.Body)
			}
		}
	}
}
