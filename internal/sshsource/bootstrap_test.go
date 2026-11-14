package sshsource

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/probeerr"
	"github.com/vinci7/sshud/internal/store"
)

// sshd is an in-process SSH server that behaves like OpenSSH for one
// account whose home is a temporary directory: it takes a password (or
// keyboard-interactive), the user's own key, and keys listed in
// ~/.ssh/authorized_keys, whose command= it then forces. Commands run under
// sh with HOME set, so the install script really edits the file.
type sshd struct {
	host, home string
	port       int
	hostKey    ssh.Signer
	auths      atomic.Int32 // authentication attempts of any kind

	password   string        // "" refuses passwords
	kbdOnly    bool          // offer keyboard-interactive but not password
	userKey    ssh.PublicKey // the user's own key, if any
	ignoreKeys bool          // like an AuthorizedKeysFile somewhere else
}

func startSSHD(t *testing.T, d *sshd) *sshd {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	d.home, d.hostKey = t.TempDir(), *newHostKey()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	d.host, d.port = "127.0.0.1", ln.Addr().(*net.TCPAddr).Port
	cfg := &ssh.ServerConfig{PublicKeyCallback: d.publicKey}
	if d.password != "" && !d.kbdOnly {
		cfg.PasswordCallback = func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			d.auths.Add(1)
			if string(pw) == d.password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
	}
	if d.password != "" {
		cfg.KeyboardInteractiveCallback = func(_ ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			d.auths.Add(1)
			if a, err := ask("", "", []string{"Password: "}, []bool{false}); err == nil && len(a) == 1 && a[0] == d.password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
	}
	cfg.AddHostKey(d.hostKey)
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(nc, cfg)
		}
	}()
	return d
}

func (d *sshd) authorizedKeys() string { return filepath.Join(d.home, ".ssh", "authorized_keys") }

func (d *sshd) publicKey(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	d.auths.Add(1)
	if d.userKey != nil && bytes.Equal(key.Marshal(), d.userKey.Marshal()) {
		return nil, nil
	}
	rest, _ := os.ReadFile(d.authorizedKeys())
	for len(rest) > 0 && !d.ignoreKeys {
		pub, _, opts, next, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			break
		}
		rest = next
		if !bytes.Equal(pub.Marshal(), key.Marshal()) {
			continue
		}
		for _, o := range opts {
			if v, ok := strings.CutPrefix(o, `command="`); ok {
				// sshd unescapes \" inside option values
				forced := strings.ReplaceAll(strings.TrimSuffix(v, `"`), `\"`, `"`)
				return &ssh.Permissions{Extensions: map[string]string{"forced": forced}}, nil
			}
		}
	}
	return nil, errors.New("denied")
}

func (d *sshd) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		c, creqs, _ := ch.Accept()
		go func() {
			defer c.Close()
			for req := range creqs {
				var p struct{ Cmd string }
				ssh.Unmarshal(req.Payload, &p)
				req.Reply(true, nil)
				cmd := exec.Command("sh", "-c", p.Cmd)
				if conn.Permissions != nil && conn.Permissions.Extensions["forced"] != "" {
					cmd = exec.Command("sh", "-c", conn.Permissions.Extensions["forced"])
				}
				cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + d.home, "SSH_ORIGINAL_COMMAND=" + p.Cmd}
				cmd.Stdout, cmd.Stderr = c, c.Stderr()
				status := uint32(0)
				if err := cmd.Run(); err != nil {
					status = 1
				}
				c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				return
			}
		}()
	}
}

// bootstrapEnv is a fresh data directory with one machine for d.
func bootstrapEnv(t *testing.T, d *sshd) (*Source, *store.Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src := New(st, dir)
	t.Cleanup(src.Close)
	m, err := st.AddMachine(store.Machine{Name: "new", Host: d.host, Port: d.port, User: "me"})
	if err != nil {
		t.Fatal(err)
	}
	return src, st, dir, m.ID
}

// sshKeygenFingerprint is what `ssh-keygen -l` shows for an ed25519 host key.
func sshKeygenFingerprint(pub ssh.PublicKey) string {
	sum := sha256.Sum256(pub.Marshal())
	return "ED25519 SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func kind(err error) probeerr.Kind {
	var pe *probeerr.Error
	if errors.As(err, &pe) {
		return pe.Kind
	}
	return ""
}

// dataContains reports whether any file under dir holds secret.
func dataContains(t *testing.T, dir, secret string) bool {
	found := false
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			b, _ := os.ReadFile(p)
			found = found || bytes.Contains(b, []byte(secret))
		}
		return nil
	})
	return found
}

func TestBootstrapPassword(t *testing.T) {
	for _, kbdOnly := range []bool{false, true} {
		d := startSSHD(t, &sshd{password: "pa55-w0rd!", kbdOnly: kbdOnly})
		src, st, dir, id := bootstrapEnv(t, d)
		signer, _ := src.StrictSigner()
		strictKey := base64.StdEncoding.EncodeToString(signer.PublicKey().Marshal())
		other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOtherKeyOtherKeyOtherKeyOtherKeyOtherKe me@laptop"
		old := `restrict,command="echo old" ssh-ed25519 ` + strictKey + " sshud"
		before := other + "\n" + old + "\n" + "# a comment\n"
		os.Mkdir(filepath.Join(d.home, ".ssh"), 0o755)
		os.WriteFile(d.authorizedKeys(), []byte(before), 0o644)

		err := src.Bootstrap(context.Background(), id, Credentials{Password: "pa55-w0rd!"})
		if err != nil {
			t.Fatalf("kbdOnly=%v: %v", kbdOnly, err)
		}
		got, _ := os.ReadFile(d.authorizedKeys())
		lines := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")
		if len(lines) != 3 || lines[0] != other || lines[1] != "# a comment" ||
			!strings.HasPrefix(lines[2], `restrict,command="sh -c `) || !strings.HasSuffix(lines[2], " ssh-ed25519 "+strictKey+" sshud") {
			t.Errorf("authorized_keys:\n%s", got)
		}
		if fi, _ := os.Stat(d.authorizedKeys()); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", fi.Mode().Perm())
		}
		baks, _ := filepath.Glob(d.authorizedKeys() + ".bak-*")
		if b, _ := os.ReadFile(baks[0]); len(baks) != 1 || string(b) != before {
			t.Errorf("backup %v = %q", baks, b)
		}
		if k, _ := st.HostKey(id); !bytes.Equal(k, d.hostKey.PublicKey().Marshal()) {
			t.Error("first host key not recorded")
		}
		if _, err := src.Probe(context.Background(), id, hub.Overview); err != nil {
			t.Errorf("probe after bootstrap: %v", err)
		}
		if dataContains(t, dir, "pa55-w0rd!") {
			t.Error("password written to the data directory")
		}
	}
}

func TestBootstrapPrivateKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	userSigner, _ := ssh.NewSignerFromKey(priv)
	plain, _ := ssh.MarshalPrivateKey(priv, "me@laptop")
	locked, _ := ssh.MarshalPrivateKeyWithPassphrase(priv, "me@laptop", []byte("open sesame"))
	for name, c := range map[string]Credentials{
		"plain":      {PrivateKey: string(pem.EncodeToMemory(plain))},
		"passphrase": {PrivateKey: string(pem.EncodeToMemory(locked)), Passphrase: "open sesame"},
	} {
		d := startSSHD(t, &sshd{userKey: userSigner.PublicKey()})
		src, _, dir, id := bootstrapEnv(t, d)
		if err := src.Bootstrap(context.Background(), id, c); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if baks, _ := filepath.Glob(d.authorizedKeys() + ".bak-*"); len(baks) != 0 {
			t.Errorf("%s: backup of a file that did not exist", name)
		}
		// the PEM body lines after the first (which is the same for every
		// OpenSSH key, sshud's own included), and the passphrase
		body := strings.Split(c.PrivateKey, "\n")
		for _, secret := range append(body[2:len(body)-2], "open sesame") {
			if len(secret) > 8 && dataContains(t, dir, secret) {
				t.Errorf("%s: %q found in the data directory", name, secret)
			}
		}
	}
	d := startSSHD(t, &sshd{userKey: userSigner.PublicKey()})
	src, _, _, id := bootstrapEnv(t, d)
	for _, pass := range []string{"", "wrong"} {
		err := src.Bootstrap(context.Background(), id, Credentials{PrivateKey: string(pem.EncodeToMemory(locked)), Passphrase: pass})
		if kind(err) != probeerr.ErrPassphrase || strings.Contains(err.Error(), "open sesame") {
			t.Errorf("passphrase %q: %v", pass, err)
		}
	}
	other, _ := ssh.MarshalPrivateKey(ed25519.NewKeyFromSeed(make([]byte, 32)), "")
	if err := src.Bootstrap(context.Background(), id, Credentials{PrivateKey: string(pem.EncodeToMemory(other))}); kind(err) != probeerr.ErrCredentials {
		t.Errorf("unknown key: %v", err)
	}
}

// A host whose key differs from the recorded one never sees a credential.
func TestBootstrapRefusesChangedHostKey(t *testing.T) {
	d := startSSHD(t, &sshd{password: "pa55-w0rd!"})
	src, st, _, id := bootstrapEnv(t, d)
	recorded := (*newHostKey()).PublicKey().Marshal()
	st.SetHostKey(id, recorded)
	err := src.Bootstrap(context.Background(), id, Credentials{Password: "pa55-w0rd!"})
	if kind(err) != probeerr.ErrHostKey {
		t.Errorf("err = %v", err)
	}
	if n := d.auths.Load(); n != 0 {
		t.Errorf("server saw %d authentication attempts", n)
	}
	if _, err := os.Stat(d.authorizedKeys()); err == nil {
		t.Error("authorized_keys written")
	}
	if k, _ := st.HostKey(id); !bytes.Equal(k, recorded) {
		t.Error("recorded host key replaced")
	}
}

func TestBootstrapFailures(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		d    *sshd
		c    Credentials
		want probeerr.Kind
		prep func(d *sshd)
	}{
		{"wrong password", &sshd{password: "right"}, Credentials{Password: "wrong"}, probeerr.ErrCredentials, nil},
		{"keys only", &sshd{userKey: (*newHostKey()).PublicKey()}, Credentials{Password: "pw"}, probeerr.ErrNoPassword, nil},
		{"install fails", &sshd{password: "pw"}, Credentials{Password: "pw"}, probeerr.ErrInstall, func(d *sshd) {
			os.WriteFile(filepath.Join(d.home, ".ssh"), []byte("not a directory"), 0o644)
		}},
		{"key not honoured", &sshd{password: "pw", ignoreKeys: true}, Credentials{Password: "pw"}, probeerr.ErrVerify, nil},
	}
	for _, c := range cases {
		d := startSSHD(t, c.d)
		if c.prep != nil {
			c.prep(d)
		}
		src, _, _, id := bootstrapEnv(t, d)
		err := src.Bootstrap(ctx, id, c.c)
		if kind(err) != c.want || strings.Contains(err.Error(), "wrong") {
			t.Errorf("%s: %v (%s)", c.name, err, kind(err))
		}
		// the remote stderr is passed on; its wording differs by platform
		// ("…/.ssh: File exists" on macOS, "Already exists" on some Linux)
		if c.want == probeerr.ErrInstall && !strings.Contains(err.Error(), "mkdir") {
			t.Errorf("%s: remote error not shown: %v", c.name, err)
		}
	}
	d := startSSHD(t, &sshd{password: "pw"})
	src, _, _, _ := bootstrapEnv(t, d)
	if fp, _, err := src.HostKey(ctx, d.host, d.port); err != nil || fp != sshKeygenFingerprint(d.hostKey.PublicKey()) || d.auths.Load() != 0 {
		t.Errorf("HostKey = %q, %v, %d auths", fp, err, d.auths.Load())
	}
}
