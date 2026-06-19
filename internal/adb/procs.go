package adb

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcRow is one process row from a /proc snapshot.
type ProcRow struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu"`   // %
	Mem     float64 `json:"mem"`   // %
	RSS     int64   `json:"rss"`   // KB
	VSZ     int64   `json:"vsz"`   // KB
	State   string  `json:"state"` // R, S, D, Z, T...
	Name    string  `json:"name"`
	Cmdline string  `json:"cmdline"`
}

// ProcSnapshot is a single sampling cycle.
type ProcSnapshot struct {
	Time  int64     `json:"time"`
	Total int       `json:"total"`
	Rows  []ProcRow `json:"rows"`
}

// pageSizeKB is the assumed memory page size in KB. The Linux page size is
// 4096 bytes on every Android arch we target; getconf is missing on the
// stripped ROMs we support, so we hardcode it rather than probe.
const pageSizeKB = 4

// TopStream periodically samples /proc on the device and emits one
// ProcSnapshot per cycle. It does NOT spawn a long-lived `top`: legacy Android
// `top` rejects `-b`/`-o`, so instead a goroutine ticks every intervalSec and
// reads procfs in a single ShellSU call.
type TopStream struct {
	c        *Client
	serial   string
	interval time.Duration

	out      chan ProcSnapshot
	cancel   context.CancelFunc
	stopOnce sync.Once
	done     chan struct{}

	// CPU% accounting carried across cycles.
	prevTotal int64         // previous /proc/stat total jiffies
	prevProc  map[int]int64 // pid -> previous (utime+stime) jiffies
}

func (s *TopStream) Snapshots() <-chan ProcSnapshot { return s.out }

func (s *TopStream) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
	<-s.done
}

// StartTopStream begins sampling process state from the device's procfs every
// intervalSec seconds and pushes snapshots onto Snapshots(). The public API is
// unchanged from the old `top`-based implementation; only the data source
// differs (procfs works on every Android version, including legacy ROMs whose
// `top`/`ps` lack the flags we need).
func (c *Client) StartTopStream(ctx context.Context, serial string, intervalSec int) (*TopStream, error) {
	if intervalSec <= 0 {
		intervalSec = 2
	}
	// Validate the adb binary up front so callers get an immediate error
	// rather than a silent empty stream.
	if _, err := c.Binary(); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithCancel(ctx)
	s := &TopStream{
		c:        c,
		serial:   serial,
		interval: time.Duration(intervalSec) * time.Second,
		out:      make(chan ProcSnapshot, 8),
		cancel:   cancel,
		done:     make(chan struct{}),
		prevProc: map[int]int64{},
	}
	go s.loop(cctx)
	return s, nil
}

// procfsCmd reads everything we need in one shell round-trip:
//
//	/proc/stat (CPU jiffies + cpuN lines for ncpu)
//	all /proc/PID/stat lines
//	/proc/meminfo (MemTotal)
//
// Sections are separated by a sentinel line so the host-side parser can split
// them without relying on tools like awk/sed (missing on stripped ROMs).
const procfsCmd = "cat /proc/stat; echo '@@@'; cat /proc/[0-9]*/stat 2>/dev/null; echo '@@@'; cat /proc/meminfo"

func (s *TopStream) loop(ctx context.Context) {
	defer close(s.out)
	defer close(s.done)

	tick := func() bool {
		out, _, err := s.c.ShellSU(ctx, s.serial, procfsCmd)
		if err != nil {
			return ctx.Err() == nil // keep looping on transient errors
		}
		snap := s.buildSnapshot(out)
		select {
		case s.out <- snap:
		case <-ctx.Done():
			return false
		}
		return true
	}

	// Emit a first snapshot immediately (CPU% will be 0 — no prior sample).
	if !tick() {
		return
	}
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !tick() {
				return
			}
		}
	}
}

// buildSnapshot parses one procfs dump, computes CPU%/Mem%, and updates the
// cross-cycle accounting state on s.
func (s *TopStream) buildSnapshot(out string) ProcSnapshot {
	statSec, procSec, memSec := splitProcfsSections(out)

	totalJiffies, ncpu := parseProcStat(statSec)
	memTotalKB := parseMemTotalKB(memSec)

	totalDelta := totalJiffies - s.prevTotal
	hasPrevTotal := s.prevTotal > 0
	if ncpu < 1 {
		ncpu = 1
	}

	nextProc := make(map[int]int64)
	var rows []ProcRow
	for _, ln := range strings.Split(procSec, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		p, ok := parseProcStatLine(ln)
		if !ok {
			continue
		}
		procJiffies := p.utime + p.stime
		nextProc[p.pid] = procJiffies

		row := ProcRow{
			PID:     p.pid,
			State:   p.state,
			Name:    p.comm,
			Cmdline: p.comm,
			RSS:     p.rssPages * pageSizeKB,
			VSZ:     p.vsize / 1024,
		}
		if memTotalKB > 0 {
			row.Mem = 100 * float64(row.RSS) / float64(memTotalKB)
		}
		if hasPrevTotal && totalDelta > 0 {
			if prev, ok := s.prevProc[p.pid]; ok {
				d := procJiffies - prev
				if d < 0 {
					d = 0
				}
				row.CPU = 100 * float64(ncpu) * float64(d) / float64(totalDelta)
				if row.CPU < 0 {
					row.CPU = 0
				}
			}
		}
		rows = append(rows, row)
	}

	s.prevTotal = totalJiffies
	s.prevProc = nextProc

	return ProcSnapshot{Time: time.Now().UnixMilli(), Total: len(rows), Rows: rows}
}

// splitProcfsSections splits the combined dump on the "@@@" sentinel lines into
// (statSection, procSection, memSection).
func splitProcfsSections(out string) (string, string, string) {
	parts := strings.Split(out, "@@@")
	get := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	return get(0), get(1), get(2)
}

// parseProcStat reads /proc/stat and returns total jiffies (sum of all numbers
// on the first "cpu " aggregate line) and the CPU count (number of "cpuN"
// per-core lines).
func parseProcStat(stat string) (totalJiffies int64, ncpu int) {
	for _, ln := range strings.Split(stat, "\n") {
		fs := strings.Fields(ln)
		if len(fs) == 0 {
			continue
		}
		if fs[0] == "cpu" {
			for _, f := range fs[1:] {
				n, err := strconv.ParseInt(f, 10, 64)
				if err != nil {
					continue
				}
				totalJiffies += n
			}
			continue
		}
		// cpu0, cpu1, ... per-core aggregate lines.
		if strings.HasPrefix(fs[0], "cpu") && len(fs[0]) > 3 {
			if _, err := strconv.Atoi(fs[0][3:]); err == nil {
				ncpu++
			}
		}
	}
	return totalJiffies, ncpu
}

// parseMemTotalKB extracts MemTotal (in KB) from /proc/meminfo.
func parseMemTotalKB(mem string) int64 {
	for _, ln := range strings.Split(mem, "\n") {
		if !strings.HasPrefix(ln, "MemTotal:") {
			continue
		}
		fs := strings.Fields(ln)
		// "MemTotal:" <number> "kB"
		if len(fs) >= 2 {
			if n, err := strconv.ParseInt(fs[1], 10, 64); err == nil {
				return n
			}
		}
	}
	return 0
}

// procStat holds the few /proc/PID/stat fields we use.
type procStat struct {
	pid      int
	comm     string
	state    string
	utime    int64
	stime    int64
	vsize    int64 // bytes
	rssPages int64 // pages
}

// parseProcStatLine parses a single /proc/PID/stat line. The format is:
//
//	pid (comm) state ppid ...
//
// comm is delimited by the FIRST '(' and the LAST ')' and may itself contain
// spaces and parentheses, so we locate those bytes explicitly rather than
// splitting on whitespace. Fields after the last ')' are 0-indexed:
//
//	[0]=state [11]=utime [12]=stime [20]=vsize(bytes) [21]=rss(pages)
func parseProcStatLine(line string) (procStat, bool) {
	line = strings.TrimSpace(line)
	open := strings.IndexByte(line, '(')
	close := strings.LastIndexByte(line, ')')
	if open < 0 || close < 0 || close < open {
		return procStat{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line[:open]))
	if err != nil {
		return procStat{}, false
	}
	comm := line[open+1 : close]

	rest := strings.Fields(strings.TrimSpace(line[close+1:]))
	if len(rest) < 22 {
		return procStat{}, false
	}
	atoi64 := func(i int) int64 {
		n, _ := strconv.ParseInt(rest[i], 10, 64)
		return n
	}
	atou64 := func(i int) int64 {
		// vsize/rss can exceed int64 in the textual unsigned form on some
		// kernels; clamp via uint64 then cast.
		u, _ := strconv.ParseUint(rest[i], 10, 64)
		return int64(u)
	}
	return procStat{
		pid:      pid,
		comm:     comm,
		state:    rest[0],
		utime:    atoi64(11),
		stime:    atoi64(12),
		vsize:    atou64(20),
		rssPages: atoi64(21),
	}, true
}
