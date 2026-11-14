package sshsource

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/probe"
	"github.com/vinci7/sshud/internal/probeerr"
	"github.com/vinci7/sshud/internal/store"
)

// forcedServer behaves like sshd with a strict-mode authorized_keys entry:
// it accepts only `allowed`, ignores the requested command and runs the
// forced command with the request in SSH_ORIGINAL_COMMAND.
func forcedServer(t *testing.T, allowed ssh.PublicKey) (string, int) {
	return forcedServerWith(t, allowed, probe.ForcedCommand())
}

func forcedServerWith(t *testing.T, allowed ssh.PublicKey, forced string) (string, int) {
	var host atomic.Pointer[ssh.Signer]
	host.Store(newHostKey())
	return forcedServerHost(t, allowed, forced, &host)
}

func newHostKey() *ssh.Signer {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, _ := ssh.NewSignerFromKey(priv)
	return &s
}

// forcedServerHost presents whatever host key is in host at each connection,
// like a machine that gets reinstalled.
func forcedServerHost(t *testing.T, allowed ssh.PublicKey, forced string, host *atomic.Pointer[ssh.Signer]) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
				if string(k.Marshal()) == string(allowed.Marshal()) {
					return nil, nil
				}
				return nil, errors.New("denied")
			}}
			cfg.AddHostKey(*host.Load())
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
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
							io.Copy(io.Discard, c) // stdin is ignored by a forced command
							cmd := exec.Command("sh", "-c", forced)
							cmd.Env = []string{"PATH=/usr/bin:/bin", "SSH_ORIGINAL_COMMAND=" + p.Cmd}
							out, _ := cmd.Output()
							c.Write(out)
							c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
							return
						}
					}()
				}
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestStrictModeEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src := New(st, dir)
	defer src.Close()

	signer, err := src.StrictSigner()
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dir, StrictKeyFile))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("strict key mode = %v", fi.Mode().Perm())
	}
	again, _ := src.StrictSigner()
	if string(again.PublicKey().Marshal()) != string(signer.PublicKey().Marshal()) {
		t.Error("strict key regenerated on second use")
	}

	host, port := forcedServer(t, signer.PublicKey())
	m, err := st.AddMachine(store.Machine{Name: "strict", Host: host, Port: port, User: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []hub.Level{hub.Overview, hub.Detail} {
		out, err := src.Probe(context.Background(), m.ID, level)
		if err != nil {
			t.Fatalf("%s: %v", level, err)
		}
		if !strings.HasPrefix(out, "@@version") || strings.Contains(out, "@@outdated") || strings.Contains(out, "@@reboot") != (level == hub.Detail) {
			t.Errorf("%s output:\n%.200s", level, out)
		}
	}
	if res := src.Test(context.Background(), m); !res.OK && res.Kind != probeerr.ErrUnsupported {
		// on systems without /proc (macOS) the probe runs but has no numbers
		t.Errorf("test = %+v", res)
	}

}

func TestOutdatedStrictLine(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	st, _ := store.Open(dir)
	defer st.Close()
	src := New(st, dir)
	defer src.Close()
	signer, _ := src.StrictSigner()
	// a line pasted before the probe changed: no version header
	old := "sh -c '" + strings.ReplaceAll(probe.OneLine(), "'", `'\''`) + "'"
	host, port := forcedServerWith(t, signer.PublicKey(), old)
	m, _ := st.AddMachine(store.Machine{Name: "old", Host: host, Port: port, User: "probe"})
	out, err := src.Probe(context.Background(), m.ID, hub.Overview)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "@@outdated") || !strings.Contains(out, "@@stat1") {
		t.Errorf("old line output should still be used and be marked outdated:\n%.200s", out)
	}
}

// A reinstalled machine presents a new host key: probes are refused until
// the user confirms the new fingerprint, then they work again.
func TestRetrustHostKey(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	st, _ := store.Open(dir)
	defer st.Close()
	src := New(st, dir)
	defer src.Close()
	signer, _ := src.StrictSigner()
	var host atomic.Pointer[ssh.Signer]
	host.Store(newHostKey())
	addr, port := forcedServerHost(t, signer.PublicKey(), probe.ForcedCommand(), &host)
	m, _ := st.AddMachine(store.Machine{Name: "m", Host: addr, Port: port, User: "probe"})
	ctx := context.Background()
	if _, err := src.Probe(ctx, m.ID, hub.Overview); err != nil {
		t.Fatal(err)
	}
	first := fingerprint((*host.Load()).PublicKey().Marshal())

	host.Store(newHostKey()) // reinstalled
	src.Close()              // drop the cached connection, as a reboot would
	second := fingerprint((*host.Load()).PublicKey().Marshal())
	refused := func() bool {
		var pe *probeerr.Error
		_, err := src.Probe(ctx, m.ID, hub.Overview)
		return errors.As(err, &pe) && pe.Kind == probeerr.ErrHostKey
	}
	if !refused() {
		t.Fatal("changed host key not refused")
	}
	// looking (or confirming a fingerprint the machine does not present)
	// changes nothing
	for _, confirm := range []string{"", first} {
		recorded, presented, err := src.Trust(ctx, m.ID, confirm)
		if err != nil || recorded != first || presented != second || !strings.HasPrefix(presented, "ED25519 SHA256:") {
			t.Fatalf("trust %q = %q, %q, %v", confirm, recorded, presented, err)
		}
		if !refused() {
			t.Fatalf("trust %q changed the recorded key", confirm)
		}
	}
	if _, _, err := src.Trust(ctx, m.ID, second); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Probe(ctx, m.ID, hub.Overview); err != nil {
		t.Errorf("after re-trust: %v", err)
	}
}
