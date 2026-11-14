// Package sshsource is the hub.Source for real machines: it looks the
// machine up in the store and runs the probe over sshpool with the
// strict-mode key.
package sshsource

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/parse"
	"github.com/vinci7/sshud/internal/probe"
	"github.com/vinci7/sshud/internal/probeerr"
	"github.com/vinci7/sshud/internal/sshpool"
	"github.com/vinci7/sshud/internal/store"
)

// Source implements hub.Source.
type Source struct {
	store   *store.Store
	pool    *sshpool.Pool
	dataDir string

	mu     sync.Mutex
	signer ssh.Signer // the strict-mode key, once loaded
}

// New returns a source backed by st. The strict-mode key lives in dataDir.
func New(st *store.Store, dataDir string) *Source {
	return &Source{store: st, pool: sshpool.New(st), dataDir: dataDir}
}

// StrictKeyFile is the name of the dedicated strict-mode private key.
const StrictKeyFile = "strict_ed25519"

// StrictSigner returns the strict-mode key, creating it (mode 0600) on first
// use. Errors name the file but never include key material.
func (s *Source) StrictSigner() (ssh.Signer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signer != nil {
		return s.signer, nil
	}
	path := filepath.Join(s.dataDir, StrictKeyFile)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		block, err := ssh.MarshalPrivateKey(priv, "sshud")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
			return nil, fmt.Errorf("create strict-mode key: %w", err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("strict-mode key %s: %w", path, errors.Unwrap(err))
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("strict-mode key %s could not be parsed", path)
	}
	s.signer = signer
	return signer, nil
}

// Close drops all SSH connections.
func (s *Source) Close() { s.pool.Close() }

// CloseIdle drops connections nobody used recently.
func (s *Source) CloseIdle() { s.pool.CloseIdle(time.Now()) }

// target returns how to reach m with the strict-mode key. The forced
// command ignores what we send except for the level, so the level is the
// whole command.
func (s *Source) target(m store.Machine) (sshpool.Target, error) {
	signer, err := s.StrictSigner()
	return sshpool.Target{Host: m.Host, Port: m.Port, User: m.User, Signer: signer}, err
}

// Probe implements hub.Source.
func (s *Source) Probe(ctx context.Context, id string, level hub.Level) (string, error) {
	m, err := s.store.Machine(id)
	if err != nil {
		return "", &probeerr.Error{Kind: probeerr.ErrInternal, Err: err}
	}
	t, err := s.target(m)
	if err != nil {
		return "", &probeerr.Error{Kind: probeerr.ErrKey, Err: err}
	}
	out, err := s.pool.Run(ctx, id, t, level.String())
	if err != nil {
		return "", err
	}
	if probe.Outdated(string(out)) {
		// the machine still runs an older forced command: show its numbers
		// (sections it lacks stay empty), but flag the line as stale
		return string(out) + "\n@@outdated\n", nil
	}
	return string(out), nil
}

// TestResult is the outcome of a one-off connection test.
type TestResult struct {
	OK          bool          `json:"ok"`
	Kind        probeerr.Kind `json:"kind,omitempty"`
	Message     string        `json:"message,omitempty"`
	Hostname    string        `json:"hostname,omitempty"`
	OS          string        `json:"os,omitempty"`
	Fingerprint string        `json:"fingerprint,omitempty"` // SHA256 of the host key, as ssh-keygen -l shows it
	Millis      int64         `json:"ms"`
}

// Test connects to m once, runs the overview probe and reports what it
// found. It neither uses nor records the stored host key, so it works for
// machines that are not saved yet; the fingerprint is returned for the user
// to compare.
func (s *Source) Test(ctx context.Context, m store.Machine) TestResult {
	start := time.Now()
	res := TestResult{}
	target, err := s.target(m)
	if err != nil {
		res.Kind, res.Message = probeerr.ErrKey, err.Error()
		return res
	}
	keys := &onceKeys{}
	pool := sshpool.New(keys)
	defer pool.Close()
	out, err := pool.Run(ctx, "test", target, "overview")
	var pe *probeerr.Error
	if errors.As(err, &pe) && pe.Kind == probeerr.ErrTimeout && ctx.Err() == nil {
		// one lost packet should not make a correct form look wrong
		out, err = pool.Run(ctx, "test", target, "overview")
	}
	res.Millis = time.Since(start).Milliseconds()
	res.Fingerprint = fingerprint(keys.key)
	if err != nil {
		res.Kind, res.Message = probeerr.ErrInternal, err.Error()
		if errors.As(err, &pe) {
			res.Kind, res.Message = pe.Kind, pe.Err.Error()
		}
		return res
	}
	sec := parse.Split(string(out))
	res.Hostname, _ = parse.ParseFirstLine(sec["hostname"].Lines)
	if o, ok := parse.ParseOSRelease(sec["osrelease"].Lines); ok {
		res.OS = o.PrettyName
	}
	if _, ok := parse.ParseCPUStat(sec["stat2"].Lines); !ok {
		res.Kind, res.Message = probeerr.ErrUnsupported, "connected, but the probe output could not be read (is /proc available?)"
		return res
	}
	res.OK = true
	return res
}

// Trust connects to machine id once and returns the fingerprints of its
// recorded host key and of the one it presents now. If the presented one is
// the confirmed one, it becomes the recorded key.
func (s *Source) Trust(ctx context.Context, id, confirmed string) (recorded, presented string, err error) {
	m, err := s.store.Machine(id)
	if err != nil {
		return "", "", err
	}
	old, err := s.store.HostKey(id)
	if err != nil {
		return "", "", err
	}
	presented, key, err := s.HostKey(ctx, m.Host, m.Port)
	if err == nil && presented == confirmed {
		err = s.store.SetHostKey(id, key)
	}
	return fingerprint(old), presented, err
}

var errSeen = errors.New("host key seen")

// HostKey returns the fingerprint and wire form of the host key that
// host:port presents. It stops right after the key exchange, before
// authentication, so it sends no key or password at all.
func (s *Source) HostKey(ctx context.Context, host string, port int) (string, []byte, error) {
	var key []byte
	_, err := sshpool.Dial(ctx, sshpool.Target{Host: host, Port: port}, &ssh.ClientConfig{
		HostKeyAlgorithms: sshpool.HostKeyAlgorithms,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = k.Marshal()
			return errSeen
		},
	})
	if key == nil {
		return "", nil, err
	}
	return fingerprint(key), key, nil
}

// fingerprint shows a host key in wire format the way ssh-keygen -l does.
func fingerprint(key []byte) string {
	pk, err := ssh.ParsePublicKey(key)
	if err != nil {
		return ""
	}
	return keyType(pk.Type()) + " " + ssh.FingerprintSHA256(pk)
}

// onceKeys accepts and remembers the first host key a server presents, or
// only the one it was created with.
type onceKeys struct{ key []byte }

func (k *onceKeys) HostKey(string) ([]byte, error) { return k.key, nil }
func (k *onceKeys) SetHostKey(_ string, key []byte) error {
	k.key = key
	return nil
}

// keyType names a host key type the way ssh-keygen -l does.
func keyType(t string) string {
	switch {
	case t == ssh.KeyAlgoED25519:
		return "ED25519"
	case strings.HasPrefix(t, "ecdsa"):
		return "ECDSA"
	case strings.HasPrefix(t, "ssh-rsa"), strings.HasPrefix(t, "rsa"):
		return "RSA"
	}
	return t
}
