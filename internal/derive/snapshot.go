// Package derive turns parsed probe sections into a Snapshot the UI can show.
// Instantaneous values (CPU %) come from the two /proc/stat reads inside one
// probe; rates (network, disk I/O, per-process CPU) need the previous probe,
// so a State is kept per machine.
package derive

import "time"

// Snapshot is one frame for one machine. Pointer and slice fields are nil
// when the machine does not support the metric or the section failed; the
// names of failed sections are listed in Failed.
type Snapshot struct {
	Time   int64    `json:"time"` // unix milliseconds
	Host   Host     `json:"host"`
	CPU    *CPU     `json:"cpu,omitempty"`
	Mem    *Mem     `json:"mem,omitempty"`
	Load   *Load    `json:"load,omitempty"`
	Disks  []Disk   `json:"disks,omitempty"`
	Net    *Net     `json:"net,omitempty"`
	Failed []string `json:"failed,omitempty"`
	// ProbeOutdated: the machine's strict-mode authorized_keys line was
	// written for an older probe and should be replaced.
	ProbeOutdated bool `json:"probeOutdated,omitempty"`

	// detail level only
	TCP    *TCP    `json:"tcp,omitempty"`
	DiskIO *DiskIO `json:"diskio,omitempty"`
	Procs  []Proc  `json:"procs,omitempty"`
	// Users and Containers are null when unknown and [] when known to be empty.
	Users      []string    `json:"users"`
	Containers []Container `json:"containers"`
	Temps      []Temp      `json:"temps,omitempty"`
	Updates    *Updates    `json:"updates,omitempty"`
	Reboot     *bool       `json:"reboot,omitempty"`
	Addrs      []string    `json:"addrs,omitempty"`
}

// Host is static-ish identity information.
type Host struct {
	Hostname string  `json:"hostname,omitempty"`
	OS       string  `json:"os,omitempty"`
	OSID     string  `json:"osId,omitempty"`   // os-release ID ("dsm" on Synology)
	OSLike   string  `json:"osLike,omitempty"` // os-release ID_LIKE
	Kernel   string  `json:"kernel,omitempty"`
	Arch     string  `json:"arch,omitempty"`
	Uptime   float64 `json:"uptime,omitempty"` // seconds
	Cores    int     `json:"cores,omitempty"`
}

// CPU percentages are of total capacity (0–100), Cores per logical CPU.
type CPU struct {
	Percent float64   `json:"percent"`
	User    float64   `json:"user"`
	System  float64   `json:"system"`
	IOWait  float64   `json:"iowait"`
	Steal   float64   `json:"steal"`
	Cores   []float64 `json:"cores,omitempty"`
}

// Mem sizes are bytes. Used excludes reclaimable cache, like free(1).
type Mem struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Cache     uint64  `json:"cache"`
	Free      uint64  `json:"free"`
	Percent   float64 `json:"percent"`
	SwapTotal uint64  `json:"swapTotal"`
	SwapUsed  uint64  `json:"swapUsed"`
}

// Load averages plus process counts.
type Load struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
	Procs  int     `json:"procs"`
}

// Disk is one mounted filesystem.
type Disk struct {
	Path    string  `json:"path"`
	Device  string  `json:"device"`
	Size    uint64  `json:"size"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}

// Net covers physical interfaces. Rates are bytes/s and nil on the first frame.
type Net struct {
	RxRate  *float64 `json:"rxRate,omitempty"`
	TxRate  *float64 `json:"txRate,omitempty"`
	RxTotal uint64   `json:"rxTotal"` // since boot
	TxTotal uint64   `json:"txTotal"`
}

// TCP retransmission rate in percent of sent segments, nil on the first frame.
type TCP struct {
	RetransPercent *float64 `json:"retransPercent,omitempty"`
	Established    uint64   `json:"established"`
}

// DiskIO is summed over whole physical disks; nil on the first frame.
type DiskIO struct {
	ReadRate  float64 `json:"readRate"` // bytes/s
	WriteRate float64 `json:"writeRate"`
	ReadIOPS  float64 `json:"readIops"`
	WriteIOPS float64 `json:"writeIops"`
}

// Proc is a top process. CPU is percent of the whole machine (0–100, like
// the CPU ring, not top's percent of one core) and nil on the first frame
// because it needs two samples.
type Proc struct {
	PID  int      `json:"pid"`
	Name string   `json:"name"`
	CPU  *float64 `json:"cpu,omitempty"`
	RSS  uint64   `json:"rss"` // bytes
}

// Container is one Docker container.
type Container struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
	// CPU is percent of the whole machine (0–100, unlike docker stats' percent
	// of one core); nil on the first frame or without readable cgroups. Mem
	// excludes inactive page cache.
	CPU *float64 `json:"cpu,omitempty"`
	Mem *uint64  `json:"mem,omitempty"`
}

// Temp is one thermal zone in °C.
type Temp struct {
	Zone    string  `json:"zone"`
	Celsius float64 `json:"celsius"`
}

// Updates are pending package updates.
type Updates struct {
	Total    int `json:"total"`
	Security int `json:"security"`
}

func ms(t time.Time) int64 { return t.UnixMilli() }
