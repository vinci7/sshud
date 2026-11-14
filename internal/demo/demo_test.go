package demo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vinci7/sshud/internal/derive"
	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/parse"
	"github.com/vinci7/sshud/internal/probeerr"
)

func TestDemoMachinesLookReal(t *testing.T) {
	s := New(Fleet(20))
	clock := s.start.Add(time.Hour)
	s.now = func() time.Time { return clock }
	for _, m := range Fleet(20) {
		t.Run(m.ID, func(t *testing.T) {
			var st derive.State
			var snap derive.Snapshot
			for i := 0; i < 3; i++ {
				if i == 1 && m.fail == "" && (snap.Net == nil || snap.Net.RxRate == nil) {
					t.Fatal("first frame has no network rate")
				}
				raw, err := s.Probe(context.Background(), m.ID, hub.Detail)
				if m.fail != "" {
					pe, ok := err.(*probeerr.Error)
					if !ok || pe.Kind != m.fail {
						t.Fatalf("err = %v", err)
					}
					if msg := err.Error(); strings.Contains(msg, "dial") && !strings.Contains(msg, m.Host) {
						t.Errorf("error %q names another address than %s", msg, m.Host)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				snap = st.Update(parse.Split(raw), clock)
				clock = clock.Add(2 * time.Second)
			}
			if snap.Host.Hostname != m.Name {
				t.Errorf("hostname = %q", snap.Host.Hostname)
			}
			if c := snap.CPU.Percent; c < m.cpu-1.5*m.swing-1 || c > m.cpu+1.5*m.swing+1 {
				t.Errorf("cpu = %.1f, want around %.0f", c, m.cpu)
			}
			if p := snap.Mem.Percent; p < m.memPct-3 || p > m.memPct+3 {
				t.Errorf("mem = %.1f, want around %.0f", p, m.memPct)
			}
			if r := *snap.Net.RxRate; r < m.rx*0.4 || r > m.rx*1.6 {
				t.Errorf("rx = %.0f, want around %.0f", r, m.rx)
			}
			if snap.DiskIO == nil || snap.DiskIO.WriteRate <= 0 || snap.TCP.RetransPercent == nil {
				t.Errorf("detail rates missing: %+v %+v", snap.DiskIO, snap.TCP)
			}
			if len(snap.Procs) == 0 || snap.Procs[0].CPU == nil || *snap.Procs[0].CPU <= 0 {
				t.Errorf("procs = %+v", snap.Procs)
			}
			// process and container CPU share the machine-wide scale of the ring
			var procs, containers float64
			for _, p := range snap.Procs {
				if p.CPU != nil {
					procs += *p.CPU
				}
			}
			for _, c := range snap.Containers {
				if c.Running && (c.CPU == nil || c.Mem == nil || *c.CPU < 0) {
					t.Errorf("container %s has no usage: %+v", c.Name, c)
				} else if c.CPU != nil {
					containers += *c.CPU
				}
			}
			if procs > snap.CPU.Percent || containers > snap.CPU.Percent {
				t.Errorf("processes use %.0f%% and containers %.0f%%, more than the machine's %.0f%%", procs, containers, snap.CPU.Percent)
			}
			for _, f := range snap.Failed {
				if f != "docker" && f != "users" && f != "updates" {
					t.Errorf("unexpected failed section %s", f)
				}
			}
		})
	}
}

func TestProbeHonoursCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(Machines).Probe(ctx, "vps-1", hub.Overview); err == nil {
		t.Error("expected error")
	}
}

func TestFleet(t *testing.T) {
	f := Fleet(25)
	seen := map[string]bool{}
	for _, m := range f {
		if seen[m.ID] {
			t.Errorf("duplicate id %s", m.ID)
		}
		seen[m.ID] = true
		if m.fail == "" && (m.cpu+1.4*m.swing > 100 || m.cpu-1.4*m.swing < 0) {
			t.Errorf("%s: cpu %v ± %v leaves 0–100", m.ID, m.cpu, m.swing)
		}
	}
	if len(f) != 25 || len(Fleet(3)) != len(Machines) {
		t.Errorf("sizes %d %d", len(f), len(Fleet(3)))
	}
	list, err := New(f).List()
	if err != nil || len(list) != 25 || list[24].ID != f[24].ID || list[24].Host != f[24].Host {
		t.Errorf("List = %+v, %v", list, err)
	}
}
