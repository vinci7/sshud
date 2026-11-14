package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMachinesCRUD(t *testing.T) {
	s := open(t)
	a, err := s.AddMachine(Machine{Name: "web-1", Host: "192.0.2.10", User: "ops"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Port != 22 {
		t.Errorf("defaults not applied: %+v", a)
	}
	if _, err := s.AddMachine(Machine{Name: "web-1", Host: "x", User: "u"}); err == nil {
		t.Error("duplicate name accepted")
	}
	b, _ := s.AddMachine(Machine{Name: "db-1", Host: "192.0.2.11", User: "ops"})
	list, _ := s.Machines()
	if len(list) != 2 || list[0].Name != "web-1" || list[1].ID != b.ID {
		t.Errorf("order = %+v", list)
	}

	// host key is kept on rename, dropped when the address changes
	s.SetHostKey(a.ID, []byte("k1"))
	a.Name = "web-one"
	if err := s.UpdateMachine(a); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.HostKey(a.ID); string(k) != "k1" {
		t.Error("host key lost on rename")
	}
	a.Host = "192.0.2.99"
	s.UpdateMachine(a)
	if k, _ := s.HostKey(a.ID); k != nil {
		t.Error("host key kept after address change")
	}

	if err := s.DeleteMachine(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Machine(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.DeleteMachine("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete unknown: %v", err)
	}
}

func TestValidate(t *testing.T) {
	ok := Machine{Name: "a", Host: "h", User: "u", Port: 22}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []func(*Machine){
		func(m *Machine) { m.Name = " " },
		func(m *Machine) { m.Host = "" },
		func(m *Machine) { m.Port = 70000 },
		func(m *Machine) { m.User = "" },
		func(m *Machine) { m.From = `x",command="sh` },
		func(m *Machine) { m.Host = "-oProxyCommand=sh" },
		func(m *Machine) { m.User = "-lroot" },
		func(m *Machine) { m.Host = "h\nx" },
		func(m *Machine) { m.User = "u\x7f" },
		func(m *Machine) { m.Host = "h\x00" },
	} {
		m := ok
		bad(&m)
		if Validate(m) == nil {
			t.Errorf("accepted %+v", m)
		}
	}
}

func TestDatabaseFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	fi, err := os.Stat(filepath.Join(dir, "sshud.db"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestSettings(t *testing.T) {
	s := open(t)
	if v, _ := s.Setting("x"); v != "" {
		t.Error("unset setting not empty")
	}
	s.SetSetting("x", "1")
	s.SetSetting("x", "2")
	if v, _ := s.Setting("x"); v != "2" {
		t.Errorf("x = %q", v)
	}
}

func TestOpenTightensPermissions(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "sshud.db"), nil, 0o644)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	for p, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "sshud.db"): 0o600} {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v", p, fi.Mode().Perm(), want)
		}
	}
}

// A database from before strict mode was the only mode still has auth and
// key_path columns and no allow_from; its machines load and new ones can be
// added.
func TestOpenOldSchema(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "sshud.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE machines (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, host TEXT NOT NULL,
		port INTEGER NOT NULL DEFAULT 22, user TEXT NOT NULL, auth TEXT NOT NULL DEFAULT 'simple',
		key_path TEXT NOT NULL DEFAULT '', position INTEGER NOT NULL DEFAULT 0);
		INSERT INTO machines VALUES ('a1', 'old', '192.0.2.1', 22, 'ops', 'simple', '/home/ops/.ssh/id_ed25519', 1);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.AddMachine(Machine{Name: "new", Host: "192.0.2.2", User: "ops", From: "192.0.2.0/24"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Machines()
	if err != nil || len(list) != 2 || list[0] != (Machine{ID: "a1", Name: "old", Host: "192.0.2.1", Port: 22, User: "ops"}) || list[1].From != "192.0.2.0/24" {
		t.Errorf("machines = %+v, %v", list, err)
	}
}
