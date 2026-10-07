package metrics

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type SystemStats struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemUsedBytes  uint64  `json:"mem_used_bytes"`
	MemTotalBytes uint64  `json:"mem_total_bytes"`
	MemPercent    float64 `json:"mem_percent"`
	DiskUsedBytes uint64  `json:"disk_used_bytes"`
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
	DiskPercent   float64 `json:"disk_percent"`
	Goroutines    int     `json:"goroutines"`
	UptimeSeconds int64   `json:"uptime_seconds"`
}

type Monitor struct {
	mu           sync.Mutex
	startTime    time.Time
	prevIdleTime uint64
	prevTotalTime uint64
	lastCPU      float64
	lastSample   time.Time
}

func NewMonitor() *Monitor {
	m := &Monitor{
		startTime: time.Now(),
	}
	m.sampleCPU()
	return m
}

func (m *Monitor) Collect(diskPath string) SystemStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	cpu := m.sampleCPU()
	memUsed, memTotal, memPct := readMemory()
	diskUsed, diskTotal, diskPct := readDisk(diskPath)

	return SystemStats{
		CPUPercent:     cpu,
		MemUsedBytes:   memUsed,
		MemTotalBytes:  memTotal,
		MemPercent:     memPct,
		DiskUsedBytes:  diskUsed,
		DiskTotalBytes: diskTotal,
		DiskPercent:    diskPct,
		Goroutines:     runtime.NumGoroutine(),
		UptimeSeconds:  int64(time.Since(m.startTime).Seconds()),
	}
}

func (m *Monitor) sampleCPU() float64 {
	if time.Since(m.lastSample) < 500*time.Millisecond && m.lastCPU > 0 {
		return m.lastCPU
	}

	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0.0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)[1:]
			var total, idle uint64
			for i, valStr := range fields {
				val, _ := strconv.ParseUint(valStr, 10, 64)
				total += val
				if i == 3 { // idle
					idle = val
				}
			}

			if m.prevTotalTime > 0 && total > m.prevTotalTime {
				deltaTotal := float64(total - m.prevTotalTime)
				deltaIdle := float64(idle - m.prevIdleTime)
				cpuUsage := 100.0 * (1.0 - (deltaIdle / deltaTotal))
				if cpuUsage < 0 {
					cpuUsage = 0
				} else if cpuUsage > 100 {
					cpuUsage = 100
				}
				m.lastCPU = float64(int(cpuUsage*10)) / 10.0
			}

			m.prevTotalTime = total
			m.prevIdleTime = idle
			m.lastSample = time.Now()
			break
		}
	}
	return m.lastCPU
}

func readMemory() (used, total uint64, percent float64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		// Fallback to runtime stats
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.Alloc, ms.Sys, 0.0
	}
	defer file.Close()

	var memTotal, memFree, memAvailable, buffers, cached uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		valBytes := val * 1024

		switch fields[0] {
		case "MemTotal:":
			memTotal = valBytes
		case "MemFree:":
			memFree = valBytes
		case "MemAvailable:":
			memAvailable = valBytes
		case "Buffers:":
			buffers = valBytes
		case "Cached:":
			cached = valBytes
		}
	}

	if memTotal == 0 {
		return 0, 0, 0
	}

	if memAvailable > 0 {
		used = memTotal - memAvailable
	} else {
		used = memTotal - (memFree + buffers + cached)
	}

	pct := float64(used) / float64(memTotal) * 100.0
	return used, memTotal, float64(int(pct*10)) / 10.0
}

func readDisk(path string) (used, total uint64, percent float64) {
	if path == "" {
		path = "/"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0
	}

	total = stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used = total - free

	if total == 0 {
		return 0, 0, 0
	}
	pct := float64(used) / float64(total) * 100.0
	return used, total, float64(int(pct*10)) / 10.0
}

func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
