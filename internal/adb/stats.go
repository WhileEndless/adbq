package adb

import (
	"context"
	"strconv"
	"strings"
)

// GetStats returns a snapshot of CPU/RAM/battery/storage/uptime.
func (c *Client) GetStats(ctx context.Context, serial string) (*Stats, error) {
	s := &Stats{}

	// Battery
	if out, err := c.Shell(ctx, serial, "dumpsys battery"); err == nil {
		for _, ln := range strings.Split(out, "\n") {
			t := strings.TrimSpace(ln)
			switch {
			case strings.HasPrefix(t, "level:"):
				s.BatteryLevel = atoi(strings.TrimSpace(strings.TrimPrefix(t, "level:")))
			case strings.HasPrefix(t, "temperature:"):
				v := atoi(strings.TrimSpace(strings.TrimPrefix(t, "temperature:")))
				s.BatteryTemp = float64(v) / 10.0
			case strings.HasPrefix(t, "voltage:"):
				s.BatteryVoltage = atoi(strings.TrimSpace(strings.TrimPrefix(t, "voltage:")))
			case strings.HasPrefix(t, "AC powered:") || strings.HasPrefix(t, "USB powered:") || strings.HasPrefix(t, "Wireless powered:"):
				if strings.Contains(t, "true") {
					s.Charging = true
				}
			}
		}
	}

	// Memory
	if out, err := c.Shell(ctx, serial, "cat /proc/meminfo"); err == nil {
		for _, ln := range strings.Split(out, "\n") {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "MemTotal:") {
				s.MemTotalKB = atoi64(extractNum(t))
			} else if strings.HasPrefix(t, "MemAvailable:") {
				s.MemAvailKB = atoi64(extractNum(t))
			}
		}
	}

	// Load average — try /proc/loadavg first (may be denied on Android 9+),
	// then fall back to `uptime` which usually still exposes it.
	if out, err := c.Shell(ctx, serial, "cat /proc/loadavg 2>/dev/null"); err == nil && strings.TrimSpace(out) != "" {
		fs := strings.Fields(out)
		if len(fs) >= 1 {
			if v, err := strconv.ParseFloat(fs[0], 64); err == nil {
				s.LoadAvg1 = v
			}
		}
	}
	if s.LoadAvg1 == 0 {
		if out, err := c.Shell(ctx, serial, "uptime"); err == nil {
			// "... load average: 6.57, 6.55, 6.97"
			if i := strings.Index(out, "load average:"); i >= 0 {
				rest := strings.TrimSpace(out[i+len("load average:"):])
				fs := strings.SplitN(rest, ",", 2)
				if len(fs) >= 1 {
					if v, err := strconv.ParseFloat(strings.TrimSpace(fs[0]), 64); err == nil {
						s.LoadAvg1 = v
					}
				}
			}
		}
	}

	// CPU percent — derive from two /proc/stat samples. This avoids `top`,
	// whose batch flags (`-b`/`-n`) and header layout vary wildly across
	// toybox/legacy builds and whose pipe-to-`head` breaks on ROMs without
	// `head`. /proc/stat is present on every Android kernel.
	if a, ok := readCPUStat(ctx, c, serial, false); ok {
		// Short device-side delay between samples so the delta is meaningful.
		if b, ok2 := readCPUStat(ctx, c, serial, true); ok2 {
			dTotal := b.total - a.total
			dIdle := b.idle - a.idle
			if dTotal > 0 {
				p := 100 * (1 - float64(dIdle)/float64(dTotal))
				if p < 0 {
					p = 0
				} else if p > 100 {
					p = 100
				}
				s.CPUPercent = p
			}
		}
	}

	// Uptime
	if out, err := c.Shell(ctx, serial, "cat /proc/uptime"); err == nil {
		fs := strings.Fields(out)
		if len(fs) >= 1 {
			if v, err := strconv.ParseFloat(fs[0], 64); err == nil {
				s.UptimeSeconds = int64(v)
			}
		}
	}

	// Storage of /data (user data) via df. Output formats vary widely:
	//   modern toybox: "Filesystem 1K-blocks Used Available Use% Mounted" (KB)
	//   legacy toolbox: "Filesystem Size Used Free Blksize" with human sizes
	//                   like 5.9G / 881.8M (and `-k` is unsupported, erroring).
	// We omit `-k` and detect the format from the header, parsing host-side.
	if out, err := c.Shell(ctx, serial, "df /data 2>/dev/null"); err == nil {
		total, free, ok := parseDataDF(out)
		if ok {
			s.StorageTotalKB = total
			s.StorageFreeKB = free
		}
	}

	// Network counters for wlan0
	if out, err := c.Shell(ctx, serial, "cat /proc/net/dev 2>/dev/null"); err == nil {
		for _, ln := range strings.Split(out, "\n") {
			ln = strings.TrimSpace(ln)
			if !strings.HasPrefix(ln, "wlan0:") {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(ln, "wlan0:"))
			fs := strings.Fields(rest)
			// rx bytes is index 0, tx bytes is index 8
			if len(fs) >= 9 {
				s.NetRxBytes = atoi64(fs[0])
				s.NetTxBytes = atoi64(fs[8])
			}
			break
		}
	}

	return s, nil
}

// cpuSample holds the aggregate jiffy counters from the `cpu` line of
// /proc/stat that we need to compute a busy percentage.
type cpuSample struct {
	total int64
	idle  int64
}

// readCPUStat reads the aggregate `cpu ...` line from /proc/stat. When delay is
// true a short device-side sleep precedes the read, giving the second sample of
// a pair a measurable window. Returns ok=false if the line can't be parsed.
func readCPUStat(ctx context.Context, c *Client, serial string, delay bool) (cpuSample, bool) {
	cmd := "cat /proc/stat 2>/dev/null"
	if delay {
		// `sleep` is a sh builtin / coreutil present on every Android.
		cmd = "sleep 0.3 2>/dev/null; " + cmd
	}
	out, err := c.Shell(ctx, serial, cmd)
	if err != nil {
		return cpuSample{}, false
	}
	for _, ln := range strings.Split(out, "\n") {
		if !strings.HasPrefix(ln, "cpu ") {
			continue
		}
		// cpu user nice system idle iowait irq softirq steal guest guest_nice
		fs := strings.Fields(ln)
		if len(fs) < 5 {
			return cpuSample{}, false
		}
		var total int64
		for _, f := range fs[1:] {
			total += atoi64(f)
		}
		// idle = idle (field 4) + iowait (field 5, when present)
		idle := atoi64(fs[4])
		if len(fs) >= 6 {
			idle += atoi64(fs[5])
		}
		return cpuSample{total: total, idle: idle}, true
	}
	return cpuSample{}, false
}

// parseDataDF extracts total/free space in KB for the /data row of `df` output,
// supporting both the 1K-block layout and the legacy human-readable layout.
func parseDataDF(out string) (totalKB, freeKB int64, ok bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, 0, false
	}
	// Detect human-readable layout from the header ("Size ... Free ... Blksize").
	header := lines[0]
	humanReadable := strings.Contains(header, "Blksize") ||
		(strings.Contains(header, "Size") && strings.Contains(header, "Free"))
	// Use the last non-empty row (it references /data given the query).
	var row string
	for i := len(lines) - 1; i >= 1; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			row = lines[i]
			break
		}
	}
	fs := strings.Fields(row)
	if len(fs) < 4 {
		return 0, 0, false
	}
	if humanReadable {
		// Filesystem Size Used Free Blksize
		return humanToKB(fs[1]), humanToKB(fs[3]), true
	}
	// Filesystem 1K-blocks Used Available Use% Mounted
	return atoi64(fs[1]), atoi64(fs[3]), true
}

// humanToKB converts a df human-readable size like "5.9G", "881.8M", "120K" or
// a bare byte count into kilobytes. Returns 0 on parse failure.
func humanToKB(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := 1.0 / 1024.0 // default: bytes
	last := s[len(s)-1]
	switch last {
	case 'K', 'k':
		mult = 1
		s = s[:len(s)-1]
	case 'M', 'm':
		mult = 1024
		s = s[:len(s)-1]
	case 'G', 'g':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case 'T', 't':
		mult = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	case 'B', 'b':
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(v * mult)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func extractNum(s string) string {
	var b strings.Builder
	started := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			started = true
		} else if started {
			break
		}
	}
	return b.String()
}
