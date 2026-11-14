package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/probeerr"
	"github.com/vinci7/sshud/internal/sshsource"
	"github.com/vinci7/sshud/internal/store"
)

type fakeAdmin struct {
	list         []store.Machine
	tested       store.Machine
	trusted      string
	bootstrapped sshsource.Credentials
}

func (f *fakeAdmin) List() ([]store.Machine, error) { return f.list, nil }
func (f *fakeAdmin) Add(m store.Machine) (store.Machine, error) {
	if err := store.Validate(m); err != nil {
		return m, err
	}
	m.ID = "new"
	f.list = append(f.list, m)
	return m, nil
}
func (f *fakeAdmin) Update(m store.Machine) error {
	for i := range f.list {
		if f.list[i].ID == m.ID {
			f.list[i] = m
			return nil
		}
	}
	return store.ErrNotFound
}
func (f *fakeAdmin) Delete(id string) error { return store.ErrNotFound }
func (f *fakeAdmin) Trust(_ context.Context, id, fp string) (string, string, error) {
	if fp == "SHA256:new" {
		f.trusted = id
	}
	return "SHA256:old", "SHA256:new", nil
}
func (f *fakeAdmin) Bootstrap(_ context.Context, id string, c sshsource.Credentials) error {
	f.bootstrapped = c
	if c.Password != "right" {
		return &probeerr.Error{Kind: probeerr.ErrCredentials, Err: errors.New("ssh: unable to authenticate")}
	}
	return nil
}
func (f *fakeAdmin) PublicKey() (ssh.PublicKey, error) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	return ssh.NewPublicKey(pub)
}
func (f *fakeAdmin) Test(_ context.Context, m store.Machine) sshsource.TestResult {
	f.tested = m
	return sshsource.TestResult{OK: true, Hostname: "h"}
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestAdminEndpoints(t *testing.T) {
	a := &fakeAdmin{}
	h := New(Options{Hosts: testHosts, UI: testUI(), Admin: a, Machines: a})

	rec := do(t, h, "POST", "/api/admin/machines", `{"name":" web ","host":"192.0.2.1","user":"ops"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if m := a.list[0]; m.Name != "web" || m.Port != 22 {
		t.Errorf("defaults/trim not applied: %+v", m)
	}
	if rec := do(t, h, "POST", "/api/admin/machines", `{"name":"x"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "required") {
		t.Errorf("invalid add: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "PUT", "/api/admin/machines/new", `{"name":"web2","host":"h","user":"u"}`); rec.Code != http.StatusOK || a.list[0].Name != "web2" {
		t.Errorf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "PUT", "/api/admin/machines/nope", `{"name":"a","host":"h","user":"u"}`); rec.Code != http.StatusNotFound {
		t.Errorf("update unknown: %d", rec.Code)
	}
	if rec := do(t, h, "DELETE", "/api/admin/machines/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete unknown: %d", rec.Code)
	}
	rec = do(t, h, "GET", "/api/admin/machines", "")
	var list []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["user"] != "u" {
		t.Errorf("list: %s", rec.Body)
	}
	if rec := do(t, h, "POST", "/api/admin/test", `{"name":"t","host":"h","user":"u","port":2222}`); rec.Code != 200 || a.tested.Port != 2222 {
		t.Errorf("test: %d %s", rec.Code, rec.Body)
	}
}

func TestDemoIsReadOnly(t *testing.T) {
	h := New(Options{Hosts: testHosts, UI: testUI(), Demo: true})
	for _, c := range [][2]string{{"GET", "/api/admin/machines"}, {"POST", "/api/admin/machines"}, {"DELETE", "/api/admin/machines/x"}, {"POST", "/api/admin/test"}} {
		if rec := do(t, h, c[0], c[1], "{}"); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d", c[0], c[1], rec.Code)
		}
	}
	rec := do(t, h, "GET", "/api/meta", "")
	if !strings.Contains(rec.Body.String(), `"readOnly":true`) {
		t.Errorf("meta: %s", rec.Body)
	}
}

func TestImportPreview(t *testing.T) {
	a := &fakeAdmin{list: []store.Machine{{ID: "1", Name: "old"}}}
	h := New(Options{Hosts: testHosts, UI: testUI(), Admin: a, Machines: a})
	text := "Host old\n User u\nHost new\n HostName 192.0.2.5\n User u\nHost jump\n User u\n ProxyJump old\nHost anon\n"
	body, _ := json.Marshal(map[string]string{"text": text})
	rec := do(t, h, "POST", "/api/admin/import/preview", string(body))
	var got struct {
		Rows []struct {
			Name, Host, Note string
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Rows) != 4 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	notes := map[string]string{}
	for _, r := range got.Rows {
		notes[r.Name] = r.Note
	}
	if notes["old"] != "exists" || notes["new"] != "" || notes["jump"] != "proxyjump" || notes["anon"] != "nouser" {
		t.Errorf("notes = %v", notes)
	}
	if r := got.Rows[1]; r.Host != "192.0.2.5" {
		t.Errorf("new = %+v", r)
	}
}

func TestMachineLines(t *testing.T) {
	h := New(Options{Hosts: testHosts, UI: testUI(), Admin: &fakeAdmin{}, Machines: &fakeAdmin{}})
	rec := do(t, h, "POST", "/api/admin/machines", `{"name":"web","host":"192.0.2.1","port":2222,"user":"ops","from":"192.0.2.0/24"}`)
	var m machineDTO
	json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != http.StatusCreated || !strings.Contains(m.Line, `,from="192.0.2.0/24" ssh-ed25519 `) || !strings.HasPrefix(m.Command, "ssh -p 2222 -- ops@192.0.2.1 ") {
		t.Errorf("add: %d %+v", rec.Code, m)
	}
	// a quote would break out of the from="..." option
	if rec := do(t, h, "POST", "/api/admin/machines", `{"name":"x","host":"h","user":"u","from":"x\",command=\"sh"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("injection accepted: %d", rec.Code)
	}
}

func TestRetrust(t *testing.T) {
	a := &fakeAdmin{}
	h := New(Options{Hosts: testHosts, UI: testUI(), Admin: a, Machines: a})
	for body, want := range map[string]string{
		``:                             `{"presented":"SHA256:new","recorded":"SHA256:old","trusted":false}`,
		`{"fingerprint":"SHA256:x"}`:   `"trusted":false`,
		`{"fingerprint":"SHA256:new"}`: `"trusted":true`,
	} {
		if rec := do(t, h, "POST", "/api/admin/machines/m1/trust", body); rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if a.trusted != "m1" {
		t.Error("confirmed key not trusted")
	}
	// another site cannot make the browser re-trust a key
	a.trusted = ""
	req := httptest.NewRequest("POST", "/api/admin/machines/m1/trust", strings.NewReader(`{"fingerprint":"SHA256:new"}`))
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || a.trusted != "" {
		t.Errorf("cross-origin trust: %d", rec.Code)
	}
}

func TestBootstrapEndpoints(t *testing.T) {
	a := &fakeAdmin{}
	h := New(Options{Hosts: testHosts, UI: testUI(), Admin: a, Machines: a})
	for body, want := range map[string]string{
		`{"password":"right"}`: `{"ok":true}`,
		`{"password":"wrong"}`: `"kind":"credentials"`,
		`{}`:                   `either a password`,
		`{"password":"right","privateKey":"-----B"}`: `either a password`,
	} {
		rec := do(t, h, "POST", "/api/admin/machines/m1/bootstrap", body)
		if !strings.Contains(rec.Body.String(), want) || strings.Contains(rec.Body.String(), "wrong") {
			t.Errorf("bootstrap %s: %d %s", body, rec.Code, rec.Body)
		}
	}
	do(t, h, "POST", "/api/admin/machines/m1/bootstrap", `{"privateKey":"PEM","passphrase":"pp"}`)
	if c := a.bootstrapped; c.PrivateKey != "PEM" || c.Passphrase != "pp" || c.Password != "" {
		t.Errorf("credentials not passed on: %+v", c)
	}
	if rec := do(t, New(Options{Hosts: testHosts, UI: testUI(), Demo: true}), "POST", "/api/admin/machines/m1/bootstrap", `{}`); rec.Code != http.StatusForbidden {
		t.Errorf("demo bootstrap: %d", rec.Code)
	}
}
