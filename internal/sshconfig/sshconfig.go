// Package sshconfig reads the subset of OpenSSH client configuration needed
// to import machines: Host blocks with HostName, User and Port.
// It is deliberately small; Match blocks and Include are reported, not
// followed, because the file may come from another computer.
package sshconfig

import (
	"bufio"
	"strconv"
	"strings"
)

// Entry is one importable host.
type Entry struct {
	Name     string `json:"name"` // the Host alias
	Host     string `json:"host"` // HostName, or the alias when absent
	Port     int    `json:"port"`
	User     string `json:"user"`
	ProxyJmp string `json:"proxyJump,omitempty"`
}

// Result is the parsed file plus anything that was skipped.
type Result struct {
	Entries []Entry  `json:"entries"`
	Skipped []string `json:"skipped,omitempty"` // human-readable notes
}

type block struct {
	patterns []string
	opts     map[string]string
}

// Parse reads ssh_config text. Options follow OpenSSH's "first value wins"
// rule, and "Host *" blocks supply defaults for the hosts above and below.
func Parse(text string) Result {
	var blocks []*block
	cur := &block{opts: map[string]string{}} // options before any Host apply to all
	blocks = append(blocks, cur)
	var res Result
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		key, val, ok := splitLine(sc.Text())
		if !ok {
			continue
		}
		switch key {
		case "host":
			cur = &block{patterns: strings.Fields(val), opts: map[string]string{}}
			blocks = append(blocks, cur)
		case "match":
			cur = &block{opts: map[string]string{}} // options inside Match are ignored
			res.Skipped = append(res.Skipped, "Match "+val)
		case "include":
			res.Skipped = append(res.Skipped, "Include "+val)
		default:
			if _, seen := cur.opts[key]; !seen {
				cur.opts[key] = val
			}
		}
	}

	// value returns the first value of key for alias across matching blocks.
	value := func(alias, key string) string {
		for _, b := range blocks {
			if b.patterns != nil && !matches(b.patterns, alias) {
				continue
			}
			if v, ok := b.opts[key]; ok {
				return v
			}
		}
		return ""
	}

	seen := map[string]bool{}
	for _, b := range blocks {
		for _, alias := range b.patterns {
			if strings.ContainsAny(alias, "*?!") || seen[alias] {
				continue
			}
			seen[alias] = true
			e := Entry{Name: alias, Host: value(alias, "hostname"), User: value(alias, "user"), Port: 22}
			if e.Host == "" {
				e.Host = alias
			}
			e.Host = strings.ReplaceAll(e.Host, "%h", alias)
			if p, err := strconv.Atoi(value(alias, "port")); err == nil {
				e.Port = p
			}
			e.ProxyJmp = value(alias, "proxyjump")
			res.Entries = append(res.Entries, e)
		}
	}
	return res
}

// splitLine returns the lower-cased keyword and its value. Both "Key Value"
// and "Key=Value" forms are accepted; quotes around the value are removed.
func splitLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return "", "", false
	}
	key := strings.ToLower(line[:i])
	val := strings.TrimLeft(line[i:], " \t")
	val = strings.TrimPrefix(val, "=")
	val = strings.Trim(strings.TrimSpace(val), `"`)
	return key, val, val != ""
}

// matches applies ssh_config Host patterns (*, ?, and !negation).
func matches(patterns []string, host string) bool {
	ok := false
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		if neg {
			p = p[1:]
		}
		if glob(p, host) {
			if neg {
				return false
			}
			ok = true
		}
	}
	return ok
}

func glob(p, s string) bool {
	if p == "" {
		return s == ""
	}
	switch p[0] {
	case '*':
		for i := 0; i <= len(s); i++ {
			if glob(p[1:], s[i:]) {
				return true
			}
		}
		return false
	case '?':
		return s != "" && glob(p[1:], s[1:])
	}
	return s != "" && p[0] == s[0] && glob(p[1:], s[1:])
}
