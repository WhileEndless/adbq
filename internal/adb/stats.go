package adb

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// statsFastCmd reads everything that genuinely moves between refreshes, in one
// round trip. Sections are `@@@`-delimited so the host parses them without awk
// or sed, neither of which exists on stripped ROMs.
//
// The two /proc/stat samples now sit in the SAME shell, with the sleep between
// them. That is not only cheaper — it is more accurate: previously the interval
// was "0.3s plus two adb round trips of unpredictable length", so the busy
// percentage was computed over a window nobody knew the width of.
const statsFastCmd = "cat /proc/stat" +
	"; echo '@@@'; sleep 0.3; cat /proc/stat" +
	"; echo '@@@'; cat /proc/meminfo" +
	"; echo '@@@'; cat /proc/loadavg 2>/dev/null" +
	"; echo '@@@'; cat /proc/uptime" +
	"; echo '@@@'; cat /proc/net/dev 2>/dev/null"

// statsSlowCmd reads what changes on a human timescale. Battery percentage and
// free storage do not meaningfully differ between two refreshes 2.5s apart, and
// `dumpsys battery` is a binder call into a system service — the most expensive
// thing the old Overview poll did, several times a minute, to watch a number
// that changes a few times an hour.
const statsSlowCmd = "dumpsys battery" +
	"; echo '@@@'; df /data 2>/dev/null"

// statsSlowTTL bounds how stale the battery/storage half may be.
const statsSlowTTL = 30 * time.Second

// GetStats returns a snapshot of CPU/RAM/battery/storage/uptime.
//
// Overview polls this every few seconds, so it is one of the two paths that
// decide adbq's idle cost. It used to issue nine separate `adb shell` calls per
// refresh; it now issues one, plus a second at most twice a minute for the
// slow-moving half.
func (c *Client) GetStats(ctx context.Context, serial string) (*Stats, error) {
	s := &Stats{}

	// ── Fixed for this connection ────────────────────────────────────────
	caps := c.Capabilities(ctx, serial)
	s.MemTotalKB = caps.MemTotalKB
	s.StorageTotalKB = caps.StorageTotalKB

	// ── Slow half: cached, and keyed under storage so a push or an install
	//    drops it (see cachedomain.go). ────────────────────────────────────
	slow, err := cachedRead(c, serial, "storage.stats.slow", statsSlowTTL, func() (statsSlow, error) {
		out, err := c.Shell(ctx, serial, statsSlowCmd)
		if err != nil {
			return statsSlow{}, err
		}
		return parseStatsSlow(out), nil
	})
	if err == nil {
		s.BatteryLevel = slow.batteryLevel
		s.BatteryTemp = slow.batteryTemp
		s.BatteryVoltage = slow.batteryVoltage
		s.Charging = slow.charging
		if slow.storageTotalKB > 0 {
			s.StorageTotalKB = slow.storageTotalKB
		}
		s.StorageFreeKB = slow.storageFreeKB
	}
	if !slow.batteryFound {
		// dumpsys battery is stubbed or absent on some emulators, TV boxes and
		// very stripped builds; sysfs is the universal fallback.
		c.readBatterySysfs(ctx, serial, s)
	}

	// ── Fast half ────────────────────────────────────────────────────────
	out, err := c.Shell(ctx, serial, statsFastCmd)
	if err != nil {
		// The slow half may still have produced something worth showing.
		return s, nil
	}
	applyStatsFast(s, out)
	return s, nil
}

// statsSlow is the parsed slow half. A struct rather than mutating Stats so the
// value can be cached and re-applied without re-reading.
type statsSlow struct {
	batteryFound   bool
	batteryLevel   int
	batteryTemp    float64
	batteryVoltage int
	charging       bool
	storageTotalKB int64
	storageFreeKB  int64
}

// parseStatsSlow parses `dumpsys battery` + `df /data`. Pure, so it is testable
// against captured output from real ROMs.
func parseStatsSlow(out string) statsSlow {
	var sl statsSlow
	parts := strings.Split(out, "@@@")
	section := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	for _, ln := range strings.Split(section(0), "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "level:"):
			sl.batteryLevel = atoi(strings.TrimSpace(strings.TrimPrefix(t, "level:")))
			sl.batteryFound = true
		case strings.HasPrefix(t, "temperature:"):
			sl.batteryTemp = float64(atoi(strings.TrimSpace(strings.TrimPrefix(t, "temperature:")))) / 10.0
		case strings.HasPrefix(t, "voltage:"):
			sl.batteryVoltage = atoi(strings.TrimSpace(strings.TrimPrefix(t, "voltage:")))
		case strings.HasPrefix(t, "AC powered:"), strings.HasPrefix(t, "USB powered:"), strings.HasPrefix(t, "Wireless powered:"):
			if strings.Contains(t, "true") {
				sl.charging = true
			}
		}
	}
	if total, free, ok := parseDataDF(section(1)); ok {
		sl.storageTotalKB, sl.storageFreeKB = total, free
	}
	return sl
}

// applyStatsFast fills the live fields from one statsFastCmd result.
func applyStatsFast(s *Stats, out string) {
	parts := strings.Split(out, "@@@")
	section := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}

	// CPU% from the delta between the two /proc/stat samples.
	if a, ok := parseCPUSample(section(0)); ok {
		if b, ok2 := parseCPUSample(section(1)); ok2 {
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

	// Memory. MemTotal comes from the capability scan, but a device that was
	// probed before /proc was readable may have none, so take it here too.
	var memFree, buffers, cached int64
	memAvailFound := false
	for _, ln := range strings.Split(section(2), "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "MemTotal:"):
			if s.MemTotalKB == 0 {
				s.MemTotalKB = atoi64(extractNum(t))
			}
		case strings.HasPrefix(t, "MemAvailable:"):
			s.MemAvailKB = atoi64(extractNum(t))
			memAvailFound = true
		case strings.HasPrefix(t, "MemFree:"):
			memFree = atoi64(extractNum(t))
		case strings.HasPrefix(t, "Buffers:"):
			buffers = atoi64(extractNum(t))
		case strings.HasPrefix(t, "Cached:"):
			cached = atoi64(extractNum(t))
		}
	}
	// MemAvailable arrived in kernel 3.14; API 21-22 devices on older kernels
	// lack it, which would make Overview report 100% RAM used.
	if !memAvailFound {
		s.MemAvailKB = memFree + buffers + cached
	}

	if fs := strings.Fields(section(3)); len(fs) >= 1 {
		if v, err := strconv.ParseFloat(fs[0], 64); err == nil {
			s.LoadAvg1 = v
		}
	}

	if fs := strings.Fields(section(4)); len(fs) >= 1 {
		if v, err := strconv.ParseFloat(fs[0], 64); err == nil {
			s.UptimeSeconds = int64(v)
		}
	}

	for _, ln := range strings.Split(section(5), "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "wlan0:") {
			continue
		}
		fs := strings.Fields(strings.TrimSpace(strings.TrimPrefix(ln, "wlan0:")))
		// rx bytes is index 0, tx bytes is index 8
		if len(fs) >= 9 {
			s.NetRxBytes = atoi64(fs[0])
			s.NetTxBytes = atoi64(fs[8])
		}
		break
	}
}

// readBatterySysfs fills battery fields from /sys/class/power_supply/battery,
// the universal source when dumpsys battery is unavailable. Each node is read
// into its own sentinel-delimited section so a missing node can't shift the
// parse of the others.
func (c *Client) readBatterySysfs(ctx context.Context, serial string, s *Stats) {
	const base = "/sys/class/power_supply/battery"
	cmd := "for f in capacity temp voltage_now status; do cat " + base + "/$f 2>/dev/null; echo '@@@'; done"
	out, err := c.Shell(ctx, serial, cmd)
	if err != nil {
		return
	}
	parts := strings.Split(out, "@@@")
	get := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	if v := get(0); v != "" {
		s.BatteryLevel = atoi(v)
	}
	if v := get(1); v != "" {
		s.BatteryTemp = float64(atoi(v)) / 10.0 // tenths of °C
	}
	if v := get(2); v != "" {
		s.BatteryVoltage = atoi(v) / 1000 // µV → mV
	}
	if st := strings.ToLower(get(3)); st == "charging" || st == "full" {
		s.Charging = true
	}
}

// cpuSample holds the aggregate jiffy counters from the `cpu` line of
// /proc/stat that we need to compute a busy percentage.
type cpuSample struct {
	total int64
	idle  int64
}

// parseCPUSample extracts the aggregate `cpu ...` line from /proc/stat output.
// Pure: both samples of a pair now arrive in one batched read (statsFastCmd),
// so nothing here talks to a device. Returns ok=false if the line is absent or
// unparseable.
func parseCPUSample(out string) (cpuSample, bool) {
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
