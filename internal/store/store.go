// Package store persists the machine list, host keys and settings in SQLite
// (pure Go driver, so the binary stays CGO-free). No credentials are stored:
// every machine is reached with sshud's own strict-mode key.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Machine is one monitored machine.
type Machine struct {
	ID   string
	Name string
	Host string
	Port int
	User string
	From string // optional from= pattern for the authorized_keys line
}

// ErrNotFound is returned for unknown machine IDs.
var ErrNotFound = errors.New("machine not found")

// Store is a handle on the database.
type Store struct {
	db *sql.DB
}

// Databases from before strict mode became the only mode also have auth and
// key_path columns. They are ignored, so machines added with the user's own
// key are now probed with the strict-mode key; their connection test asks
// for the authorized_keys line.
const schema = `
CREATE TABLE IF NOT EXISTS machines (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL UNIQUE,
	host     TEXT NOT NULL,
	port     INTEGER NOT NULL DEFAULT 22,
	user     TEXT NOT NULL,
	position INTEGER NOT NULL DEFAULT 0,
	allow_from TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS host_keys (
	machine_id TEXT PRIMARY KEY REFERENCES machines(id) ON DELETE CASCADE,
	key        BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);
`

// Open opens (creating if needed) the database in dir with 0600 permissions.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "sshud.db")
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		f.Close()
	} else {
		return nil, err
	}
	// tighten files created by hand or by an older version
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite writes are serialised anyway; avoids SQLITE_BUSY
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	// added after the first version of the schema
	if _, err := db.Exec(`ALTER TABLE machines ADD COLUMN allow_from TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, fmt.Errorf("upgrade schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Machines returns all machines in display order.
func (s *Store) Machines() ([]Machine, error) {
	rows, err := s.db.Query(`SELECT id, name, host, port, user, allow_from FROM machines ORDER BY position, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Machine
	for rows.Next() {
		var m Machine
		if err := rows.Scan(&m.ID, &m.Name, &m.Host, &m.Port, &m.User, &m.From); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Machine returns one machine by ID.
func (s *Store) Machine(id string) (Machine, error) {
	var m Machine
	err := s.db.QueryRow(`SELECT id, name, host, port, user, allow_from FROM machines WHERE id = ?`, id).
		Scan(&m.ID, &m.Name, &m.Host, &m.Port, &m.User, &m.From)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Validate checks a machine before it is saved.
func Validate(m Machine) error {
	switch {
	case strings.TrimSpace(m.Name) == "":
		return errors.New("name is required")
	case strings.TrimSpace(m.Host) == "":
		return errors.New("host is required")
	case strings.TrimSpace(m.User) == "":
		return errors.New("user is required")
	case badArg(m.Host) || badArg(m.User):
		// both end up on an ssh command line
		return errors.New("host and user must not start with - or contain control characters")
	case m.Port < 1 || m.Port > 65535:
		return errors.New("port must be 1-65535")
	case strings.ContainsAny(m.From, "\"\\ \t\r\n"):
		// it goes inside from="..." in authorized_keys
		return errors.New("allowed addresses must be a comma-separated list without spaces or quotes")
	}
	return nil
}

func badArg(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "-") ||
		strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// AddMachine inserts m, assigning an ID and defaults. It returns the stored machine.
func (s *Store) AddMachine(m Machine) (Machine, error) {
	if m.Port == 0 {
		m.Port = 22
	}
	if err := Validate(m); err != nil {
		return m, err
	}
	m.ID = newID()
	_, err := s.db.Exec(`INSERT INTO machines (id, name, host, port, user, allow_from, position)
		VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM machines))`,
		m.ID, m.Name, m.Host, m.Port, m.User, m.From)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return m, fmt.Errorf("a machine named %q already exists", m.Name)
	}
	return m, err
}

// UpdateMachine replaces the stored fields of m.ID. Changing the address
// forgets the recorded host key, since it now belongs to another host.
func (s *Store) UpdateMachine(m Machine) error {
	if err := Validate(m); err != nil {
		return err
	}
	old, err := s.Machine(m.ID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE machines SET name=?, host=?, port=?, user=?, allow_from=? WHERE id=?`,
		m.Name, m.Host, m.Port, m.User, m.From, m.ID); err != nil {
		return err
	}
	if old.Host != m.Host || old.Port != m.Port {
		if _, err := tx.Exec(`DELETE FROM host_keys WHERE machine_id=?`, m.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteMachine removes a machine and its host key.
func (s *Store) DeleteMachine(id string) error {
	res, err := s.db.Exec(`DELETE FROM machines WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// HostKey implements sshpool.HostKeys.
func (s *Store) HostKey(machineID string) ([]byte, error) {
	var k []byte
	err := s.db.QueryRow(`SELECT key FROM host_keys WHERE machine_id=?`, machineID).Scan(&k)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

// SetHostKey implements sshpool.HostKeys.
func (s *Store) SetHostKey(machineID string, key []byte) error {
	_, err := s.db.Exec(`INSERT INTO host_keys (machine_id, key) VALUES (?, ?)
		ON CONFLICT(machine_id) DO UPDATE SET key=excluded.key`, machineID, key)
	return err
}

// Setting returns a setting, or "" when unset.
func (s *Store) Setting(k string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT v FROM settings WHERE k=?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting stores a setting.
func (s *Store) SetSetting(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO settings (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
