package sshpool

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/probeerr"
)

// memKeys is an in-memory HostKeys.
type memKeys struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (k *memKeys) HostKey(id string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[id], nil
}

func (k *memKeys) SetHostKey(id string, key []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string][]byte{}
	}
	k.m[id] = key
	return nil
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// server is a minimal in-process SSH server. handle decides the reply to
// each exec request: stdout, exit status, and whether to hang instead. The
// command "vanish" makes the server stop reading, like a peer whose network
// went away: it neither answers nor closes anything.
type server struct {
	addr    string
	host    ssh.Signer
	allowed ssh.PublicKey
	handle  func(cmd string) (out string, status uint32, hang bool)
	ln      net.Listener
	mu      sync.Mutex
	conns   []net.Conn
	accepts int
	frozen  atomic.Bool
	closed  chan struct{}
}

// frozenConn swallows everything read once the server is frozen.
type frozenConn struct {
	net.Conn
	s *server
}

func (c frozenConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if c.s.frozen.Load() {
		<-c.s.closed
		return 0, net.ErrClosed
	}
	return n, err
}

func startServer(t *testing.T, client ssh.Signer, handle func(string) (string, uint32, bool)) *server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &server{addr: ln.Addr().String(), host: newSigner(t), allowed: client.PublicKey(), handle: handle, ln: ln, closed: make(chan struct{})}
	t.Cleanup(s.close)
	go s.serve()
	return s
}

func (s *server) close() {
	close(s.closed)
	s.ln.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
}

// dropConnections kills established sessions but keeps listening, like a
// NAT timing out.
func (s *server) dropConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
}

func (s *server) serve() {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(s.allowed.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	cfg.AddHostKey(s.host)
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, nc)
		s.accepts++
		s.mu.Unlock()
		go func() {
			_, chans, reqs, err := ssh.NewServerConn(frozenConn{nc, s}, cfg)
			if err != nil {
				nc.Close()
				return
			}
			go ssh.DiscardRequests(reqs)
			for ch := range chans {
				if ch.ChannelType() != "session" {
					ch.Reject(ssh.UnknownChannelType, "")
					continue
				}
				c, creqs, _ := ch.Accept()
				go s.session(c, creqs)
			}
		}()
	}
}

func (s *server) session(c ssh.Channel, reqs <-chan *ssh.Request) {
	defer c.Close()
	for req := range reqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		var p struct{ Cmd string }
		ssh.Unmarshal(req.Payload, &p)
		req.Reply(true, nil)
		if p.Cmd == "vanish" {
			s.frozen.Store(true)
			<-s.closed
			return
		}
		out, status, hang := s.handle(p.Cmd)
		if hang {
			c.Write([]byte(out))
			time.Sleep(time.Hour)
			return
		}
		c.Write([]byte(out))
		c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
		return
	}
}

func (s *server) target(signer ssh.Signer) Target {
	host, port, _ := net.SplitHostPort(s.addr)
	p, _ := strconv.Atoi(port)
	return Target{Host: host, Port: p, User: "probe", Signer: signer}
}

func echo(cmd string) (string, uint32, bool) {
	return "@@" + cmd, 0, false
}

func kindOf(t *testing.T, err error) probeerr.Kind {
	t.Helper()
	var pe *probeerr.Error
	if !errors.As(err, &pe) {
		t.Fatalf("not a probeerr.Error: %v", err)
	}
	return pe.Kind
}

func TestRunReusesConnectionAndRecordsHostKey(t *testing.T) {
	key := newSigner(t)
	srv := startServer(t, key, echo)
	keys := &memKeys{}
	p := New(keys)
	defer p.Close()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		out, err := p.Run(ctx, "m", srv.target(key), "overview")
		if err != nil || string(out) != "@@overview" {
			t.Fatalf("run %d: %q %v", i, out, err)
		}
	}
	if srv.accepts != 1 {
		t.Errorf("dialled %d times, want 1", srv.accepts)
	}
	if got, _ := keys.HostKey("m"); string(got) != string(srv.host.PublicKey().Marshal()) {
		t.Error("host key not recorded on first use")
	}
}

func TestHostKeyChangeIsRefused(t *testing.T) {
	key := newSigner(t)
	srv := startServer(t, key, echo)
	keys := &memKeys{}
	keys.SetHostKey("m", newSigner(t).PublicKey().Marshal()) // someone else's key
	p := New(keys)
	defer p.Close()
	_, err := p.Run(context.Background(), "m", srv.target(key), "x")
	if kindOf(t, err) != probeerr.ErrHostKey {
		t.Errorf("err = %v", err)
	}
}

func TestWrongKeyIsAuthError(t *testing.T) {
	srv := startServer(t, newSigner(t), echo)
	p := New(&memKeys{})
	defer p.Close()
	_, err := p.Run(context.Background(), "m", srv.target(newSigner(t)), "x")
	if kindOf(t, err) != probeerr.ErrAuth {
		t.Errorf("err = %v", err)
	}
}

func TestRefusedAndTimeout(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().(*net.TCPAddr)
	ln.Close() // nothing listens here any more
	p := New(&memKeys{})
	defer p.Close()
	key := newSigner(t)
	_, err := p.Run(context.Background(), "m", Target{Host: "127.0.0.1", Port: addr.Port, User: "u", Signer: key}, "x")
	if kindOf(t, err) != probeerr.ErrRefused {
		t.Errorf("closed port: %v", err)
	}

	// accepts TCP but never speaks SSH
	silent, _ := net.Listen("tcp", "127.0.0.1:0")
	defer silent.Close()
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	old := DialTimeout
	DialTimeout = 200 * time.Millisecond
	defer func() { DialTimeout = old }()
	sp := silent.Addr().(*net.TCPAddr).Port
	start := time.Now()
	_, err = p.Run(context.Background(), "s", Target{Host: "127.0.0.1", Port: sp, User: "u", Signer: key}, "x")
	if kindOf(t, err) != probeerr.ErrTimeout || time.Since(start) > 2*time.Second {
		t.Errorf("silent host: %v after %s", err, time.Since(start))
	}
}

func TestCommandTimeoutAndRejection(t *testing.T) {
	key := newSigner(t)
	srv := startServer(t, key, func(cmd string) (string, uint32, bool) {
		switch cmd {
		case "hang":
			return "", 0, true
		case "partial":
			return "@@stat1\ncpu 1 2 3 4\n", 1, false
		case "root":
			return "Please login as the user \"ubuntu\" rather than the user \"root\".\n", 142, false
		case "rootslow":
			return "Please login as ubuntu", 0, true
		}
		return "", 1, false // forced command did not match: no output
	})
	old := RunTimeout
	RunTimeout = 200 * time.Millisecond
	defer func() { RunTimeout = old }()
	p := New(&memKeys{})
	defer p.Close()
	ctx := context.Background()

	if _, err := p.Run(ctx, "m", srv.target(key), "hang"); kindOf(t, err) != probeerr.ErrTimeout {
		t.Errorf("hang: %v", err)
	}
	if out, err := p.Run(ctx, "m", srv.target(key), "partial"); err != nil || len(out) == 0 {
		t.Errorf("non-zero exit with output must be usable: %q %v", out, err)
	}
	if _, err := p.Run(ctx, "m", srv.target(key), "nope"); kindOf(t, err) != probeerr.ErrRejected {
		t.Errorf("rejected: %v", err)
	}
	_, err := p.Run(ctx, "m", srv.target(key), "root")
	if kindOf(t, err) != probeerr.ErrRejected || !strings.Contains(err.Error(), "Please login as") {
		t.Errorf("forced command: %v", err)
	}
	_, err = p.Run(ctx, "m", srv.target(key), "rootslow")
	if kindOf(t, err) != probeerr.ErrRejected || !strings.Contains(err.Error(), "Please login as") {
		t.Errorf("slow forced command: %v", err)
	}
}

func TestDeadConnectionIsRedialled(t *testing.T) {
	key := newSigner(t)
	srv := startServer(t, key, echo)
	p := New(&memKeys{})
	defer p.Close()
	ctx := context.Background()
	if _, err := p.Run(ctx, "m", srv.target(key), "a"); err != nil {
		t.Fatal(err)
	}
	srv.dropConnections()
	time.Sleep(50 * time.Millisecond)
	if out, err := p.Run(ctx, "m", srv.target(key), "b"); err != nil || string(out) != "@@b" {
		t.Fatalf("after drop: %q %v", out, err)
	}
	if srv.accepts != 2 {
		t.Errorf("accepts = %d", srv.accepts)
	}
}

func TestVanishedPeerTimesOut(t *testing.T) {
	key := newSigner(t)
	srv := startServer(t, key, echo)
	old := RunTimeout
	RunTimeout = 200 * time.Millisecond
	defer func() { RunTimeout = old }()
	p := New(&memKeys{})
	defer p.Close()
	start := time.Now()
	_, err := p.Run(context.Background(), "m", srv.target(key), "vanish")
	if kindOf(t, err) != probeerr.ErrTimeout || time.Since(start) > 2*time.Second {
		t.Errorf("vanished peer: %v after %s", err, time.Since(start))
	}
}
