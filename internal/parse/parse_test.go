package parse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, name string) map[string]Section {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "samples", name, "detail.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return Split(string(b))
}

func TestSplit(t *testing.T) {
	s := Split("junk\n@@a\nx\ny\n\n@@b\n@@err\n@@c\n")
	if got := s["a"].Lines; len(got) != 2 || got[1] != "y" {
		t.Errorf("a = %q", got)
	}
	if !s["b"].Err || len(s["b"].Lines) != 0 {
		t.Errorf("b = %+v", s["b"])
	}
	if _, ok := s["c"]; !ok || s["c"].Err {
		t.Errorf("c = %+v", s["c"])
	}
	if _, ok := s["missing"]; ok {
		t.Error("unexpected section")
	}
}

// Expected values below were read by hand from the raw sample files.
func TestUbuntuDockerHost(t *testing.T) {
	s := load(t, "ubuntu-24.04-x86-docker")

	st, ok := ParseCPUStat(s["stat1"].Lines)
	if !ok || len(st.Cores) != 16 || st.All.User != 87464660 || st.All.Nice != 244873224 {
		t.Errorf("stat = %v %+v cores=%d", ok, st.All, len(st.Cores))
	}
	m, ok := ParseMemInfo(s["meminfo"].Lines)
	if !ok || m.Total != 20240116*1024 || m.Available != 12930236*1024 || m.SwapFree != 4859320*1024 || !m.HasAvailable {
		t.Errorf("mem = %+v", m)
	}
	nets, _ := ParseNetDev(s["netdev"].Lines)
	var phys []string
	for _, n := range nets {
		if !n.Virtual() {
			phys = append(phys, n.Name)
		}
		if n.Name == "enp1s0" && (n.RxBytes != 10166514547885 || n.TxBytes != 389839121999) {
			t.Errorf("enp1s0 = %+v", n)
		}
	}
	if strings.Join(phys, ",") != "enp1s0,wlp2s0,tailscale0" {
		t.Errorf("physical ifaces = %v", phys)
	}
	mounts, _ := ParseDF(s["df"].Lines)
	var paths []string
	for _, mt := range mounts {
		paths = append(paths, mt.Path)
	}
	if strings.Join(paths, ",") != "/,/mnt/photo,/mnt/nas" {
		t.Errorf("mounts = %v", paths)
	}
	if mounts[0].Size != 959218776*1024 || int(mounts[0].UsedPercent()) != 16 {
		t.Errorf("root = %+v %.1f", mounts[0], mounts[0].UsedPercent())
	}
	disks, _ := ParseDiskstats(s["diskstats"].Lines)
	if len(disks) != 1 || disks[0].Name != "nvme0n1" {
		t.Errorf("disks = %+v", disks)
	}
	tcp, ok := ParseSNMP(s["snmp"].Lines)
	if !ok || tcp.RetransSegs != 569046 || tcp.OutSegs != 1020876753 {
		t.Errorf("tcp = %+v", tcp)
	}
	procs, _ := ParseProcs(s["procs"].Lines)
	if len(procs) < 500 || procs[0].PID != 1 || procs[0].Comm != "systemd" || procs[0].Ticks != 123229+87760 {
		t.Errorf("procs = %d %+v", len(procs), procs[0])
	}
	cs := ParseDocker(s["docker"].Lines)
	if len(cs) != 24 || !cs[0].Running() || cs[0].Name != "app-1" {
		t.Errorf("docker = %d %+v", len(cs), cs)
	}
	if u := ParseUsers(s["users"].Lines); len(u) != 1 || u[0] != "alice" {
		t.Errorf("users = %v", u)
	}
	upd, ok := ParseUpdates(s["updates"].Lines)
	if !ok || upd.Total != 142 || upd.Security != 22 {
		t.Errorf("updates = %+v", upd)
	}
	if temps := ParseThermal(s["thermal"].Lines); len(temps) != 1 || temps[0].Celsius != 20 {
		t.Errorf("thermal = %+v", temps)
	}
	if c := ParseConf(s["conf"].Lines); c.ClkTck != 100 || c.PageSize != 4096 {
		t.Errorf("conf = %+v", c)
	}
}

func TestUbuntuVM(t *testing.T) {
	s := load(t, "ubuntu-24.04-x86-vm")

	st, _ := ParseCPUStat(s["stat2"].Lines)
	if len(st.Cores) != 2 || st.All.Steal == 0 {
		t.Errorf("stat = %+v", st)
	}
	m, _ := ParseMemInfo(s["meminfo"].Lines)
	if m.Total != 974968*1024 || m.SwapTotal != 0 {
		t.Errorf("mem = %+v", m)
	}
	mounts, _ := ParseDF(s["df"].Lines)
	if len(mounts) != 2 || mounts[0].Path != "/" || mounts[1].Path != "/boot" {
		t.Errorf("mounts = %+v", mounts)
	}
	disks, _ := ParseDiskstats(s["diskstats"].Lines)
	if len(disks) != 1 || disks[0].Name != "sda" {
		t.Errorf("disks = %+v", disks)
	}
	if !s["docker"].Err {
		t.Error("docker should be flagged as failed on a host without docker")
	}
	addrs, _ := ParseAddr(s["addr"].Lines)
	if len(addrs) != 1 || addrs[0].CIDR != "192.0.2.10/24" || addrs[0].Iface != "ens3" {
		t.Errorf("addrs = %+v", addrs)
	}
	u, _ := ParseUname(s["uname"].Lines)
	o, _ := ParseOSRelease(s["osrelease"].Lines)
	if u.Arch != "x86_64" || o.ID != "ubuntu" || o.VersionID != "24.04" {
		t.Errorf("uname=%+v os=%+v", u, o)
	}
	if upd, ok := ParseUpdates(s["updates"].Lines); !ok || upd.Total != 0 {
		t.Errorf("updates = %+v %v", upd, ok)
	}
	if r, ok := ParseReboot(s["reboot"].Lines); !ok || r {
		t.Errorf("reboot = %v %v", r, ok)
	}
	if h, _ := ParseFirstLine(s["hostname"].Lines); h != "web-1" {
		t.Errorf("hostname = %q", h)
	}
}

func TestEdgeCases(t *testing.T) {
	// old kernel: no MemAvailable, no steal column
	m, ok := ParseMemInfo([]string{"MemTotal: 1000 kB", "MemFree: 100 kB"})
	if !ok || m.HasAvailable || m.Total != 1024000 {
		t.Errorf("mem = %+v", m)
	}
	st, ok := ParseCPUStat([]string{"cpu  1 2 3 4"})
	if !ok || st.All.Idle != 4 || st.All.Total() != 10 {
		t.Errorf("stat = %+v", st)
	}
	// process name with spaces and parentheses
	p, _ := ParseProcs([]string{"42 1 2 -1 tmux: server (x)"})
	if len(p) != 1 || p[0].Comm != "tmux: server (x)" || p[0].RSSPages != 0 {
		t.Errorf("procs = %+v", p)
	}
	// busybox df with a mount path containing a space
	mts, _ := ParseDF([]string{
		"Filesystem           1024-blocks    Used Available Capacity Mounted on",
		"/dev/root              30000000 15000000  15000000  50% /",
		"/dev/sdb1                 1000      900       100  90% /mnt/My Disk",
		// a snap on CentOS: not under /snap/, but on a loop device and always 100%
		"/dev/loop0                 56960    56960         0 100% /var/lib/snapd/snap/core18/2829",
	})
	if len(mts) != 2 || mts[1].Path != "/mnt/My Disk" || mts[1].UsedPercent() != 90 {
		t.Errorf("df = %+v", mts)
	}
	for name, want := range map[string]bool{"sda": true, "sda1": false, "nvme0n1": true, "nvme0n1p1": false, "mmcblk0": true, "mmcblk0p2": false, "vdb": true, "loop3": false, "dm-0": false} {
		if wholeDisk(name) != want {
			t.Errorf("wholeDisk(%s) != %v", name, want)
		}
	}
	if _, ok := ParseLoadAvg([]string{"garbage"}); ok {
		t.Error("loadavg garbage accepted")
	}
	la, _ := ParseLoadAvg([]string{"0.50 0.25 0.10 2/345 6789"})
	if la.Load1 != 0.5 || la.Total != 345 {
		t.Errorf("loadavg = %+v", la)
	}
	if temps := ParseThermal([]string{"cpu-thermal 48312", "iwlwifi_1"}); len(temps) != 1 || temps[0].Celsius < 48.3 || temps[0].Celsius > 48.32 {
		t.Errorf("thermal = %+v", temps)
	}
}

func TestParseCgroups(t *testing.T) {
	id := strings.Repeat("ab", 32)
	got := ParseCgroups([]string{
		"v2 docker-" + id + ".scope 2247616568 133505024 14155776",
		"v1 " + strings.Repeat("cd", 32) + " 5000000000 1000 200",
		"v2 docker-short.scope 1 2 3",
		"v2 docker-" + strings.Repeat("ef", 32) + ".scope  1000", // unreadable memory
	})
	if u := got[id]; u.CPUUsec != 2247616568 || u.Memory != 133505024-14155776 {
		t.Errorf("v2 = %+v", u)
	}
	if u := got[strings.Repeat("cd", 32)]; u.CPUUsec != 5000000 || u.Memory != 800 {
		t.Errorf("v1 = %+v", u)
	}
	if len(got) != 2 {
		t.Errorf("got %d entries: %+v", len(got), got)
	}
}
