package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

var alignment = regexp.MustCompile(`;[ \t]+`)

// OneLine joins the script into a single line: comment and blank lines are
// dropped and commands are separated by "; ". The script is written so that
// every line is a complete command.
func OneLine() string {
	var parts []string
	for _, l := range strings.Split(string(Script), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		parts = append(parts, strings.TrimSuffix(l, ";"))
	}
	// drop the column alignment used in the script file
	return alignment.ReplaceAllString(strings.Join(parts, "; "), "; ")
}

// Version identifies the probe; it changes whenever a command in the script
// does (comments do not count). The forced command prints it first.
func Version() string {
	sum := sha256.Sum256([]byte(OneLine()))
	return hex.EncodeToString(sum[:6])
}

// ForcedCommand is the command sshd runs for the strict-mode key, whatever
// the client asks for. It is run by the account's login shell, so it hands
// the script to sh explicitly. It starts by printing the probe version, so
// sshud can tell when a machine still has an old line.
func ForcedCommand() string {
	return "sh -c " + shellQuote("echo @@version; echo "+Version()+"; "+OneLine())
}

// Outdated reports whether output came from a forced command written for a
// different probe version.
func Outdated(output string) bool {
	return !strings.HasPrefix(output, "@@version\n"+Version()+"\n")
}

// AuthorizedKeysLine is the line to append to ~/.ssh/authorized_keys on a
// monitored machine. restrict disables forwarding, PTYs and user rc files;
// the forced command means the key can only ever print the probe output.
// from, when set, limits the key to those source addresses (a pattern list).
func AuthorizedKeysLine(pub ssh.PublicKey, from string) string {
	opts := `restrict,command="` + quoteOption(ForcedCommand()) + `"`
	if from = strings.TrimSpace(from); from != "" {
		opts += `,from="` + quoteOption(from) + `"`
	}
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	return opts + " " + key + " sshud"
}

// quoteOption escapes a value for a double-quoted authorized_keys option;
// OpenSSH only unescapes \" inside such values.
func quoteOption(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// InstallScript is the shell script that puts line into
// ~/.ssh/authorized_keys. sshud runs it itself when it bootstraps a machine
// with one-time credentials; InstallCommand wraps it for the user's own
// computer. An existing file is first copied to authorized_keys.bak-<time>.
// Any earlier line for the same sshud key is dropped: sshd uses the first
// entry that matches a key, so an appended update would never win. The file
// is rewritten in place (cat, not mv) to keep its owner, mode and SELinux
// label. printf, not echo: dash's echo turns the probe's "\t" into a tab.
func InstallScript(line string) string {
	f := strings.Fields(line)
	key := f[len(f)-2] // "... ssh-ed25519 <key> sshud"
	const ak, tmp = "~/.ssh/authorized_keys", "~/.ssh/authorized_keys.sshud"
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh" +
		" && { [ ! -f " + ak + " ] || cp -p " + ak + " " + ak + ".bak-$(date +%Y%m%d%H%M%S); }" +
		" && touch " + ak + " && chmod 600 " + ak +
		" && { grep -vF " + shellQuote(key) + " " + ak + "; printf '%s\\n' " + shellQuote(line) + "; } > " + tmp +
		" && cat " + tmp + " > " + ak + " && rm " + tmp
}

// InstallCommand is a command the user runs on their own computer, with
// their own SSH key, to run InstallScript on user@host. The script is
// quoted once more for the local shell, and "--" ends ssh's options.
func InstallCommand(line, user, host string, port int) string {
	return "ssh -p " + strconv.Itoa(port) + " -- " + shellWord(user+"@"+host) + " " + shellQuote(InstallScript(line))
}

// shellQuote quotes s as one POSIX shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9@%+=:,./_-]+$`)

// shellWord leaves s alone when no shell would change it.
func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return shellQuote(s)
}
