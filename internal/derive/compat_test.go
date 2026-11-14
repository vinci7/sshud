package derive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every sample must yield the overview essentials, whatever the system.
func TestAllSamplesHaveOverviewEssentials(t *testing.T) {
	dirs, err := os.ReadDir(filepath.Join("..", "..", "samples"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		n++
		t.Run(d.Name(), func(t *testing.T) {
			var s State
			snap := s.Update(sample(t, d.Name()), time.Unix(1000, 0))
			if snap.CPU == nil || snap.Mem == nil || snap.Load == nil || snap.Net == nil || len(snap.Disks) == 0 {
				t.Fatalf("missing essentials: %+v", snap)
			}
			if snap.Host.OS == "" || snap.Host.Hostname == "" || snap.Host.Cores == 0 {
				t.Errorf("host = %+v", snap.Host)
			}
			if snap.CPU.Percent < 0 || snap.CPU.Percent > 100 || snap.Mem.Percent <= 0 || snap.Mem.Percent > 100 {
				t.Errorf("cpu=%v mem=%v", snap.CPU.Percent, snap.Mem.Percent)
			}
			if len(snap.Procs) == 0 {
				t.Error("no processes")
			}
		})
	}
	if n < 5 {
		t.Errorf("only %d samples", n)
	}
}

func TestHandWrittenSamples(t *testing.T) {
	t.Run("alpine", func(t *testing.T) {
		var s State
		snap := s.Update(sample(t, "alpine-3.20-busybox"), time.Unix(1000, 0))
		// no getconf: page size falls back to 4096
		if snap.Procs[0].Name != "sshd: alice [priv]" || snap.Procs[0].RSS != 1802*4096 {
			t.Errorf("procs = %+v", snap.Procs)
		}
		if got := paths(snap.Disks); got != "/,/boot" {
			t.Errorf("disks = %s", got)
		}
		if snap.Host.OS != "Alpine Linux v3.20" || snap.Updates != nil || snap.Users != nil { // who failed: unknown, not empty
			t.Errorf("snap = %+v", snap)
		}
		if f := join(snap.Failed); f != "users,docker,updates" {
			t.Errorf("failed = %s", f)
		}
	})
	t.Run("raspbian", func(t *testing.T) {
		var s State
		snap := s.Update(sample(t, "raspbian-12-armv7"), time.Unix(1000, 0))
		// 8-column /proc/stat; core 0: 140 busy of 155 jiffies
		if snap.Host.Arch != "armv7l" || len(snap.CPU.Cores) != 4 || !near(snap.CPU.Cores[0], 90.32) || snap.CPU.Cores[1] != 0 {
			t.Errorf("cpu = %+v arch=%s", snap.CPU, snap.Host.Arch)
		}
		if snap.Disks[0].Percent < 88 || len(snap.Temps) != 1 || snap.Temps[0].Celsius != 51.54 {
			t.Errorf("disks=%+v temps=%+v", snap.Disks, snap.Temps)
		}
		if got := paths(snap.Disks); got != "/,/boot/firmware" {
			t.Errorf("disks = %s", got)
		}
	})
	t.Run("oracle linux", func(t *testing.T) {
		var s State
		snap := s.Update(sample(t, "oraclelinux-9-x86"), time.Unix(1000, 0))
		if snap.Host.OS != "Oracle Linux Server 9.4" {
			t.Errorf("os = %q", snap.Host.OS)
		}
		// LVM volumes count as disks; /boot/efi and tmpfs do not
		if got := paths(snap.Disks); got != "/,/var/oled,/boot" {
			t.Errorf("disks = %s", got)
		}
		// first-frame network rate from the probe's own pause (0.5 s): 200000 B in, 40000 B out
		if snap.Net.RxRate == nil || *snap.Net.RxRate != 400000 || *snap.Net.TxRate != 80000 {
			t.Errorf("net = %+v", snap.Net)
		}
		if snap.Updates != nil || snap.Containers != nil || join(snap.Failed) != "docker,updates" {
			t.Errorf("updates=%v containers=%v failed=%v", snap.Updates, snap.Containers, snap.Failed)
		}
	})
	t.Run("synology", func(t *testing.T) {
		var s State
		t0 := time.Unix(1000, 0)
		s.Update(sample(t, "synology-dsm7"), t0)
		snap := s.Update(sample(t, "synology-dsm7"), t0.Add(time.Second))
		if snap.Host.OS != "DSM 7.2.1" {
			t.Errorf("os = %q", snap.Host.OS)
		}
		if got := paths(snap.Disks); got != "/,/volume1" {
			t.Errorf("disks = %s", got)
		}
		if snap.DiskIO == nil { // sata1 must be recognised as a whole disk
			t.Error("no disk io")
		}
		if snap.Containers != nil || join(snap.Failed) != "docker,updates" {
			t.Errorf("containers=%v failed=%v", snap.Containers, snap.Failed)
		}
	})
}

func paths(ds []Disk) string {
	var p []string
	for _, d := range ds {
		p = append(p, d.Path)
	}
	return join(p)
}

func join(s []string) string { return strings.Join(s, ",") }
