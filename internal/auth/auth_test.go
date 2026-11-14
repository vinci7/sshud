package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mem map[string]string

func (m mem) Setting(k string) (string, error) { return m[k], nil }
func (m mem) SetSetting(k, v string) error     { m[k] = v; return nil }

func withCookie(v string) *http.Request {
	r := httptest.NewRequest("GET", "/api/machines", nil)
	if v != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: v})
	}
	return r
}

func TestLoginAndSessions(t *testing.T) {
	st := mem{}
	a := New(st)
	if need, _ := a.NeedsSetup(); !need {
		t.Fatal("fresh store should need setup")
	}
	if err := a.SetPassword("short"); !errors.Is(err, ErrWeak) {
		t.Errorf("weak password accepted: %v", err)
	}
	if err := a.SetPassword("correct horse"); err != nil {
		t.Fatal(err)
	}
	if st[keyHash] == "correct horse" || st[keyHash] == "" {
		t.Fatal("password must be stored hashed")
	}
	if _, err := a.Login("wrong", false); !errors.Is(err, ErrBadPassword) {
		t.Errorf("wrong password: %v", err)
	}
	c, err := a.Login("correct horse", true)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie flags = %+v", c)
	}
	if a.Valid(withCookie("")) {
		t.Error("request without cookie accepted")
	}
	if !a.Valid(withCookie(c.Value)) {
		t.Error("fresh session rejected")
	}

	// an attacker can neither forge a session nor extend one by editing the expiry
	_, sig, _ := strings.Cut(c.Value, ".")
	for _, v := range []string{"garbage", "9999999999", "9999999999." + sig, "9999999999.AAAA"} {
		if a.Valid(withCookie(v)) {
			t.Errorf("forged cookie %q accepted", v)
		}
	}
	a.now = func() time.Time { return time.Now().Add(sessionTTL + time.Hour) }
	if a.Valid(withCookie(c.Value)) {
		t.Error("expired session accepted")
	}
	a.now = time.Now
	a.SetPassword("another password")
	if a.Valid(withCookie(c.Value)) {
		t.Error("session survived a password change")
	}
}

func TestEnsurePasswordKeepsSessions(t *testing.T) {
	a := New(mem{})
	a.EnsurePassword("correct horse")
	c, _ := a.Login("correct horse", false)
	a.EnsurePassword("correct horse") // restart with the same env password
	if !a.Valid(withCookie(c.Value)) {
		t.Error("same password rotated the secret")
	}
}
