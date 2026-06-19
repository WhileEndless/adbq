package adb

import (
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// LogEntry is a parsed `logcat -v threadtime` row.
type LogEntry struct {
	Time  string `json:"time"`
	PID   int    `json:"pid"`
	TID   int    `json:"tid"`
	Level string `json:"lvl"` // V D I W E F
	Tag   string `json:"tag"`
	Msg   string `json:"msg"`
}

// LogcatStream is a running logcat subscription. Lines() emits parsed entries
// until the stream is stopped or the process exits.
type LogcatStream struct {
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	ch       chan LogEntry
	stopOnce sync.Once
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
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if buffer <= 0 {
		buffer = 1024
	}
	s := &LogcatStream{cmd: cmd, stdout: stdout, ch: make(chan LogEntry, buffer), done: make(chan struct{})}
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
		// Non-threadtime line is a continuation (e.g. ART stacktrace). Attach
		// to the previous record's message as a new logical line.
		t := strings.TrimRight(line, " \r")
		if t == "" || last == nil {
			continue
		}
		// Emit as a synthetic continuation row at same level so the user still
		// sees the stack frames inline.
		s.ch <- LogEntry{
			Time:  last.Time,
			PID:   last.PID,
			TID:   last.TID,
			Level: last.Level,
			Tag:   last.Tag,
			Msg:   "    " + strings.TrimLeft(t, " "),
		}
	}
	_ = s.cmd.Wait()
}

func (s *LogcatStream) Lines() <-chan LogEntry { return s.ch }

func (s *LogcatStream) Stop() {
	s.stopOnce.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
	<-s.done
}

// parseThreadtime parses lines of form:
//
//	05-22 12:18:04.123  1582 1612 I ActivityManager: msg here
func parseThreadtime(line string) (LogEntry, bool) {
	if len(line) < 28 {
		return LogEntry{}, false
	}
	// time = first two tokens
	parts := strings.SplitN(strings.TrimLeft(line, " "), " ", 6)
	// "05-22" "12:18:04.123" "1582" "1612" "I" "Tag: msg"
	// In practice, multiple spaces collapse — re-split more carefully.
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return LogEntry{}, false
	}
	date := fields[0]
	time := fields[1]
	pid, err1 := strconv.Atoi(fields[2])
	tid, err2 := strconv.Atoi(fields[3])
	lvl := fields[4]
	if err1 != nil || err2 != nil || len(lvl) != 1 {
		return LogEntry{}, false
	}
	// Remainder: "Tag: message" — find first ": " after the level token
	idx := indexAfter(line, fields[4])
	rest := strings.TrimSpace(line[idx:])
	tag := rest
	msg := ""
	if i := strings.Index(rest, ": "); i >= 0 {
		tag = strings.TrimSpace(rest[:i])
		msg = rest[i+2:]
	}
	_ = parts
	return LogEntry{
		Time:  date + " " + time,
		PID:   pid,
		TID:   tid,
		Level: lvl,
		Tag:   tag,
		Msg:   msg,
	}, true
}

func indexAfter(s, tok string) int {
	i := strings.Index(s, " "+tok+" ")
	if i < 0 {
		return len(s)
	}
	return i + len(tok) + 2
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
