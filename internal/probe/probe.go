// Package probe holds the shell script run on monitored machines.
package probe

import _ "embed"

// Script is the POSIX sh probe. It runs as the forced command of the
// strict-mode key in authorized_keys (see ForcedCommand).
//
//go:embed probe.sh
var Script []byte
