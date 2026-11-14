// Package probeerr classifies why reaching a machine failed, so the UI can
// say something useful. It is shared by the SSH code that detects failures
// and the hub and server that report them.
package probeerr

// Kind classifies a failure.
type Kind string

const (
	ErrUnreachable Kind = "unreachable" // network down, DNS, no route
	ErrTimeout     Kind = "timeout"
	ErrRefused     Kind = "refused"  // TCP connection refused
	ErrAuth        Kind = "auth"     // key rejected
	ErrKey         Kind = "key"      // local private key missing or unreadable
	ErrHostKey     Kind = "hostkey"  // host key changed since first contact
	ErrRejected    Kind = "rejected" // forced command did not match (strict mode)
	ErrUnsupported Kind = "unsupported"
	ErrInternal    Kind = "internal"
)

// Bootstrap failures, when sshud installs its line with one-time credentials.
const (
	ErrCredentials Kind = "credentials" // password or key rejected
	ErrNoPassword  Kind = "nopassword"  // neither password nor keyboard-interactive offered
	ErrPassphrase  Kind = "passphrase"  // the private key could not be decrypted
	ErrInstall     Kind = "install"     // the install script failed
	ErrVerify      Kind = "verify"      // installed, but sshud's key still does not work
)

// Error is a classified failure.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string { return string(e.Kind) + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }
