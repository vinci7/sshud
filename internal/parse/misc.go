package parse

import (
	"regexp"
	"strconv"
	"strings"
)

// Conf holds system constants needed to interpret /proc values.
type Conf struct {
	ClkTck   int // jiffies per second
	PageSize int // bytes
}

// ParseConf parses the "conf" section, falling back to Linux defaults.
func ParseConf(lines []string) Conf {
	c := Conf{ClkTck: 100, PageSize: 4096}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		n, err := strconv.Atoi(f[1])
		if err != nil || n <= 0 {
			continue
		}
		switch f[0] {
		case "clk_tck":
			c.ClkTck = n
		case "pagesize":
			c.PageSize = n
		}
	}
	return c
}

// Proc is one process: cumulative CPU time in jiffies and resident pages.
type Proc struct {
	PID      int
	Ticks    uint64 // utime + stime
	RSSPages uint64
	Comm     string
}

// ParseProcs parses the "procs" section: "pid utime stime rss comm".
func ParseProcs(lines []string) ([]Proc, bool) {
	var out []Proc
	for _, l := range lines {
		f := strings.SplitN(l, " ", 5)
		if len(f) < 5 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ut, e2 := strconv.ParseUint(f[1], 10, 64)
		st, e3 := strconv.ParseUint(f[2], 10, 64)
		rss, e4 := strconv.ParseInt(f[3], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		if rss < 0 {
			rss = 0
		}
		out = append(out, Proc{PID: pid, Ticks: ut + st, RSSPages: uint64(rss), Comm: f[4]})
	}
	return out, len(out) > 0
}

// ParseUsers returns the distinct login names from "who".
func ParseUsers(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) == 0 || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		out = append(out, f[0])
	}
	return out
}

// Container is one row of "docker ps -a".
type Container struct {
	ID, Name, Image, State, Status string
}

// Running reports whether the container is up.
func (c Container) Running() bool { return c.State == "running" }

// ParseDocker parses tab separated "id name image state status" rows.
func ParseDocker(lines []string) []Container {
	var out []Container
	for _, l := range lines {
		f := strings.Split(l, "\t")
		if len(f) < 5 {
			continue
		}
		out = append(out, Container{ID: f[0], Name: f[1], Image: f[2], State: f[3], Status: f[4]})
	}
	return out
}

// Temp is one thermal zone reading.
type Temp struct {
	Zone    string
	Celsius float64
}

// ParseThermal parses "<type> <millidegrees>" lines, skipping unreadable zones.
func ParseThermal(lines []string) []Temp {
	var out []Temp
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		v, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			continue
		}
		if v > 1000 || v < -1000 { // millidegrees; a few drivers report whole degrees
			v /= 1000
		}
		out = append(out, Temp{Zone: f[0], Celsius: v})
	}
	return out
}

// Updates is the pending package count reported by the OS.
type Updates struct {
	Total, Security int
}

var (
	reUpdTotal = regexp.MustCompile(`(\d+) updates? can be (?:applied|installed) immediately`)
	reUpdSec   = regexp.MustCompile(`(\d+) of these updates (?:is a|are) (?:standard )?security updates?`)
)

// ParseUpdates parses Ubuntu's update-notifier summary.
func ParseUpdates(lines []string) (Updates, bool) {
	text := strings.Join(lines, "\n")
	m := reUpdTotal.FindStringSubmatch(text)
	if m == nil {
		return Updates{}, false
	}
	var u Updates
	u.Total, _ = strconv.Atoi(m[1])
	if s := reUpdSec.FindStringSubmatch(text); s != nil {
		u.Security, _ = strconv.Atoi(s[1])
	}
	return u, true
}

// ParseReboot parses the reboot-required flag ("yes" / "no").
func ParseReboot(lines []string) (bool, bool) {
	s, ok := ParseFirstLine(lines)
	if !ok {
		return false, false
	}
	return s == "yes", s == "yes" || s == "no"
}

// CgroupUsage is the resource use of one container's cgroup.
type CgroupUsage struct {
	CPUUsec uint64 // cumulative CPU time in microseconds
	Memory  uint64 // bytes, excluding inactive page cache (what docker stats shows)
}

// ParseCgroups parses the "cgroup" section, keyed by full container ID.
func ParseCgroups(lines []string) map[string]CgroupUsage {
	out := map[string]CgroupUsage{}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 4 {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(f[1], "docker-"), ".scope")
		cpu, e1 := strconv.ParseUint(f[2], 10, 64)
		mem, e2 := strconv.ParseUint(f[3], 10, 64)
		if e1 != nil || e2 != nil || len(id) < 12 {
			continue
		}
		if f[0] == "v1" {
			cpu /= 1000 // cpuacct.usage is in nanoseconds
		}
		if len(f) > 4 {
			if inactive, err := strconv.ParseUint(f[4], 10, 64); err == nil && inactive < mem {
				mem -= inactive
			}
		}
		out[id] = CgroupUsage{CPUUsec: cpu, Memory: mem}
	}
	return out
}
