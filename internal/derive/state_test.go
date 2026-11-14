package derive

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vinci7/sshud/internal/parse"
)

func sample(t *testing.T, name string) map[string]parse.Section {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "samples", name, "detail.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return parse.Split(string(b))
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// Expected CPU values were computed independently with awk from the sample.
func TestFirstFrameFromSample(t *testing.T) {
	var s State
	snap := s.Update(sample(t, "ubuntu-24.04-x86-docker"), time.Unix(1000, 0))

	if snap.CPU == nil || !near(snap.CPU.Percent, 6.98) || !near(snap.CPU.User, 6.61) || len(snap.CPU.Cores) != 16 {
		t.Errorf("cpu = %+v", snap.CPU)
	}
	if snap.Host.Cores != 16 || snap.Host.Hostname != "homelab" || snap.Host.OS != "Ubuntu 24.04.4 LTS" {
		t.Errorf("host = %+v", snap.Host)
	}
	// used = MemTotal - MemAvailable, the same number free(1) shows
	if m := snap.Mem; m == nil || m.Used != (20240116-12930236)*1024 || !near(m.Percent, 36.12) {
		t.Errorf("mem = %+v", m)
	} else if m.Used+m.Cache+m.Free > m.Total {
		t.Errorf("mem parts exceed total: %+v", m)
	}
	if snap.Net == nil || snap.Net.RxRate != nil || snap.Net.RxTotal == 0 {
		t.Errorf("net on first frame = %+v", snap.Net)
	}
	if snap.TCP == nil || snap.TCP.RetransPercent != nil || snap.DiskIO != nil {
		t.Error("rates must be empty on the first frame")
	}
	if len(snap.Procs) != TopN || snap.Procs[0].CPU != nil {
		t.Errorf("procs = %+v", snap.Procs)
	}
	for _, p := range snap.Procs {
		if p.RSS == 0 {
			t.Errorf("kernel thread in top list: %+v", p)
		}
	}
	if len(snap.Containers) != 24 || len(snap.Disks) != 3 || snap.Updates.Security != 22 || !*snap.Reboot {
		t.Errorf("containers=%d disks=%d updates=%+v", len(snap.Containers), len(snap.Disks), snap.Updates)
	}
	if len(snap.Failed) != 0 {
		t.Errorf("failed = %v", snap.Failed)
	}
}

func TestDockerMissingIsFailedNotFatal(t *testing.T) {
	var s State
	snap := s.Update(sample(t, "ubuntu-24.04-x86-vm"), time.Unix(1000, 0))
	if len(snap.Failed) != 1 || snap.Failed[0] != "docker" {
		t.Errorf("failed = %v", snap.Failed)
	}
	if snap.CPU == nil || snap.Mem == nil || snap.Mem.SwapTotal != 0 || snap.Containers != nil {
		t.Errorf("snap = %+v", snap)
	}
}

func frame(rx, tx, outSegs, retrans, reads, rsec, ticks string) map[string]parse.Section {
	return parse.Split(`@@stat1
cpu  100 0 100 800 0 0 0 0
cpu0 50 0 50 400 0 0 0 0
cpu1 50 0 50 400 0 0 0 0
@@stat2
cpu  150 0 150 900 0 0 0 0
cpu0 75 0 75 450 0 0 0 0
cpu1 75 0 75 450 0 0 0 0
@@netdev
  eth0: ` + rx + ` 1 0 0 0 0 0 0 ` + tx + ` 1 0 0 0 0 0 0
  docker0: 999999 1 0 0 0 0 0 0 999999 1 0 0 0 0 0 0
@@snmp
Tcp: OutSegs RetransSegs CurrEstab
Tcp: ` + outSegs + ` ` + retrans + ` 3
@@diskstats
   8       0 sda ` + reads + ` 0 ` + rsec + ` 0 10 0 80 0 0 0 0
@@conf
clk_tck 100
pagesize 4096
@@procs
1 ` + ticks + ` 0 10 busy loop
2 0 0 999 idle
`)
}

func TestRatesAcrossFrames(t *testing.T) {
	var s State
	t0 := time.Unix(1000, 0)
	s.Update(frame("1000", "2000", "100", "1", "10", "100", "0"), t0)
	snap := s.Update(frame("3000", "2500", "300", "3", "30", "2148", "150"), t0.Add(2*time.Second))

	if snap.CPU.Percent != 50 { // 100 busy jiffies of 200
		t.Errorf("cpu = %v", snap.CPU.Percent)
	}
	if *snap.Net.RxRate != 1000 || *snap.Net.TxRate != 250 { // docker0 must not count
		t.Errorf("net = %v %v", *snap.Net.RxRate, *snap.Net.TxRate)
	}
	if *snap.TCP.RetransPercent != 1 {
		t.Errorf("retrans = %v", *snap.TCP.RetransPercent)
	}
	if snap.DiskIO.ReadIOPS != 10 || snap.DiskIO.ReadRate != 2048*512/2 || snap.DiskIO.WriteIOPS != 0 {
		t.Errorf("diskio = %+v", snap.DiskIO)
	}
	// 150 ticks at 100 Hz = 1.5 CPU-seconds, of the 2 s × 2 cores = 4 the
	// machine had: 37.5 % of the machine (top would say 75 %)
	if p := snap.Procs[0]; p.Name != "busy loop" || p.CPU == nil || *p.CPU != 37.5 {
		t.Errorf("top proc = %+v", p)
	}
	if snap.Procs[1].Name != "idle" || snap.Procs[1].RSS != 999*4096 {
		t.Errorf("procs = %+v", snap.Procs)
	}

	// counters went backwards (reboot): rates are dropped, not negative
	snap = s.Update(frame("10", "10", "1", "0", "1", "1", "1"), t0.Add(4*time.Second))
	if snap.Net.RxRate != nil || snap.TCP.RetransPercent != nil || snap.DiskIO != nil {
		t.Errorf("rates after reset: %+v %+v %+v", snap.Net, snap.TCP, snap.DiskIO)
	}
}

func TestMemWithoutAvailable(t *testing.T) {
	m := memFrom(parse.MemInfo{Total: 1000, Free: 200, Buffers: 100, Cached: 300, Shmem: 50})
	if m.Used != 450 || m.Cache != 350 || m.Free != 200 {
		t.Errorf("mem = %+v", m)
	}
}

func TestFirstFrameNetRateFromProbePause(t *testing.T) {
	var s State
	snap := s.Update(parse.Split(`@@netdev0
  eth0: 1000 1 0 0 0 0 0 0 5000 1 0 0 0 0 0 0
@@uptime0
100.00 1.00
@@uptime
100.50 1.00
@@netdev
  eth0: 1500 1 0 0 0 0 0 0 6000 1 0 0 0 0 0 0
`), time.Unix(1000, 0))
	if snap.Net.RxRate == nil || *snap.Net.RxRate != 1000 || *snap.Net.TxRate != 2000 {
		t.Errorf("net = %+v", snap.Net)
	}
}

func TestEmptyListsAreKnownEmpty(t *testing.T) {
	var s State
	snap := s.Update(parse.Split("@@users\n@@docker\n"), time.Unix(1000, 0))
	if snap.Users == nil || len(snap.Users) != 0 || snap.Containers == nil {
		t.Errorf("users=%#v containers=%#v", snap.Users, snap.Containers)
	}
	b, _ := json.Marshal(snap)
	if !strings.Contains(string(b), `"users":[]`) || !strings.Contains(string(b), `"containers":[]`) {
		t.Errorf("json = %s", b)
	}
}

func TestRetransSpansAWindow(t *testing.T) {
	var s State
	t0 := time.Unix(1000, 0)
	tcp := func(out, re int) map[string]parse.Section {
		return parse.Split(fmt.Sprintf("@@snmp\nTcp: OutSegs RetransSegs CurrEstab\nTcp: %d %d 1\n", out, re))
	}
	s.Update(tcp(1000, 10), t0)
	s.Update(tcp(1100, 10), t0.Add(2*time.Second))
	// one retransmit in a quiet frame: 1/100 per frame, but 1/200 over the window
	snap := s.Update(tcp(1200, 11), t0.Add(4*time.Second))
	if p := *snap.TCP.RetransPercent; p != 0.5 {
		t.Errorf("retrans = %v, want 0.5 over the window", p)
	}
	for i := 3; i < RetransWindow+5; i++ {
		snap = s.Update(tcp(1000+100*i, 11), t0.Add(time.Duration(2*i)*time.Second))
	}
	if p := *snap.TCP.RetransPercent; p != 0 {
		t.Errorf("retrans after the window moved on = %v", p)
	}
}

func TestContainerUsage(t *testing.T) {
	id := strings.Repeat("a", 64)
	frame := func(usec int) map[string]parse.Section {
		return parse.Split(fmt.Sprintf("@@stat1\ncpu 0 0 0 0\ncpu0 0 0 0 0\ncpu1 0 0 0 0\ncpu2 0 0 0 0\ncpu3 0 0 0 0\n@@stat2\ncpu 0 0 0 4\ncpu0 0 0 0 1\ncpu1 0 0 0 1\ncpu2 0 0 0 1\ncpu3 0 0 0 1\n@@docker\n%s\tweb\tnginx\trunning\tUp 1 hour\n%s\told\tnginx\texited\tExited (137) 2 hours ago\n@@cgroup\nv2 docker-%s.scope %d 3000 1000\n", id, strings.Repeat("b", 64), id, usec))
	}
	var s State
	t0 := time.Unix(1000, 0)
	snap := s.Update(frame(1000000), t0)
	if c := snap.Containers[0]; c.CPU != nil || c.Mem == nil || *c.Mem != 2000 {
		t.Errorf("first frame = %+v", c)
	}
	snap = s.Update(frame(4000000), t0.Add(2*time.Second))
	// 3 CPU-seconds of the 2 s × 4 cores = 8 the machine had (docker stats: 150 %)
	if c := snap.Containers[0]; c.CPU == nil || *c.CPU != 37.5 {
		t.Errorf("cpu = %+v", c)
	}
	if c := snap.Containers[1]; c.CPU != nil || c.Mem != nil {
		t.Errorf("stopped container has usage: %+v", c)
	}
}
