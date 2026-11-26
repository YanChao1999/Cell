// Package hoststats samples worker host CPU / memory / disk (stdlib only).
package hoststats

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Sample is a point-in-time host resource snapshot.
type Sample struct {
	CPURatio       float64 `json:"cpu_ratio"` // 0..1 over recent interval
	MemUsedBytes   uint64  `json:"mem_used_bytes"`
	MemTotalBytes  uint64  `json:"mem_total_bytes"`
	DiskUsedBytes  uint64  `json:"disk_used_bytes"`
	DiskTotalBytes uint64  `json:"disk_total_bytes"`
}

var (
	cpuMu     sync.Mutex
	prevTotal uint64
	prevIdle  uint64
	prevOK    bool
)

// Collect returns CPU/mem (process host) and disk usage for diskPath (e.g. workspaces root).
func Collect(diskPath string) Sample {
	var s Sample
	s.CPURatio = cpuRatio()
	s.MemUsedBytes, s.MemTotalBytes = memBytes()
	s.DiskUsedBytes, s.DiskTotalBytes = diskBytes(diskPath)
	return s
}

func cpuRatio() float64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	idle, total, ok := readProcStat()
	if !ok {
		return 0
	}
	cpuMu.Lock()
	defer cpuMu.Unlock()
	if !prevOK || total <= prevTotal {
		prevIdle, prevTotal, prevOK = idle, total, true
		return 0 // first sample / counter reset — need a later heartbeat interval
	}
	dTotal := total - prevTotal
	dIdle := idle - prevIdle
	prevIdle, prevTotal = idle, total
	if dTotal == 0 {
		return 0
	}
	busy := 1 - float64(dIdle)/float64(dTotal)
	if busy < 0 {
		return 0
	}
	if busy > 1 {
		return 1
	}
	return busy
}

func readProcStat() (idle, total uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(sc.Text())
	// cpu user nice system idle iowait irq softirq steal …
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var sum uint64
	for i := 1; i < len(fields); i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		sum += v
		if i == 4 {
			idle = v
		}
	}
	return idle, sum, true
}

func memBytes() (used, total uint64) {
	if runtime.GOOS != "linux" {
		return 0, 0
	}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()
	var memTotal, memAvail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			memTotal = parseMemKB(line) * 1024
		case strings.HasPrefix(line, "MemAvailable:"):
			memAvail = parseMemKB(line) * 1024
		}
	}
	if memTotal == 0 {
		return 0, 0
	}
	if memAvail > memTotal {
		memAvail = memTotal
	}
	return memTotal - memAvail, memTotal
}

func parseMemKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[1], 10, 64)
	return v
}

func diskBytes(path string) (used, total uint64) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	// #nosec G115 — filesystem block counts fit uint64 on Linux.
	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	free := st.Bavail * bsize
	if free > total {
		free = total
	}
	return total - free, total
}
