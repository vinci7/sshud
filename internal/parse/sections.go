// Package parse turns raw probe output into typed values. It is pure: no I/O,
// no SSH, no clocks. Every parser tolerates missing or malformed input and
// reports it through ok=false / nil instead of failing the whole snapshot.
package parse

import "strings"

// Section is the raw text of one "@@name" block of probe output.
type Section struct {
	Lines []string
	Err   bool // the command for this section failed on the remote side
}

// Split cuts probe output into sections keyed by name. A section whose body
// contains the "@@err" marker is flagged Err; any lines before the first
// header are ignored.
func Split(raw string) map[string]Section {
	out := map[string]Section{}
	var name string
	var cur *Section
	flush := func() {
		if cur != nil {
			out[name] = *cur
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "@@") {
			if line == "@@err" {
				if cur != nil {
					cur.Err = true
				}
				continue
			}
			flush()
			name = line[2:]
			cur = &Section{}
			continue
		}
		if cur != nil {
			cur.Lines = append(cur.Lines, line)
		}
	}
	flush()
	for k, s := range out {
		// drop trailing blank lines so "empty" really means empty
		for len(s.Lines) > 0 && strings.TrimSpace(s.Lines[len(s.Lines)-1]) == "" {
			s.Lines = s.Lines[:len(s.Lines)-1]
		}
		out[k] = s
	}
	return out
}
