// Package sshpool keeps one SSH connection per machine and runs commands on
// it. It knows nothing about metrics: it sends a command (and optional stdin)
// and returns stdout, classifying failures so the UI can explain them.
package sshpool

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/probeerr"
)

// Timeouts. The design proposed 3 s / 5 s, but on lossy long-distance links
// one lost SYN costs 1 s and two cost 4 s, which failed healthy machines.
// Each machine has its own goroutine, so a slow one never delays the others.
var (
	DialTimeout = 10 * time.Second // TCP connect plus SSH handshake
	RunTimeout  = 8 * time.Second
	IdleTimeout = 5 * time.Minute
)

// Target is how to reach one machine.
type Target struct {
	Host   string
	Port   int
	User   string
	Signer ssh.Signer
}

func (t Target) addr() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

// HostKeys stores the host key first seen for each machine (trust on first use).
type HostKeys interface {
	// HostKey returns the stored key in wire format, or nil if none.
	HostKey(machineID string) ([]byte, error)
	SetHostKey(machineID string, key []byte) error
}

// Pool caches connections by machine ID. It is safe for concurrent use.
type Pool struct {
	keys HostKeys

	mu    sync.Mutex
	conns map[string]*conn
}

type conn struct {
	client *ssh.Client
	target Target
	used   time.Time
}

// New returns a pool that checks host keys against keys.
func New(keys HostKeys) *Pool {
	return &Pool{keys: keys, conns: map[string]*conn{}}
}

// Run executes cmd on the machine and returns stdout. Errors are *probeerr.Error. A connection that fails is dropped so
// the next call dials afresh.
func (p *Pool) Run(ctx context.Context, id string, t Target, cmd string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		c, reused, err := p.get(ctx, id, t)
		if err != nil {
			return nil, err
		}
		out, err := run(ctx, c.client, cmd)
		if err == nil {
			return out, nil
		}
		p.drop(id, c)
		// a cached connection may have died quietly (peer rebooted, NAT
		// expired): dial once more before reporting a failure
		if reused && attempt == 0 && errors.Is(err, errSession) {
			continue
		}
		return nil, err
	}
}

// Close drops every connection.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.conns {
		c.client.Close()
		delete(p.conns, id)
	}
}

// CloseIdle drops connections unused for longer than IdleTimeout.
func (p *Pool) CloseIdle(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.conns {
		if now.Sub(c.used) > IdleTimeout {
			c.client.Close()
			delete(p.conns, id)
		}
	}
}

func (p *Pool) get(ctx context.Context, id string, t Target) (c *conn, reused bool, err error) {
	p.mu.Lock()
	c = p.conns[id]
	if c != nil && !sameTarget(c.target, t) {
		// the machine was edited: forget the old connection
		c.client.Close()
		delete(p.conns, id)
		c = nil
	}
	if c != nil {
		c.used = time.Now()
		p.mu.Unlock()
		return c, true, nil
	}
	p.mu.Unlock()

	client, err := p.dial(ctx, id, t)
	if err != nil {
		return nil, false, err
	}
	c = &conn{client: client, target: t, used: time.Now()}
	p.mu.Lock()
	if old := p.conns[id]; old != nil {
		// lost a race with another dial; keep the first
		p.mu.Unlock()
		client.Close()
		return old, true, nil
	}
	p.conns[id] = c
	p.mu.Unlock()
	return c, false, nil
}

func (p *Pool) drop(id string, c *conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns[id] == c {
		delete(p.conns, id)
	}
	c.client.Close()
}

func sameTarget(a, b Target) bool {
	if a.Host != b.Host || a.Port != b.Port || a.User != b.User {
		return false
	}
	if a.Signer == nil || b.Signer == nil {
		return a.Signer == b.Signer
	}
	return bytes.Equal(a.Signer.PublicKey().Marshal(), b.Signer.PublicKey().Marshal())
}

// errSession marks a failure to open a session on an established connection.
var errSession = errors.New("open session")

// ErrHostKeyChanged marks a host key mismatch inside the handshake.
var ErrHostKeyChanged = errors.New("host key does not match the key seen on first connection")

func (p *Pool) dial(ctx context.Context, id string, t Target) (*ssh.Client, error) {
	if t.Signer == nil {
		return nil, &probeerr.Error{Kind: probeerr.ErrKey, Err: errors.New("no private key configured")}
	}
	cfg := &ssh.ClientConfig{
		User:              t.User,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(t.Signer)},
		HostKeyAlgorithms: HostKeyAlgorithms,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			got := key.Marshal()
			want, err := p.keys.HostKey(id)
			if err != nil {
				return err
			}
			if want == nil {
				return p.keys.SetHostKey(id, got)
			}
			if subtle.ConstantTimeCompare(want, got) != 1 {
				return ErrHostKeyChanged
			}
			return nil
		},
	}
	return Dial(ctx, t, cfg)
}

// HostKeyAlgorithms prefers the same host key type as OpenSSH, so the
// fingerprint we show matches what `ssh-keygen -l` prints for
// ssh_host_ed25519_key.
var HostKeyAlgorithms = []string{
	ssh.KeyAlgoED25519,
	ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
	ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256,
}

// Dial connects to t's host and port and completes the SSH handshake with
// cfg (t.Signer is not used). Errors are classified *probeerr.Error; a
// HostKeyCallback that returns ErrHostKeyChanged gives probeerr.ErrHostKey.
func Dial(ctx context.Context, t Target, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	dctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	nc, err := (&net.Dialer{}).DialContext(dctx, "tcp", t.addr())
	if err != nil {
		return nil, classifyDial(err)
	}
	// bound the handshake too; a host that accepts TCP but never speaks SSH
	// must not hang the probe
	_ = nc.SetDeadline(time.Now().Add(DialTimeout))
	sc, chans, reqs, err := ssh.NewClientConn(nc, t.addr(), cfg)
	if err != nil {
		nc.Close()
		return nil, classifyHandshake(err)
	}
	_ = nc.SetDeadline(time.Time{})
	return ssh.NewClient(sc, chans, reqs), nil
}

func classifyDial(err error) error {
	kind := probeerr.ErrUnreachable
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		kind = probeerr.ErrTimeout
	case errors.Is(err, syscall.ECONNREFUSED):
		kind = probeerr.ErrRefused
	}
	return &probeerr.Error{Kind: kind, Err: err}
}

func classifyHandshake(err error) error {
	kind := probeerr.ErrUnreachable
	var ne net.Error
	switch {
	case errors.Is(err, ErrHostKeyChanged):
		kind = probeerr.ErrHostKey
	case strings.Contains(err.Error(), "unable to authenticate"):
		kind = probeerr.ErrAuth
	case errors.As(err, &ne) && ne.Timeout():
		kind = probeerr.ErrTimeout
	}
	return &probeerr.Error{Kind: kind, Err: err}
}

func run(ctx context.Context, client *ssh.Client, cmd string) ([]byte, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, &probeerr.Error{Kind: probeerr.ErrUnreachable, Err: fmt.Errorf("%w: %w", errSession, err)}
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &limited{w: &stderr, n: 4096}

	rctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-rctx.Done():
		// closing only the session would wait for the peer to confirm, which
		// a peer that went away never does; close the whole connection (the
		// caller drops it anyway) so Run returns now
		client.Close()
		<-done
		if msg := foreignOutput(stdout.Bytes()); msg != "" {
			return nil, &probeerr.Error{Kind: probeerr.ErrRejected, Err: errors.New(msg)}
		}
		return nil, &probeerr.Error{Kind: probeerr.ErrTimeout, Err: fmt.Errorf("command did not finish within %s", RunTimeout)}
	case err = <-done:
	}
	if msg := foreignOutput(stdout.Bytes()); msg != "" {
		return nil, &probeerr.Error{Kind: probeerr.ErrRejected, Err: errors.New(msg)}
	}
	if err != nil {
		var ee *ssh.ExitError
		if errors.As(err, &ee) && stdout.Len() > 0 {
			// the probe tolerates failing sections; a non-zero exit with
			// output is still a usable frame
			return stdout.Bytes(), nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, &probeerr.Error{Kind: probeerr.ErrRejected, Err: errors.New(msg)}
	}
	return stdout.Bytes(), nil
}

// foreignOutput returns the start of stdout when something other than our
// command answered, e.g. a forced command such as cloud images put on root
// ("Please login as the user ubuntu rather than root"). Our probe output
// always starts with an "@@" section header.
func foreignOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" || strings.HasPrefix(s, "@@") {
		return ""
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// limited keeps the first n bytes and discards the rest.
type limited struct {
	w io.Writer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
