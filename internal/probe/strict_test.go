package probe

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// dequote mirrors OpenSSH's opt_dequote: inside a quoted option only \" is
// an escape.
func dequote(t *testing.T, line, opt string) string {
	t.Helper()
	i := strings.Index(line, opt+`="`)
	if i < 0 {
		t.Fatalf("no %s option in %q", opt, line)
	}
	var b strings.Builder
	for j := i + len(opt) + 2; j < len(line); j++ {
		switch {
		case line[j] == '\\' && j+1 < len(line) && line[j+1] == '"':
			b.WriteByte('"')
			j++
		case line[j] == '"':
			return b.String()
		default:
			b.WriteByte(line[j])
		}
	}
	t.Fatal("unterminated option")
	return ""
}

func TestAuthorizedKeysLineRunsTheProbe(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(pub)
	line := AuthorizedKeysLine(sp, "192.0.2.0/24")
	if !strings.HasPrefix(line, `restrict,command="sh -c '`) || !strings.Contains(line, `,from="192.0.2.0/24" ssh-ed25519 `) || !strings.HasSuffix(line, " sshud") {
		t.Fatalf("line = %s", line)
	}
	if strings.Contains(line, "\n") || len(line) > 8192 {
		t.Fatalf("line must be a single line under 8 KB, got %d bytes", len(line))
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	cmd := dequote(t, line, "command")
	for _, level := range []string{"overview", "detail", "rm -rf /tmp/x"} {
		// sshd runs the forced command through the user's shell with the
		// client's request in SSH_ORIGINAL_COMMAND
		c := exec.Command("sh", "-c", cmd)
		c.Env = []string{"PATH=/usr/bin:/bin", "SSH_ORIGINAL_COMMAND=" + level}
		out, _ := c.Output()
		got := string(out)
		if Outdated(got) {
			t.Errorf("%s: fresh line reported as outdated", level)
		}
		if !strings.Contains(got, "\n@@stat1\n") || !strings.Contains(got, "@@osrelease") {
			t.Errorf("%s: output does not look like the probe:\n%.300s", level, got)
		}
		if detail := strings.Contains(got, "@@reboot"); detail != (level == "detail") {
			t.Errorf("%s: detail sections present = %v", level, detail)
		}
	}
}

func TestOutdated(t *testing.T) {
	if !Outdated("@@stat1\ncpu 1 2 3 4\n") || !Outdated("@@version\n000000000000\n@@stat1\n") {
		t.Error("old output not detected")
	}
	if Outdated("@@version\n" + Version() + "\n@@stat1\n") {
		t.Error("current output reported as outdated")
	}
}

// TestInstallCommand runs the generated command through real shells, with a
// fake ssh that runs the remote part under sh in a temporary home, and
// checks that authorized_keys ends up holding the line byte for byte.
func TestInstallCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(pub)
	line := AuthorizedKeysLine(sp, "192.0.2.0/24")
	cmd := InstallCommand(line, "ops", "192.0.2.10", 2222)
	if !strings.HasPrefix(cmd, "ssh -p 2222 -- ops@192.0.2.10 'mkdir -p ~/.ssh && chmod 700 ~/.ssh && ") || strings.Contains(cmd, "\n") {
		t.Fatalf("command = %.120s", cmd)
	}
	bin := t.TempDir()
	// the fake ssh checks its arguments and runs the last one like sshd
	// does, under the same shell as the local side
	fake := "#!/bin/sh\n" +
		`[ "$1 $2 $3 $4" = "-p 2222 -- ops@192.0.2.10" ] && [ $# -eq 5 ] || { echo "bad args: $*" >&2; exit 9; }` + "\n" +
		`exec "$RSH" -c "$5"` + "\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOtherKeyOtherKeyOtherKeyOtherKeyOtherKe me@laptop"
	old := strings.Replace(line, `command="`, `command="echo old; `, 1)
	cases := []struct{ name, before, want string }{
		{"new", "", line + "\n"},
		{"keeps others, no final newline", other, other + "\n" + line + "\n"},
		{"replaces an older sshud line", other + "\n" + old + "\n", other + "\n" + line + "\n"},
	}
	for _, shell := range []string{"sh", "bash", "zsh", "dash"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		for _, c := range cases {
			home := t.TempDir()
			if c.before != "" {
				os.Mkdir(filepath.Join(home, ".ssh"), 0o755)
				os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(c.before), 0o644)
			}
			run := exec.Command(shell, "-c", cmd)
			run.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "RSH=" + shell}
			if out, err := run.CombinedOutput(); err != nil {
				t.Fatalf("%s/%s: %v %s", shell, c.name, err, out)
			}
			ak := filepath.Join(home, ".ssh", "authorized_keys")
			got, _ := os.ReadFile(ak)
			if string(got) != c.want {
				t.Errorf("%s/%s: authorized_keys =\n%q\nwant\n%q", shell, c.name, got, c.want)
			}
			fa, _ := os.Stat(ak)
			fd, _ := os.Stat(filepath.Dir(ak))
			if fa.Mode().Perm() != 0o600 || fd.Mode().Perm() != 0o700 {
				t.Errorf("%s/%s: modes %v %v", shell, c.name, fa.Mode().Perm(), fd.Mode().Perm())
			}
			if _, err := os.Stat(ak + ".sshud"); err == nil {
				t.Errorf("%s/%s: temporary file left behind", shell, c.name)
			}
			// an existing file is backed up as it was; a new one is not
			baks, _ := filepath.Glob(ak + ".bak-*")
			if c.before == "" && len(baks) != 0 {
				t.Errorf("%s/%s: backup of a missing file: %v", shell, c.name, baks)
			} else if c.before != "" {
				if b, _ := os.ReadFile(baks[0]); len(baks) != 1 || string(b) != c.before {
					t.Errorf("%s/%s: backups %v hold %q", shell, c.name, baks, b)
				}
			}
		}
	}
	if got := InstallCommand(line, "o'k", "h", 22); !strings.HasPrefix(got, `ssh -p 22 -- 'o'\''k@h' `) {
		t.Errorf("odd user not quoted: %.40s", got)
	}
}
