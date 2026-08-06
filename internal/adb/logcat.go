package adb

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// LogEntry is a parsed `logcat -v threadtime` row.
type LogEntry struct {
	Time  string `json:"time"`
	PID   int    `json:"pid"`
	TID   int    `json:"tid"`
	Level string `json:"lvl"` // V D I W E F
	Tag   string `json:"tag"`
	Msg   string `json:"msg"`
	// Proc and App are filled in by the caller from a ProcTable snapshot;
	// the parser itself only sees the pid. App is false for OS-owned lines.
	Proc string `json:"proc,omitempty"`
	App  bool   `json:"app,omitempty"`
}

// maxMsgLen caps a single log message. A runaway line (a base64 blob, a dumped
// heap) would otherwise be held in the UI's ring buffer for its whole lifetime
// and re-rendered on every frame; 8 KB is far beyond any readable line.
const maxMsgLen = 8 << 10

// LogcatStream is a running logcat subscription. Lines() emits parsed entries
// until the stream is stopped or the process exits.
type LogcatStream struct {
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	stderr   *bytes.Buffer
	ch       chan LogEntry
	stopOnce sync.Once
	stopped  atomic.Bool // set by Stop(); suppresses the stderr-on-exit entry
	done     chan struct{}
}

// StartLogcat spawns `adb -s serial logcat -v threadtime [--pid=N] [-T tail]`.
// Optional pid filter applied via --pid (API 24+). When tailLines > 0, only the
// last N lines are pulled before live-tailing (logcat -T).
func (c *Client) StartLogcat(ctx context.Context, serial string, pid int, buffer int, tailLines int) (*LogcatStream, error) {
	args := []string{"-s", serial, "logcat", "-v", "threadtime"}
	if pid > 0 {
		args = append(args, "--pid="+strconv.Itoa(pid))
	}
	if tailLines > 0 {
		args = append(args, "-T", strconv.Itoa(tailLines))
	}
	// Explicit `*:V` filterspec so verbosity is decided here, not by whatever
	// ANDROID_LOG_TAGS the device's shell environment happens to export (some
	// ROMs/CI images default to a restrictive level, silently dropping V/D).
	args = append(args, "*:V")
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Capture stderr so a failed invocation (e.g. `--pid` rejected by pre-API-24
	// logcat, an unknown buffer, or a permission error) surfaces to the user
	// instead of producing a silent empty stream.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if buffer <= 0 {
		buffer = 1024
	}
	s := &LogcatStream{cmd: cmd, stdout: stdout, stderr: &stderr, ch: make(chan LogEntry, buffer), done: make(chan struct{})}
	go s.pump()
	return s, nil
}

func (s *LogcatStream) pump() {
	defer close(s.ch)
	defer close(s.done)
	sc := LineScanner(s.stdout)
	var last *LogEntry
	for sc.Scan() {
		line := sc.Text()
		if entry, ok := parseThreadtime(line); ok {
			cp := entry
			s.ch <- cp
			last = &cp
			continue
		}
		t := strings.TrimRight(line, " \r")
		// logcat prints buffer-switch banners like "--------- beginning of
		// main"; they are not continuations of the previous entry, so drop them
		// rather than splicing them into an unrelated message.
		if t == "" || strings.HasPrefix(strings.TrimLeft(t, " "), "--------- beginning of") {
			continue
		}
		if last == nil {
			continue
		}
		// Genuine continuation (e.g. ART stacktrace). Attach to the previous
		// record as a new logical line at the same level.
		s.ch <- LogEntry{
			Time:  last.Time,
			PID:   last.PID,
			TID:   last.TID,
			Level: last.Level,
			Tag:   last.Tag,
			Msg:   truncMsg("    " + strings.TrimLeft(t, " ")),
		}
	}
	_ = s.cmd.Wait()
	// If the process produced diagnostics on stderr (a rejected flag, missing
	// buffer, permission denial), surface them so the stream doesn't just end
	// silently with no logs and no explanation. Skip this when the user stopped
	// the stream — a kill can leave benign teardown noise on stderr.
	if s.stderr != nil && !s.stopped.Load() {
		if msg := strings.TrimSpace(s.stderr.String()); msg != "" {
			s.ch <- LogEntry{Level: "E", Tag: "adbq", Msg: "logcat: " + firstLine(msg)}
		}
	}
}

func (s *LogcatStream) Lines() <-chan LogEntry { return s.ch }

func (s *LogcatStream) Stop() {
	s.stopOnce.Do(func() {
		s.stopped.Store(true)
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
	<-s.done
}

// parseThreadtime parses a `logcat -v threadtime` line. The canonical shape is:
//
//	05-22 12:18:04.123  1582 1612 I ActivityManager: msg here
//
// but the leading timestamp columns vary across Android versions and logd
// settings and threadtime gives no flag to pin them: a year may be prefixed
// (`2026-05-22`, API 24+), a UTC offset token may follow the time (`+0000`),
// or the whole stamp may be monotonic (`1234.567`). Rather than assume fixed
// column offsets we scan for the PID/TID/level triple — the first place where
// two consecutive all-digit tokens are followed by a single log-level letter —
// and treat everything before it as the timestamp and everything after the
// level as "Tag: message".
func parseThreadtime(line string) (LogEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return LogEntry{}, false
	}
	// i starts at 1 so at least one timestamp token precedes the PID column.
	for i := 1; i+2 < len(fields); i++ {
		// The token before the PID column has to look like the tail of a
		// timestamp. Without that anchor a wrapped stack-trace line such as
		// "at 100 200 D foo" matches the digit/digit/level shape and is parsed
		// as a brand-new entry, tearing the trace apart and inventing a pid.
		if !looksLikeStampTail(fields[i-1]) {
			continue
		}
		if !isAllDigits(fields[i]) || !isAllDigits(fields[i+1]) {
			continue
		}
		lvl := fields[i+2]
		if len(lvl) != 1 || !isLogLevel(lvl[0]) {
			continue
		}
		pid, _ := strconv.Atoi(fields[i])
		tid, _ := strconv.Atoi(fields[i+1])
		// Rest of the original line after the leading (i+3) tokens, preserving
		// the message's internal spacing.
		rest := strings.TrimSpace(skipFields(line, i+3))
		tag := rest
		msg := ""
		if before, after, found := strings.Cut(rest, ": "); found {
			tag = strings.TrimSpace(before)
			msg = after
		}
		return LogEntry{
			Time:  strings.Join(fields[:i], " "),
			PID:   pid,
			TID:   tid,
			Level: lvl,
			Tag:   tag,
			Msg:   truncMsg(msg),
		}, true
	}
	return LogEntry{}, false
}

// truncMsg clamps an over-long message, marking it so the reader knows the
// tail was dropped here rather than by the device.
func truncMsg(s string) string {
	if len(s) <= maxMsgLen {
		return s
	}
	// Back off to a rune boundary so the cut does not leave half a UTF-8
	// sequence, which JSON marshalling would turn into a replacement char.
	cut := maxMsgLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + " …[truncated]"
}

// looksLikeStampTail reports whether tok can be the last token of a threadtime
// timestamp, i.e. the thing that immediately precedes the PID column. That is
// either a clock time ("12:18:04.123"), a monotonic stamp ("1234.567"), or the
// optional UTC offset some ROMs append ("+0000").
func looksLikeStampTail(tok string) bool {
	if strings.ContainsAny(tok, ":.") {
		return true
	}
	if len(tok) == 5 && (tok[0] == '+' || tok[0] == '-') && isAllDigits(tok[1:]) {
		return true
	}
	return false
}

// isLogLevel reports whether b is an Android log priority letter.
func isLogLevel(b byte) bool {
	switch b {
	case 'V', 'D', 'I', 'W', 'E', 'F', 'A', 'S':
		return true
	}
	return false
}

// PidOf returns the PID of the named package or 0 if not running.
func (c *Client) PidOf(ctx context.Context, serial, pkg string) (int, error) {
	out, err := c.Shell(ctx, serial, "pidof "+pkg)
	if err == nil {
		if fields := strings.Fields(strings.TrimSpace(out)); len(fields) > 0 {
			if pid, perr := strconv.Atoi(fields[0]); perr == nil {
				return pid, nil
			}
		}
	}
	// Fallback for stripped ROMs lacking pidof / ps -A / awk: scan procfs.
	main, subs := c.pidsForPackage(ctx, serial, pkg)
	if main > 0 {
		return main, nil
	}
	if len(subs) > 0 {
		return subs[0], nil
	}
	return 0, nil
}
