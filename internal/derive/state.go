package derive

import (
	"sort"
	"time"

	"github.com/vinci7/sshud/internal/parse"
)

// TopN is how many processes are kept per sort order (CPU and memory).
const TopN = 5

// RetransWindow is how many frames the TCP retransmission rate spans. A
// 2-second frame on a quiet host carries a few dozen segments, so one
// retransmit would read as several percent; about a minute is steadier.
const RetransWindow = 30

// State remembers the counters of the previous frame of one machine.
// It is not safe for concurrent use.
type State struct {
	at       time.Time
	netRx    uint64
	netTx    uint64
	tcp      []parse.TCPStat // last RetransWindow frames, oldest first
	disk     *ioTotals
	procs    map[int]procKey
	cgroups  map[string]uint64 // container ID -> cumulative CPU µs
	hasFrame bool
}

type ioTotals struct{ reads, writes, rsec, wsec uint64 }

type procKey struct {
	comm  string
	ticks uint64
}

// sections every probe level prints, and detail-only ones
var (
	overviewSections = []string{"stat1", "stat2", "meminfo", "loadavg", "uptime", "netdev", "df", "uname", "hostname", "osrelease"}
	detailSections   = []string{"snmp", "diskstats", "procs", "users", "docker", "addr", "updates"}
)

// Update derives a Snapshot from one probe output taken at now.
func (s *State) Update(sec map[string]parse.Section, now time.Time) Snapshot {
	snap := Snapshot{Time: ms(now)}
	_, snap.ProbeOutdated = sec["outdated"]
	failed := map[string]bool{}
	lines := func(name string) []string {
		x, ok := sec[name]
		if ok && x.Err {
			failed[name] = true
		}
		return x.Lines
	}
	// fail marks a section that was present but could not be parsed.
	fail := func(name string) {
		if _, ok := sec[name]; ok {
			failed[name] = true
		}
	}
	dt := now.Sub(s.at).Seconds()
	havePrev := s.hasFrame && dt > 0

	// host
	snap.Host.Hostname, _ = parse.ParseFirstLine(lines("hostname"))
	if o, ok := parse.ParseOSRelease(lines("osrelease")); ok {
		snap.Host.OS = o.PrettyName
	}
	if u, ok := parse.ParseUname(lines("uname")); ok {
		snap.Host.Kernel, snap.Host.Arch = u.Release, u.Arch
	}
	snap.Host.Uptime, _ = parse.ParseUptime(lines("uptime"))

	// cpu: two reads 0.5 s apart inside one probe
	a, ok1 := parse.ParseCPUStat(lines("stat1"))
	b, ok2 := parse.ParseCPUStat(lines("stat2"))
	if ok1 && ok2 {
		snap.CPU = cpuBetween(a, b)
		snap.Host.Cores = len(b.Cores)
	} else {
		fail("stat2")
	}

	if m, ok := parse.ParseMemInfo(lines("meminfo")); ok {
		snap.Mem = memFrom(m)
	} else {
		fail("meminfo")
	}
	if la, ok := parse.ParseLoadAvg(lines("loadavg")); ok {
		snap.Load = &Load{Load1: la.Load1, Load5: la.Load5, Load15: la.Load15, Procs: la.Total}
	} else {
		fail("loadavg")
	}
	if mounts, ok := parse.ParseDF(lines("df")); ok {
		for _, m := range mounts {
			snap.Disks = append(snap.Disks, Disk{Path: m.Path, Device: m.Device, Size: m.Size, Used: m.Used, Percent: m.UsedPercent()})
		}
	} else {
		fail("df")
	}

	if ifaces, ok := parse.ParseNetDev(lines("netdev")); ok {
		rx, tx := physicalTotals(ifaces)
		snap.Net = &Net{RxTotal: rx, TxTotal: tx}
		if havePrev && rx >= s.netRx && tx >= s.netTx {
			r, t := float64(rx-s.netRx)/dt, float64(tx-s.netTx)/dt
			snap.Net.RxRate, snap.Net.TxRate = &r, &t
		} else if r, t, ok := netInProbe(sec, rx, tx); ok {
			snap.Net.RxRate, snap.Net.TxRate = &r, &t
		}
		s.netRx, s.netTx = rx, tx
	} else {
		fail("netdev")
	}

	// detail level
	if x, ok := sec["snmp"]; ok && !x.Err {
		if t, ok := parse.ParseSNMP(x.Lines); ok {
			snap.TCP = &TCP{Established: t.CurrEstab}
			if n := len(s.tcp); n > 0 && (t.OutSegs < s.tcp[n-1].OutSegs || t.RetransSegs < s.tcp[n-1].RetransSegs) {
				s.tcp = nil // counters reset (reboot)
			}
			if len(s.tcp) > 0 {
				old := s.tcp[0]
				if t.OutSegs > old.OutSegs {
					p := float64(t.RetransSegs-old.RetransSegs) * 100 / float64(t.OutSegs-old.OutSegs)
					snap.TCP.RetransPercent = &p
				}
			}
			s.tcp = append(s.tcp, t)
			if len(s.tcp) > RetransWindow {
				s.tcp = s.tcp[1:]
			}
		} else {
			fail("snmp")
		}
	}
	if x, ok := sec["diskstats"]; ok && !x.Err {
		if disks, ok := parse.ParseDiskstats(x.Lines); ok {
			var cur ioTotals
			for _, d := range disks {
				cur.reads += d.Reads
				cur.writes += d.Writes
				cur.rsec += d.ReadSectors
				cur.wsec += d.WriteSectors
			}
			if p := s.disk; havePrev && p != nil && cur.reads >= p.reads && cur.writes >= p.writes && cur.rsec >= p.rsec && cur.wsec >= p.wsec {
				snap.DiskIO = &DiskIO{
					ReadRate:  float64(cur.rsec-p.rsec) * 512 / dt,
					WriteRate: float64(cur.wsec-p.wsec) * 512 / dt,
					ReadIOPS:  float64(cur.reads-p.reads) / dt,
					WriteIOPS: float64(cur.writes-p.writes) / dt,
				}
			}
			s.disk = &cur
		}
	}
	// process and container CPU is a share of the whole machine, like the
	// CPU ring: CPU time used between frames over what all cores offered
	capacity := dt * float64(snap.Host.Cores)
	if x, ok := sec["procs"]; ok && !x.Err {
		conf := parse.ParseConf(lines("conf"))
		if procs, ok := parse.ParseProcs(x.Lines); ok {
			snap.Procs = s.topProcs(procs, conf, capacity, havePrev)
		} else {
			fail("procs")
		}
	}
	if x, ok := sec["users"]; ok && !x.Err {
		snap.Users = append([]string{}, parse.ParseUsers(x.Lines)...)
	}
	if x, ok := sec["docker"]; ok && !x.Err {
		usage := parse.ParseCgroups(sec["cgroup"].Lines)
		next := make(map[string]uint64, len(usage))
		snap.Containers = []Container{}
		for _, c := range parse.ParseDocker(x.Lines) {
			ct := Container{Name: c.Name, Image: c.Image, State: c.State, Status: c.Status, Running: c.Running()}
			if u, ok := usage[c.ID]; ok && ct.Running {
				mem := u.Memory
				ct.Mem = &mem
				if prev, ok := s.cgroups[c.ID]; ok && havePrev && capacity > 0 && u.CPUUsec >= prev {
					p := float64(u.CPUUsec-prev) / 1e4 / capacity // µs → percent
					ct.CPU = &p
				}
				next[c.ID] = u.CPUUsec
			}
			snap.Containers = append(snap.Containers, ct)
		}
		s.cgroups = next
	}
	for _, t := range parse.ParseThermal(lines("thermal")) {
		snap.Temps = append(snap.Temps, Temp{Zone: t.Zone, Celsius: t.Celsius})
	}
	if x, ok := sec["updates"]; ok && !x.Err {
		if u, ok := parse.ParseUpdates(x.Lines); ok {
			snap.Updates = &Updates{Total: u.Total, Security: u.Security}
		}
	}
	if r, ok := parse.ParseReboot(lines("reboot")); ok {
		snap.Reboot = &r
	}
	if addrs, ok := parse.ParseAddr(lines("addr")); ok {
		for _, a := range addrs {
			snap.Addrs = append(snap.Addrs, a.CIDR)
		}
	}

	for _, n := range append(overviewSections, detailSections...) {
		if failed[n] || sec[n].Err {
			snap.Failed = append(snap.Failed, n)
		}
	}
	s.at = now
	s.hasFrame = true
	return snap
}

// netInProbe derives a rate from the counters the probe read before its
// pause, timed by /proc/uptime, so the very first frame has network rates.
func netInProbe(sec map[string]parse.Section, rx, tx uint64) (float64, float64, bool) {
	t0, ok0 := parse.ParseUptime(sec["uptime0"].Lines)
	t1, ok1 := parse.ParseUptime(sec["uptime"].Lines)
	ifaces, ok := parse.ParseNetDev(sec["netdev0"].Lines)
	if !ok || !ok0 || !ok1 || t1-t0 < 0.1 {
		return 0, 0, false
	}
	rx0, tx0 := physicalTotals(ifaces)
	if rx < rx0 || tx < tx0 {
		return 0, 0, false
	}
	dt := t1 - t0
	return float64(rx-rx0) / dt, float64(tx-tx0) / dt, true
}

// physicalTotals sums the byte counters of real interfaces; loopback,
// bridges and veths would count the same traffic twice.
func physicalTotals(ifaces []parse.NetIface) (rx, tx uint64) {
	for _, n := range ifaces {
		if !n.Virtual() {
			rx += n.RxBytes
			tx += n.TxBytes
		}
	}
	return rx, tx
}

func cpuBetween(a, b parse.CPUStat) *CPU {
	c := &CPU{}
	c.Percent, c.User, c.System, c.IOWait, c.Steal = cpuSplit(a.All, b.All)
	if len(a.Cores) == len(b.Cores) {
		for i := range b.Cores {
			p, _, _, _, _ := cpuSplit(a.Cores[i], b.Cores[i])
			c.Cores = append(c.Cores, p)
		}
	}
	return c
}

// cpuSplit returns busy, user, system, iowait and steal percentages.
// Busy excludes idle and iowait, matching top's "100 - id - wa".
func cpuSplit(a, b parse.CPUTimes) (busy, user, sys, iowait, steal float64) {
	if b.Total() <= a.Total() {
		return 0, 0, 0, 0, 0
	}
	tot := float64(b.Total() - a.Total())
	d := func(x, y uint64) float64 {
		if y < x {
			return 0
		}
		return float64(y-x) * 100 / tot
	}
	user = d(a.User+a.Nice, b.User+b.Nice)
	sys = d(a.System+a.IRQ+a.SoftIRQ, b.System+b.IRQ+b.SoftIRQ)
	iowait = d(a.IOWait, b.IOWait)
	steal = d(a.Steal, b.Steal)
	busy = 100 - d(a.Idle, b.Idle) - iowait
	if busy < 0 {
		busy = 0
	}
	return
}

func memFrom(m parse.MemInfo) *Mem {
	cache := m.Buffers + m.Cached + m.SReclaimable
	if m.Shmem < cache {
		cache -= m.Shmem // shared memory sits in Cached but cannot be dropped
	}
	avail := m.Available
	if !m.HasAvailable {
		avail = m.Free + cache
	}
	if avail > m.Total {
		avail = m.Total
	}
	used := m.Total - avail
	free := m.Free
	if used+free > m.Total {
		free = m.Total - used
	}
	if used+free+cache > m.Total {
		cache = m.Total - used - free
	}
	out := &Mem{Total: m.Total, Used: used, Cache: cache, Free: free, SwapTotal: m.SwapTotal}
	if m.Total > 0 {
		out.Percent = float64(used) * 100 / float64(m.Total)
	}
	if m.SwapTotal >= m.SwapFree {
		out.SwapUsed = m.SwapTotal - m.SwapFree
	}
	return out
}

func (s *State) topProcs(procs []parse.Proc, conf parse.Conf, capacity float64, havePrev bool) []Proc {
	type row struct {
		Proc
		cpu float64
	}
	rows := make([]row, 0, len(procs))
	next := make(map[int]procKey, len(procs))
	for _, p := range procs {
		if p.RSSPages == 0 {
			// kernel threads (kworker, migration, …) have no user memory;
			// top lists of real tools hide them too
			continue
		}
		r := row{Proc: Proc{PID: p.PID, Name: p.Comm, RSS: p.RSSPages * uint64(conf.PageSize)}}
		if prev, ok := s.procs[p.PID]; havePrev && capacity > 0 && ok && prev.comm == p.Comm && p.Ticks >= prev.ticks {
			r.cpu = float64(p.Ticks-prev.ticks) * 100 / float64(conf.ClkTck) / capacity
			c := r.cpu
			r.CPU = &c
		}
		next[p.PID] = procKey{comm: p.Comm, ticks: p.Ticks}
		rows = append(rows, r)
	}
	s.procs = next

	pick := map[int]bool{}
	var out []Proc
	add := func(less func(i, j int) bool) {
		sort.SliceStable(rows, less)
		for i := 0; i < len(rows) && i < TopN; i++ {
			if !pick[rows[i].PID] {
				pick[rows[i].PID] = true
				out = append(out, rows[i].Proc)
			}
		}
	}
	if havePrev {
		add(func(i, j int) bool { return rows[i].cpu > rows[j].cpu })
	}
	add(func(i, j int) bool { return rows[i].RSS > rows[j].RSS })
	return out
}
