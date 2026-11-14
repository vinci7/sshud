// Package demo is a hub.Source of fake machines for --demo: it replays the
// sanitized samples and animates their counters so every number on screen
// moves the way a real machine's would. No network is touched.
package demo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/parse"
	"github.com/vinci7/sshud/internal/probeerr"
	"github.com/vinci7/sshud/internal/store"
	"github.com/vinci7/sshud/samples"
)

// Machine is a demo machine as listed to the UI.
type Machine struct {
	ID     string
	Name   string
	Host   string
	sample string
	cpu    float64 // average busy percent
	swing  float64 // +- percent the CPU wanders
	memPct float64 // target used memory percent
	rx, tx float64 // average bytes/s
	fail   probeerr.Kind
	stale  bool // pretend its strict-mode line predates the current probe
}

// Machines is the fixed demo fleet. Addresses use documentation ranges.
var Machines = []Machine{
	{ID: "vps-1", Name: "vps-1", Host: "192.0.2.10", sample: "ubuntu-24.04-x86-vm", cpu: 12, swing: 6, memPct: 41, rx: 180e3, tx: 950e3},
	{ID: "homelab", Name: "homelab", Host: "192.0.2.20", sample: "ubuntu-24.04-x86-docker", cpu: 76, swing: 14, memPct: 64, rx: 4.2e6, tx: 1.1e6},
	{ID: "nas-1", Name: "nas-1", Host: "192.0.2.50", sample: "synology-dsm7", cpu: 23, swing: 8, memPct: 58, rx: 22e6, tx: 3.5e6},
	{ID: "pi-1", Name: "pi-1", Host: "192.0.2.40", sample: "raspbian-12-armv7", cpu: 34, swing: 10, memPct: 92, rx: 310e3, tx: 120e3},
	{ID: "alpine-1", Name: "alpine-1", Host: "192.0.2.30", sample: "alpine-3.20-busybox", cpu: 4, swing: 2, memPct: 18, rx: 12e3, tx: 9e3},
	{ID: "backup-offsite", Name: "backup-offsite", Host: "203.0.113.7", fail: probeerr.ErrTimeout},
	{ID: "ci-runner", Name: "ci-runner", Host: "198.51.100.12", fail: probeerr.ErrAuth},
}

// extraNames fit the sample they reuse: entry i copies Machines[i%5] (cloud
// VM, Docker host, NAS, Raspberry Pi, Alpine box). Keep the length a multiple
// of five so the pairing survives the wrap-around.
var extraNames = []string{
	"build-runner-eu-central-1-spot-instance-0042", "media", "nas-2", "printer-pi", "vpn-gateway",
	"vps-tokyo", "apps-2", "backup-onsite", "garage-pi", "mail-relay",
	"vps-frankfurt", "dev-box", "nas-offsite", "sensor-pi", "dns-2",
}

var extraFails = []probeerr.Kind{probeerr.ErrRefused, probeerr.ErrHostKey, probeerr.ErrRejected, probeerr.ErrUnreachable}

// Fleet returns n demo machines (at least the seven built-in ones). Extra
// machines reuse the samples with other loads and names, including a very
// long one and a few more failure kinds, to exercise large dashboards.
func Fleet(n int) []Machine {
	out := append([]Machine{}, Machines...)
	for i := 0; len(out) < n; i++ {
		base := Machines[i%5] // the five healthy ones
		m := base
		m.Name = extraNames[i%len(extraNames)]
		if i >= len(extraNames) {
			m.Name = fmt.Sprintf("%s-%d", m.Name, i/len(extraNames)+1)
		}
		m.ID = m.Name
		m.Host = fmt.Sprintf("198.51.100.%d", 20+i)
		m.cpu = math.Mod(base.cpu*1.7+float64(i)*13, 85) + 3
		m.swing = math.Min(m.swing, math.Min(m.cpu, 100-m.cpu)/1.5) // keep cpu ± 1.4·swing in 0–100
		m.memPct = math.Mod(base.memPct+float64(i)*11, 90) + 5
		if i%5 == 3 {
			m.fail = extraFails[(i/5)%len(extraFails)]
		}
		m.stale = i == 7
		out = append(out, m)
	}
	return out
}

// Source implements hub.Source.
type Source struct {
	fleet []Machine
	start time.Time
	now   func() time.Time

	mu   sync.Mutex
	base map[string]map[string]parse.Section
}

// New returns a demo source serving fleet.
func New(fleet []Machine) *Source {
	return &Source{fleet: fleet, start: time.Now(), now: time.Now, base: map[string]map[string]parse.Section{}}
}

// List returns the fleet as configured machines.
func (s *Source) List() ([]store.Machine, error) {
	out := make([]store.Machine, 0, len(s.fleet))
	for _, m := range s.fleet {
		out = append(out, store.Machine{ID: m.ID, Name: m.Name, Host: m.Host})
	}
	return out, nil
}

// failMsg is what each failure looks like; HOST is the machine's address.
var failMsg = map[probeerr.Kind]string{
	probeerr.ErrTimeout:     "dial tcp HOST:22: i/o timeout",
	probeerr.ErrAuth:        "ssh: unable to authenticate, attempted methods [none publickey]",
	probeerr.ErrRefused:     "dial tcp HOST:22: connect: connection refused",
	probeerr.ErrHostKey:     "host key does not match the key seen on first connection",
	probeerr.ErrRejected:    "This account is currently not available.",
	probeerr.ErrUnreachable: "dial tcp HOST:22: connect: no route to host",
}

// Probe implements hub.Source.
func (s *Source) Probe(ctx context.Context, id string, level hub.Level) (string, error) {
	m, ok := s.find(id)
	if !ok {
		return "", &probeerr.Error{Kind: probeerr.ErrInternal, Err: fmt.Errorf("unknown demo machine %q", id)}
	}
	// feel like a network round trip, and honour cancellation like a real source
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(150 * time.Millisecond):
	}
	if m.fail != "" {
		return "", &probeerr.Error{Kind: m.fail, Err: errors.New(strings.ReplaceAll(failMsg[m.fail], "HOST", m.Host))}
	}
	base, err := s.sample(m.sample)
	if err != nil {
		return "", &probeerr.Error{Kind: probeerr.ErrInternal, Err: err}
	}
	out := render(base, m, s.now().Sub(s.start).Seconds(), level)
	if m.stale {
		out += "@@outdated\n"
	}
	return out, nil
}

func (s *Source) find(id string) (Machine, bool) {
	for _, m := range s.fleet {
		if m.ID == id {
			return m, true
		}
	}
	return Machine{}, false
}

func (s *Source) sample(name string) (map[string]parse.Section, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.base[name]; ok {
		return b, nil
	}
	raw, err := fs.ReadFile(samples.FS, name+"/detail.txt")
	if err != nil {
		return nil, err
	}
	b := parse.Split(string(raw))
	s.base[name] = b
	return b, nil
}

var overviewOrder = []string{"stat1", "netdev0", "uptime0", "stat2", "meminfo", "loadavg", "uptime", "netdev", "df", "uname", "hostname", "osrelease"}
var detailOrder = []string{"conf", "snmp", "diskstats", "procs", "users", "docker", "cgroup", "thermal", "addr", "updates", "reboot"}

// render rebuilds probe output at time t (seconds since start). Counters are
// integrals of smooth rate functions, so two frames always give sane rates.
func render(base map[string]parse.Section, m Machine, t float64, level hub.Level) string {
	cores := 1
	if st, ok := parse.ParseCPUStat(base["stat1"].Lines); ok && len(st.Cores) > 0 {
		cores = len(st.Cores)
	}
	sec := map[string][]string{}
	sec["stat1"] = statLines(m, cores, t)
	sec["stat2"] = statLines(m, cores, t+0.5)
	sec["meminfo"] = memLines(base["meminfo"].Lines, m, t)
	busy := cpuAt(m, t, -1) / 100 * float64(cores)
	sec["loadavg"] = []string{fmt.Sprintf("%.2f %.2f %.2f 2/%d %d", busy*1.1, busy, busy*0.9, 180+cores*12, 4000+int(t))}
	up, _ := parse.ParseUptime(base["uptime"].Lines)
	sec["uptime"] = []string{fmt.Sprintf("%.2f %.2f", up+t+0.5, (up+t)*float64(cores)*0.9)}
	sec["hostname"] = []string{m.Name} // extra machines reuse samples under other names
	netdev := func(t float64) []string {
		return []string{
			"Inter-|   Receive                                                |  Transmit",
			" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed",
			fmt.Sprintf("  eth0: %d %d 0 0 0 0 0 0 %d %d 0 0 0 0 0 0",
				counter(m.rx, t, m.rx*up), counter(m.rx/900, t, m.rx*up/900), counter(m.tx, t, m.tx*up), counter(m.tx/900, t, m.tx*up/900)),
		}
	}
	sec["netdev0"] = netdev(t)
	sec["uptime0"] = []string{fmt.Sprintf("%.2f 0.00", up+t)}
	sec["netdev"] = netdev(t + 0.5)
	if level == hub.Detail {
		sec["snmp"] = []string{
			"Tcp: OutSegs RetransSegs CurrEstab",
			fmt.Sprintf("Tcp: %d %d %d", counter(m.tx/1200, t, 1e9), counter(m.tx/1200*0.004, t, 4e6), 8+int(m.cpu/4)),
		}
		sec["diskstats"] = []string{fmt.Sprintf(" 8 0 sda %d 0 %d 0 %d 0 %d 0 0 0 0",
			counter(20+m.cpu, t, 2e6), counter((20+m.cpu)*64, t, 9e8), counter(30+m.cpu*2, t, 5e6), counter((30+m.cpu*2)*24, t, 3e9))}
		sec["procs"] = procLines(base["procs"].Lines, m, cores, t)
		if d, ok := base["docker"]; ok && !d.Err {
			sec["docker"], sec["cgroup"] = containerLines(d.Lines, m, cores, t)
		}
	}
	order := overviewOrder
	if level == hub.Detail {
		order = append(append([]string{}, overviewOrder...), detailOrder...)
	}
	var b strings.Builder
	for _, name := range order {
		lines, ok := sec[name]
		if !ok {
			bs, present := base[name]
			if !present {
				continue
			}
			lines = bs.Lines
			if bs.Err {
				lines = append(append([]string{}, lines...), "@@err")
			}
		}
		b.WriteString("@@" + name + "\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// cpuAt is the busy percent of core (or of the whole machine for core < 0).
// Profiles keep cpu ± 1.4·swing inside 0–100, so no clamping is needed and
// cpuJiffies can stay its exact integral.
func cpuAt(m Machine, t float64, core int) float64 {
	phase := float64(core+1) * 1.7
	return m.cpu + m.swing*math.Sin(t/9+phase) + m.swing*0.4*math.Sin(t/2.3+phase*2)
}

// cpuJiffies is ∫₀ᵗ cpuAt dt at 100 Hz.
func cpuJiffies(m Machine, t float64, core int) float64 {
	phase := float64(core+1) * 1.7
	return wave(m.cpu, m.swing, 9, phase, t) + wave(0, m.swing*0.4, 2.3, phase*2, t)
}

// wave is ∫₀ᵗ (a + b·sin(x/p + φ)) dx; it never decreases when |b| ≤ a.
func wave(a, b, p, phase, t float64) float64 {
	return a*t - b*p*(math.Cos(t/p+phase)-math.Cos(phase))
}

// statLines renders /proc/stat whose jiffies integrate cpuAt over time.
func statLines(m Machine, cores int, t float64) []string {
	const hz = 100.0
	line := func(name string, busyJ, totalJ float64) string {
		user, sys := busyJ*0.72, busyJ*0.25
		iowait := busyJ * 0.03
		idle := totalJ - user - sys - iowait
		return fmt.Sprintf("%s %d 0 %d %d %d 0 0 0 0 0", name, int64(user), int64(sys), int64(idle), int64(iowait))
	}
	var all float64
	var lines []string
	for c := 0; c < cores; c++ {
		busy := 1e5 + cpuJiffies(m, t, c)/100*hz
		all += busy
		lines = append(lines, line(fmt.Sprintf("cpu%d", c), busy, t*hz+1e6))
	}
	return append([]string{line("cpu ", all, (t*hz+1e6)*float64(cores))}, lines...)
}

// counter is a cumulative counter growing at about rate per second.
func counter(rate, t, offset float64) uint64 {
	return uint64(offset + wave(rate, rate*0.45, 5, 0, t))
}

func memLines(lines []string, m Machine, t float64) []string {
	mem, _ := parse.ParseMemInfo(lines)
	totalKB := mem.Total / 1024
	pct := math.Min(97, m.memPct+1.5*math.Sin(t/20))
	availKB := uint64(float64(totalKB) * (100 - pct) / 100)
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "MemAvailable:") {
			l = fmt.Sprintf("MemAvailable: %d kB", availKB)
		} else if strings.HasPrefix(l, "MemFree:") {
			l = fmt.Sprintf("MemFree: %d kB", availKB/3)
		}
		out = append(out, l)
	}
	return out
}

// procLines makes the sample's processes burn CPU in proportion to the CPU
// time they had used on the sampled machine, so the ranking stays the real
// one. Together they use 60 % of the machine's busy time.
func procLines(lines []string, m Machine, cores int, t float64) []string {
	procs, _ := parse.ParseProcs(lines)
	var total float64
	for _, p := range procs {
		total += float64(p.Ticks) + 1
	}
	var out []string
	for _, p := range procs {
		share := m.cpu * float64(cores) * 0.6 * (float64(p.Ticks) + 1) / total // percent of one core = ticks/s
		ticks := p.Ticks + uint64(wave(share, share*0.3, 4, float64(p.PID), t))
		out = append(out, fmt.Sprintf("%d %d 0 %d %s", p.PID, ticks, p.RSSPages, p.Comm))
	}
	return out
}

// services replace the sanitized sample's app-1…app-24 with what a home
// server runs. cpu is the share of the machine's busy time; together they
// leave room for the host's own processes (the desktop session, say).
var services = []struct {
	name, image string
	cpu, memMB  float64
}{
	{"photos", "photoprism/photoprism", 0.10, 900},
	{"postgres", "postgres:16", 0.05, 620},
	{"redis", "redis:7-alpine", 0.04, 60},
	{"jellyfin", "jellyfin/jellyfin:10.9", 0.20, 1400},
	{"traefik", "traefik:v3", 0.02, 80},
	{"grafana", "grafana/grafana:11", 0.01, 210},
	{"prometheus", "prom/prometheus:v2.54", 0.03, 480},
	{"node-exporter", "prom/node-exporter:v1.8", 0.01, 25},
	{"nextcloud", "nextcloud:29-apache", 0.02, 350},
	{"mariadb", "mariadb:11", 0.01, 300},
	{"uptime-kuma", "louislam/uptime-kuma:1", 0.01, 140},
	{"vaultwarden", "vaultwarden/server:1.32", 0.002, 45},
	{"gitea", "gitea/gitea:1.22", 0.005, 230},
	{"syncthing", "syncthing/syncthing:1", 0.02, 120},
	{"mosquitto", "eclipse-mosquitto:2", 0.002, 12},
	{"minio", "minio/minio:latest", 0.005, 260},
	{"loki", "grafana/loki:3.1", 0.01, 280},
	{"promtail", "grafana/promtail:3.1", 0.005, 90},
	{"watchtower", "containrrr/watchtower", 0.001, 20},
	{"adminer", "adminer:4", 0.001, 30},
	{"freshrss", "freshrss/freshrss:1.24", 0.01, 90},
	{"memos", "neosmemo/memos:stable", 0.002, 60},
	{"it-tools", "corentinth/it-tools", 0.001, 20},
	{"backup", "restic/restic:0.17", 0, 0}, // a nightly job: shown as exited
}

// containerLines renames the sample's containers and gives the running ones
// cgroup usage.
func containerLines(lines []string, m Machine, cores int, t float64) (docker, cgroup []string) {
	for i, c := range parse.ParseDocker(lines) {
		sv := services[i%len(services)]
		c.Name, c.Image = sv.name, sv.image
		if sv.name == "backup" {
			c.State, c.Status = "exited", "Exited (0) 2 hours ago"
		}
		docker = append(docker, strings.Join([]string{c.ID, c.Name, c.Image, c.State, c.Status}, "\t"))
		if c.State != "running" {
			continue
		}
		share := m.cpu * float64(cores) * sv.cpu // percent of one core
		usec := uint64(1e9 + wave(share, share*0.3, 6, float64(i), t)*1e4)
		mem := uint64(sv.memMB * 1e6)
		cgroup = append(cgroup, fmt.Sprintf("v2 docker-%s.scope %d %d %d", c.ID, usec, mem+8_000_000, 8_000_000))
	}
	return docker, cgroup
}
