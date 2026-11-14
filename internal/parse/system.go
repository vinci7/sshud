package parse

import (
	"strconv"
	"strings"
)

// CPUTimes holds jiffies from one "cpu" line of /proc/stat.
type CPUTimes struct {
	User, Nice, System, Idle, IOWait, IRQ, SoftIRQ, Steal uint64
}

// Total is the sum of all accounted jiffies (guest time is already in User).
func (c CPUTimes) Total() uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal
}

// CPUStat is the parsed content of /proc/stat.
type CPUStat struct {
	All   CPUTimes
	Cores []CPUTimes
}

// ParseCPUStat parses /proc/stat. Older kernels with fewer columns are accepted.
func ParseCPUStat(lines []string) (CPUStat, bool) {
	var st CPUStat
	found := false
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		v := make([]uint64, 8)
		for i := 0; i < 8 && i+1 < len(f); i++ {
			v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
		}
		t := CPUTimes{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]}
		if f[0] == "cpu" {
			st.All = t
			found = true
		} else {
			st.Cores = append(st.Cores, t)
		}
	}
	return st, found
}

// MemInfo holds /proc/meminfo values in bytes.
type MemInfo struct {
	Total, Free, Available, Buffers, Cached, SReclaimable, Shmem uint64
	SwapTotal, SwapFree                                          uint64
	HasAvailable                                                 bool // MemAvailable exists (kernel >= 3.14)
}

// ParseMemInfo parses /proc/meminfo.
func ParseMemInfo(lines []string) (MemInfo, bool) {
	var m MemInfo
	fields := map[string]*uint64{
		"MemTotal": &m.Total, "MemFree": &m.Free, "MemAvailable": &m.Available,
		"Buffers": &m.Buffers, "Cached": &m.Cached, "SReclaimable": &m.SReclaimable,
		"Shmem": &m.Shmem, "SwapTotal": &m.SwapTotal, "SwapFree": &m.SwapFree,
	}
	for _, l := range lines {
		key, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		p, ok := fields[key]
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		*p = n
		if key == "MemAvailable" {
			m.HasAvailable = true
		}
	}
	return m, m.Total > 0
}

// LoadAvg is the parsed content of /proc/loadavg.
type LoadAvg struct {
	Load1, Load5, Load15 float64
	Running, Total       int
}

// ParseLoadAvg parses /proc/loadavg.
func ParseLoadAvg(lines []string) (LoadAvg, bool) {
	if len(lines) == 0 {
		return LoadAvg{}, false
	}
	f := strings.Fields(lines[0])
	if len(f) < 3 {
		return LoadAvg{}, false
	}
	var la LoadAvg
	var err error
	for i, p := range []*float64{&la.Load1, &la.Load5, &la.Load15} {
		if *p, err = strconv.ParseFloat(f[i], 64); err != nil {
			return LoadAvg{}, false
		}
	}
	if len(f) > 3 {
		if r, t, ok := strings.Cut(f[3], "/"); ok {
			la.Running, _ = strconv.Atoi(r)
			la.Total, _ = strconv.Atoi(t)
		}
	}
	return la, true
}

// ParseUptime returns seconds since boot from /proc/uptime.
func ParseUptime(lines []string) (float64, bool) {
	if len(lines) == 0 {
		return 0, false
	}
	f := strings.Fields(lines[0])
	if len(f) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(f[0], 64)
	return v, err == nil
}

// Uname holds the output of "uname -srm".
type Uname struct {
	Kernel, Release, Arch string
}

// ParseUname parses "uname -srm".
func ParseUname(lines []string) (Uname, bool) {
	if len(lines) == 0 {
		return Uname{}, false
	}
	f := strings.Fields(lines[0])
	if len(f) < 3 {
		return Uname{}, false
	}
	return Uname{Kernel: f[0], Release: f[1], Arch: f[len(f)-1]}, true
}

// OSRelease holds selected fields of /etc/os-release.
type OSRelease struct {
	ID, IDLike, VersionID, PrettyName string
}

// ParseOSRelease parses /etc/os-release.
func ParseOSRelease(lines []string) (OSRelease, bool) {
	var o OSRelease
	for _, l := range lines {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			o.ID = v
		case "ID_LIKE":
			o.IDLike = v
		case "VERSION_ID":
			o.VersionID = v
		case "PRETTY_NAME":
			o.PrettyName = v
		case "os_name": // Synology /etc.defaults/VERSION
			o.ID = strings.ToLower(v)
		case "productversion":
			o.VersionID = v
		}
	}
	if o.PrettyName == "" && o.ID == "dsm" {
		o.PrettyName = "DSM " + o.VersionID
	}
	return o, o.ID != "" || o.PrettyName != ""
}

// ParseFirstLine returns the trimmed first line, e.g. for the hostname section.
func ParseFirstLine(lines []string) (string, bool) {
	if len(lines) == 0 {
		return "", false
	}
	s := strings.TrimSpace(lines[0])
	return s, s != ""
}
