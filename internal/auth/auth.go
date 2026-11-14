// Package auth implements the single-administrator login: a bcrypt password
// hash and a signing secret kept in the settings store, and stateless
// HMAC-signed session cookies so restarts do not log anyone out.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Settings is the persistence the authenticator needs.
type Settings interface {
	Setting(k string) (string, error)
	SetSetting(k, v string) error
}

const (
	keyHash    = "auth.password_hash"
	keySecret  = "auth.session_secret"
	cookieName = "sshud_session"
	sessionTTL = 30 * 24 * time.Hour
)

var (
	ErrWeak        = errors.New("password must be at least 8 characters")
	ErrBadPassword = errors.New("wrong password")
)

// Auth checks the admin password and session cookies.
type Auth struct {
	st  Settings
	now func() time.Time
}

// New returns an authenticator backed by st.
func New(st Settings) *Auth { return &Auth{st: st, now: time.Now} }

// NeedsSetup reports whether no password has been set yet.
func (a *Auth) NeedsSetup() (bool, error) {
	h, err := a.st.Setting(keyHash)
	return h == "", err
}

// SetPassword stores a new password and a new signing secret, which logs out
// every existing session.
func (a *Auth) SetPassword(pw string) error {
	if len(pw) < 8 {
		return ErrWeak
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	if err := a.st.SetSetting(keyHash, string(h)); err != nil {
		return err
	}
	return a.st.SetSetting(keySecret, base64.RawURLEncoding.EncodeToString(secret))
}

// EnsurePassword sets pw unless it already is the password, so a password
// given through the environment does not log everyone out on each restart.
func (a *Auth) EnsurePassword(pw string) error {
	if a.check(pw) {
		return nil
	}
	return a.SetPassword(pw)
}

func (a *Auth) check(pw string) bool {
	h, err := a.st.Setting(keyHash)
	return err == nil && h != "" && bcrypt.CompareHashAndPassword([]byte(h), []byte(pw)) == nil
}

// Login returns a session cookie if pw is the password. secure sets the
// cookie's Secure flag (the browser reached us over HTTPS).
func (a *Auth) Login(pw string, secure bool) (*http.Cookie, error) {
	if !a.check(pw) {
		return nil, ErrBadPassword
	}
	secret, err := a.st.Setting(keySecret)
	if err != nil {
		return nil, err
	}
	exp := a.now().Add(sessionTTL).Unix()
	payload := strconv.FormatInt(exp, 10)
	c := Logout(secure)
	c.Value, c.MaxAge, c.Expires = payload+"."+sign(secret, payload), 0, time.Unix(exp, 0)
	return c, nil
}

// Logout returns a cookie that clears the session in the browser.
func Logout(secure bool) *http.Cookie {
	return &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode}
}

// Valid reports whether r carries a live, correctly signed session cookie.
func (a *Auth) Valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	payload, sig, _ := strings.Cut(c.Value, ".")
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || a.now().Unix() > exp {
		return false
	}
	secret, err := a.st.Setting(keySecret)
	return err == nil && secret != "" && hmac.Equal([]byte(sig), []byte(sign(secret, payload)))
}

func sign(secret, payload string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
