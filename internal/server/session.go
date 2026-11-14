package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/vinci7/sshud/internal/auth"
)

// secure reports whether the browser reached us over HTTPS, directly or
// through a reverse proxy, so cookies get the Secure flag.
func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// checkHost answers only to localhost, IP addresses and the names listed in
// SSHUD_HOSTS. This stops DNS rebinding: a page on evil.example whose name is
// re-pointed at sshud's address is same-origin with itself, so cookies and
// Origin checks do not help, but its requests still say Host: evil.example.
func checkHost(names []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost(r.Host, names) {
			http.Error(w, "sshud does not answer to the host name "+r.Host+"; add it to SSHUD_HOSTS (or --hosts) if this is how you reach it", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedHost(host string, names []string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(host) != nil ||
		slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, host) })
}

// sameOrigin rejects cross-site writes. SameSite=Strict cookies already stop
// them in current browsers; checking Origin covers older ones.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true // same-origin fetches from some browsers, curl
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

// guardAPI requires a session for every /api/ path except the login ones.
func guardAPI(a *auth.Auth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != http.MethodGet && !sameOrigin(r) {
				errorJSON(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			public := r.URL.Path == "/api/meta" || r.URL.Path == "/api/login" || r.URL.Path == "/api/logout" || r.URL.Path == "/api/setup"
			if a != nil && !public && !a.Valid(r) {
				errorJSON(w, http.StatusUnauthorized, "login required")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func registerSession(mux *http.ServeMux, a *auth.Auth) {
	// login handles both /api/login and, while no password exists, /api/setup
	login := func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if r.URL.Path == "/api/setup" {
			if need, err := a.NeedsSetup(); err != nil || !need {
				errorJSON(w, http.StatusForbidden, "a password is already set")
				return
			}
			if err := a.SetPassword(body.Password); err != nil {
				errorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		c, err := a.Login(body.Password, secure(r))
		if err != nil {
			errorJSON(w, http.StatusUnauthorized, err.Error())
			return
		}
		http.SetCookie(w, c)
		w.WriteHeader(http.StatusNoContent)
	}
	mux.HandleFunc("POST /api/login", login)
	mux.HandleFunc("POST /api/setup", login)
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, auth.Logout(secure(r)))
		w.WriteHeader(http.StatusNoContent)
	})
}

// csp allows only our own origin. Carbon and Carbon Charts set inline style
// attributes, so styles also allow 'unsafe-inline'; scripts never do.
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// securityHeaders is applied to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
