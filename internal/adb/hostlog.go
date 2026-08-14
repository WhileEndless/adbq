package adb

import (
	"strings"
	"sync"
)

// Host-process output (the emulator, rootAVD) is kept in memory, separate from
// the device logcat ring, and deliberately small: these logs are read when
// something goes wrong, not tailed continuously, and a headless emulator left
// running for a day would otherwise accumulate megabytes nobody reads.
const (
	hostLogMaxLines = 1500
	hostLogMaxBytes = 256 * 1024
)

// HostLogLine is one line of host-process output. Seq is monotonic per buffer
// so the UI can pull only what it hasn't seen instead of re-rendering the whole
// log every poll.
type HostLogLine struct {
	Seq  int    `json:"seq"`
	Text string `json:"text"`
	Err  bool   `json:"err"` // came from stderr
}

// hostLog is a bounded, sequence-stamped ring of output lines.
type hostLog struct {
	mu    sync.Mutex
	lines []HostLogLine
	bytes int
	seq   int
}

func newHostLog() *hostLog { return &hostLog{} }

// Append adds one line, dropping the oldest lines once either bound is hit.
func (l *hostLog) Append(text string, isErr bool) {
	text = strings.TrimRight(text, "\r\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	l.lines = append(l.lines, HostLogLine{Seq: l.seq, Text: text, Err: isErr})
	l.bytes += len(text)
	for len(l.lines) > hostLogMaxLines || (l.bytes > hostLogMaxBytes && len(l.lines) > 1) {
		l.bytes -= len(l.lines[0].Text)
		l.lines = l.lines[1:]
	}
}

// Since returns the retained lines with Seq greater than sinceSeq. Passing 0
// returns everything still held.
func (l *hostLog) Since(sinceSeq int) []HostLogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []HostLogLine{}
	for _, ln := range l.lines {
		if ln.Seq > sinceSeq {
			out = append(out, ln)
		}
	}
	return out
}

// LastMeaningful returns the most recent non-empty line, which is what a failed
// launch should surface as its error rather than the whole transcript.
func (l *hostLog) LastMeaningful() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(l.lines[i].Text); t != "" {
			return t
		}
	}
	return ""
}

func (l *hostLog) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = nil
	l.bytes = 0
	// seq intentionally keeps counting: a frontend holding an old cursor must
	// not be handed lines it already rendered.
}
