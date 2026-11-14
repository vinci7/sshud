package parse

import (
	"strconv"
	"strings"
)

// NetIface holds cumulative counters of one interface from /proc/net/dev.
type NetIface struct {
	Name                 string
	RxBytes, TxBytes     uint64
	RxPackets, TxPackets uint64
}

// Virtual reports interfaces that should not count toward host traffic:
// loopback, container bridges/veths and other software devices. Their
// traffic is already counted on the physical interface it leaves through.
func (n NetIface) Virtual() bool {
	if n.Name == "lo" {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "cni", "flannel", "cali", "vxlan", "kube", "dummy", "lxcbr", "lxc"} {
		if strings.HasPrefix(n.Name, p) {
			return true
		}
	}
	return false
}

// ParseNetDev parses /proc/net/dev.
func ParseNetDev(lines []string) ([]NetIface, bool) {
	var out []NetIface
	for _, l := range lines {
		name, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 10 {
			continue
		}
		var v [10]uint64
		bad := false
		for i := range v {
			n, err := strconv.ParseUint(f[i], 10, 64)
			if err != nil {
				bad = true
				break
			}
			v[i] = n
		}
		if bad {
			continue
		}
		out = append(out, NetIface{
			Name:    strings.TrimSpace(name),
			RxBytes: v[0], RxPackets: v[1],
			TxBytes: v[8], TxPackets: v[9],
		})
	}
	return out, len(out) > 0
}

// Mount is one filesystem from "df -P -k", sizes in bytes.
type Mount struct {
	Device, Path      string
	Size, Used, Avail uint64
}

// UsedPercent mirrors df's Capacity column: used / (used + avail), which
// excludes blocks reserved for root.
func (m Mount) UsedPercent() float64 {
	d := m.Used + m.Avail
	if d == 0 {
		return 0
	}
	return float64(m.Used) * 100 / float64(d)
}

var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "overlay": true, "efivarfs": true, "squashfs": true,
	"udev": true, "none": true, "shm": true, "cgroup": true, "cgroup2": true, "proc": true,
	"sysfs": true, "devfs": true, "run": true, "nsfs": true,
}

// ParseDF parses "df -P -k" and drops pseudo filesystems, loop / snap mounts and
// duplicate mounts of the same device (bind mounts).
func ParseDF(lines []string) ([]Mount, bool) {
	var out []Mount
	seen := map[string]bool{}
	for i, l := range lines {
		if i == 0 && strings.HasPrefix(l, "Filesystem") {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 6 {
			continue
		}
		// the mount path is everything after the 5th column and may contain spaces
		path := strings.Join(f[5:], " ")
		dev := f[0]
		// loop devices are disk images (snaps, ISOs): always 100% full, never a real disk
		if pseudoFS[dev] || strings.HasPrefix(dev, "/dev/loop") || strings.HasPrefix(path, "/snap/") || strings.HasPrefix(path, "/sys/") ||
			strings.HasPrefix(path, "/proc") || strings.HasPrefix(path, "/dev") || strings.HasPrefix(path, "/run") ||
			strings.HasPrefix(path, "/var/lib/docker/") || path == "/boot/efi" || path == "/tmp" {
			continue
		}
		size, e1 := strconv.ParseUint(f[1], 10, 64)
		used, e2 := strconv.ParseUint(f[2], 10, 64)
		avail, e3 := strconv.ParseUint(f[3], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || size == 0 {
			continue
		}
		if seen[dev] && strings.HasPrefix(dev, "/dev/") {
			continue
		}
		seen[dev] = true
		out = append(out, Mount{Device: dev, Path: path, Size: size * 1024, Used: used * 1024, Avail: avail * 1024})
	}
	return out, len(out) > 0
}

// TCPStat holds the TCP counters used for the retransmission rate.
type TCPStat struct {
	OutSegs, RetransSegs uint64
	CurrEstab            uint64
}

// ParseSNMP parses the "Tcp:" header/value pair of /proc/net/snmp.
func ParseSNMP(lines []string) (TCPStat, bool) {
	var hdr []string
	for _, l := range lines {
		if !strings.HasPrefix(l, "Tcp:") {
			continue
		}
		f := strings.Fields(l)[1:]
		if hdr == nil {
			hdr = f
			continue
		}
		var t TCPStat
		for i, k := range hdr {
			if i >= len(f) {
				break
			}
			n, err := strconv.ParseUint(f[i], 10, 64)
			if err != nil {
				continue
			}
			switch k {
			case "OutSegs":
				t.OutSegs = n
			case "RetransSegs":
				t.RetransSegs = n
			case "CurrEstab":
				t.CurrEstab = n
			}
		}
		return t, true
	}
	return TCPStat{}, false
}

// DiskIO holds cumulative counters of one block device from /proc/diskstats.
type DiskIO struct {
	Name                      string
	Reads, Writes             uint64 // completed operations
	ReadSectors, WriteSectors uint64 // 512-byte units regardless of device sector size
}

// ParseDiskstats parses /proc/diskstats keeping whole physical disks only
// (partitions, loop, ram and device-mapper children are skipped).
func ParseDiskstats(lines []string) ([]DiskIO, bool) {
	var out []DiskIO
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 10 {
			continue
		}
		name := f[2]
		if !wholeDisk(name) {
			continue
		}
		n := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
		out = append(out, DiskIO{Name: name, Reads: n(3), ReadSectors: n(5), Writes: n(7), WriteSectors: n(9)})
	}
	return out, len(out) > 0
}

func wholeDisk(name string) bool {
	for _, p := range []string{"loop", "ram", "zram", "dm-", "md", "sr", "fd"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	last := name[len(name)-1]
	switch {
	case strings.HasPrefix(name, "nvme"), strings.HasPrefix(name, "mmcblk"), strings.HasPrefix(name, "sata"), strings.HasPrefix(name, "sas"):
		// nvme0n1p2, mmcblk0p1 and Synology's sata1p1 are partitions
		return !strings.Contains(name[3:], "p")
	case strings.HasPrefix(name, "sd"), strings.HasPrefix(name, "vd"), strings.HasPrefix(name, "xvd"), strings.HasPrefix(name, "hd"):
		return last < '0' || last > '9'
	}
	return false
}

// Addr is one non-loopback address from "ip -o addr show".
type Addr struct {
	Iface, CIDR string
	IPv6        bool
}

// ParseAddr parses "ip -o addr show", skipping loopback, link-local and
// container bridge addresses.
func ParseAddr(lines []string) ([]Addr, bool) {
	var out []Addr
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 4 || (f[2] != "inet" && f[2] != "inet6") {
			continue
		}
		iface := strings.TrimSuffix(f[1], ":")
		cidr := f[3]
		if (NetIface{Name: iface}).Virtual() || strings.HasPrefix(cidr, "fe80:") {
			continue
		}
		out = append(out, Addr{Iface: iface, CIDR: cidr, IPv6: f[2] == "inet6"})
	}
	return out, len(out) > 0
}
