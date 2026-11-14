package sshsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/probe"
	"github.com/vinci7/sshud/internal/probeerr"
	"github.com/vinci7/sshud/internal/sshpool"
)

// Credentials log in once to install sshud's line: a password, or a private
// key with an optional passphrase. Nothing stores, logs or returns them.
type Credentials struct{ Password, PrivateKey, Passphrase string }

// Bootstrap logs in to machine id with c, installs sshud's authorized_keys
// line, checks it with sshud's own key and records the host key. Like ssh -o
// StrictHostKeyChecking=accept-new, a first host key is accepted; a recorded
// one must match, which is checked in the key exchange, before c is sent.
func (s *Source) Bootstrap(ctx context.Context, id string, c Credentials) error {
	m, err := s.store.Machine(id)
	if err != nil {
		return &probeerr.Error{Kind: probeerr.ErrInternal, Err: err}
	}
	t, err := s.target(m)
	if err != nil {
		return &probeerr.Error{Kind: probeerr.ErrKey, Err: err}
	}
	auth, offered, err := authMethods(c)
	if err != nil {
		return err
	}
	hostKey, err := s.store.HostKey(id)
	if err != nil {
		return &probeerr.Error{Kind: probeerr.ErrInternal, Err: err}
	}
	client, err := sshpool.Dial(ctx, t, &ssh.ClientConfig{
		User: m.User, Auth: auth, HostKeyAlgorithms: sshpool.HostKeyAlgorithms,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if hostKey != nil && !bytes.Equal(hostKey, k.Marshal()) {
				return fmt.Errorf("%w: it presents %s, not the recorded key", sshpool.ErrHostKeyChanged, fingerprint(k.Marshal()))
			}
			hostKey = k.Marshal()
			return nil
		},
	})
	if pe := (*probeerr.Error)(nil); errors.As(err, &pe) && pe.Kind == probeerr.ErrAuth {
		pe.Kind = probeerr.ErrCredentials
		if c.PrivateKey == "" && !*offered {
			pe.Kind = probeerr.ErrNoPassword
		}
	}
	if err != nil {
		return err
	}
	defer client.Close()
	defer context.AfterFunc(ctx, func() { client.Close() })() // ends a hanging script
	sess, err := client.NewSession()
	if err == nil {
		var stdout, stderr bytes.Buffer
		sess.Stdout, sess.Stderr = &stdout, &stderr
		if err = sess.Run(probe.InstallScript(probe.AuthorizedKeysLine(t.Signer.PublicKey(), m.From))); err != nil {
			err = fmt.Errorf("%v: %s", err, head(stderr.String()+stdout.String(), c.Password))
		}
	}
	if err != nil {
		return &probeerr.Error{Kind: probeerr.ErrInstall, Err: err}
	}
	// sshud's own key, pinned to the same host key, must now get the
	// current probe's output
	pool := sshpool.New(&onceKeys{key: hostKey})
	defer pool.Close()
	got, err := pool.Run(ctx, id, t, "overview")
	if err == nil && probe.Outdated(string(got)) {
		err = errors.New("the machine did not answer with sshud's probe")
	}
	if err != nil {
		return &probeerr.Error{Kind: probeerr.ErrVerify, Err: err}
	}
	return s.store.SetHostKey(id, hostKey)
}

// authMethods turns c into SSH auth methods. *offered becomes true once the
// server lets us try the password (as password or keyboard-interactive),
// which tells a wrong password from a server that only takes keys.
func authMethods(c Credentials) (_ []ssh.AuthMethod, offered *bool, _ error) {
	offered = new(bool)
	if c.PrivateKey == "" {
		password := func() (string, error) { *offered = true; return c.Password, nil }
		answer := func(_, _ string, questions []string, _ []bool) ([]string, error) {
			*offered = true
			return slices.Repeat([]string{c.Password}, len(questions)), nil
		}
		return []ssh.AuthMethod{ssh.PasswordCallback(password), ssh.KeyboardInteractive(answer)}, offered, nil
	}
	// The byte copies are wiped after parsing. Go strings (the request
	// fields) are immutable and cannot be; they become garbage after the request.
	pem, pass := []byte(c.PrivateKey), []byte(c.Passphrase)
	defer clear(pem)
	defer clear(pass)
	signer, err := ssh.ParsePrivateKey(pem)
	if missing := (*ssh.PassphraseMissingError)(nil); errors.As(err, &missing) {
		if signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, pass); err != nil {
			return nil, nil, &probeerr.Error{Kind: probeerr.ErrPassphrase, Err: errors.New("the passphrase does not unlock this private key")}
		}
	}
	if err != nil {
		return nil, nil, &probeerr.Error{Kind: probeerr.ErrCredentials, Err: errors.New("the private key could not be read")}
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, offered, nil
}

// head keeps the first three lines of remote output, with the password
// masked in case the remote side echoed it.
func head(s, password string) string {
	if password != "" {
		s = strings.ReplaceAll(s, password, "***")
	}
	lines := strings.SplitN(strings.TrimSpace(s), "\n", 4)
	return strings.Join(lines[:min(len(lines), 3)], "\n")
}
