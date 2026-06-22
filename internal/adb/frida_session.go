package adb

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed frida_driver.py
var fridaDriverPy []byte

// fridaSessionRing bounds how many messages we keep per session for backfill.
// Matches the logcat ring so a busy script can't grow memory without bound.
const fridaSessionRing = 5000

// FridaMsg is one normalized line of session output. Seq is a per-session
// monotonic counter the backend stamps on ingest so the frontend can backfill
// missed lines (Wails events are fire-and-forget) and de-duplicate by seq.
type FridaMsg struct {
	Seq     int    `json:"seq"`
	Time    int64  `json:"time"` // unix millis
	Kind    string `json:"kind"` // log|send|error|loaded|resumed|detached|status|fatal|ready
	Script  string `json:"script,omitempty"`
	Level   string `json:"level,omitempty"`   // info|warning|error (for log)
	Payload string `json:"payload,omitempty"` // message text / JSON-encoded send
	Stack   string `json:"stack,omitempty"`   // for error
	Detail  string `json:"detail,omitempty"`  // for fatal/status
}

// FridaSessionInfo is the metadata the UI lists (no log body).
type FridaSessionInfo struct {
	ID         string `json:"id"`
	Serial     string `json:"serial"`
	Package    string `json:"package"`
	Mode       string `json:"mode"`
	Runtime    string `json:"runtime"` // frida version of the host runtime
	StartedAt  int64  `json:"startedAt"`
	Status     string `json:"status"` // running|ended|error
	StatusNote string `json:"statusNote,omitempty"`
}

// FridaScriptArg is one script to load (name + JS source).
type FridaScriptArg struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// FridaSession is a running host-side frida driver process streaming messages.
type FridaSession struct {
	info  FridaSessionInfo
	cmd   *exec.Cmd
	stdin io.WriteCloser
	tmp   string // per-session temp dir (driver + job)

	mu     sync.Mutex
	ring   []FridaMsg
	seq    int
	status string
	note   string

	ch       chan FridaMsg
	done     chan struct{}
	stopOnce sync.Once
}

type fridaJob struct {
	Serial     string           `json:"serial"`
	Package    string           `json:"package"`
	Mode       string           `json:"mode"`
	Scripts    []FridaScriptArg `json:"scripts"`
	BridgesDir string           `json:"bridgesDir,omitempty"` // dir with java.js/objc.js/swift.js (Frida 17 needs these)
}

// StartFridaSession launches the embedded driver under the given host runtime,
// instrumenting pkg on the device. mode is "spawn" (cold-start, default) or
// "attach" (to a running process). The caller is responsible for ensuring a
// matching frida-server is already running on the device.
func StartFridaSession(ctx context.Context, rt FridaRuntime, id, serial, pkg, mode string, scripts []FridaScriptArg) (*FridaSession, error) {
	py := resolveInterpreter(rt.PythonPath)
	if py == "" || !fileExists(py) {
		return nil, fmt.Errorf("host runtime interpreter not found: %s", rt.PythonPath)
	}
	if strings.TrimSpace(pkg) == "" {
		return nil, fmt.Errorf("no target package")
	}
	if mode != "attach" {
		mode = "spawn"
	}

	tmp, err := os.MkdirTemp("", "adbq-frida-"+sanitize(id)+"-")
	if err != nil {
		return nil, err
	}
	driverPath := filepath.Join(tmp, "frida_driver.py")
	if err := os.WriteFile(driverPath, fridaDriverPy, 0o600); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	// Provision the Frida 17 runtime bridges (Java/ObjC/Swift) so scripts that
	// reference them work like they do under the `frida` CLI. Best-effort and
	// cached: a failure just means a Java-using script will report the same
	// "Java is not defined" as before, but non-bridge scripts still run.
	bridgesDir := ""
	if rt.FridaVersion != "" {
		bctx, bcancel := context.WithTimeout(ctx, 60*time.Second)
		if d, err := ensureFridaBridges(bctx, rt.FridaVersion); err == nil {
			bridgesDir = d
		}
		bcancel()
	}

	jobPath := filepath.Join(tmp, "job.json")
	jobBytes, _ := json.Marshal(fridaJob{Serial: serial, Package: pkg, Mode: mode, Scripts: scripts, BridgesDir: bridgesDir})
	if err := os.WriteFile(jobPath, jobBytes, 0o600); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}

	// Not bound to ctx: a session outlives the request that started it and is
	// stopped explicitly (or on app shutdown). -u defeats stdout buffering.
	cmd := exec.Command(py, "-u", driverPath, jobPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("launch frida driver: %w", err)
	}

	s := &FridaSession{
		info: FridaSessionInfo{
			ID: id, Serial: serial, Package: pkg, Mode: mode,
			Runtime: rt.FridaVersion, StartedAt: time.Now().UnixMilli(), Status: "running",
		},
		cmd:    cmd,
		stdin:  stdin,
		tmp:    tmp,
		status: "running",
		ch:     make(chan FridaMsg, 256),
		done:   make(chan struct{}),
	}
	go s.pump(stdout, stderr)
	return s, nil
}

// pump reads the driver's stdout line-by-line, normalizes each JSON object into
// a sequenced FridaMsg, and fans it out to the ring buffer and the live channel.
// stderr is drained so a chatty interpreter can't block the child on a full pipe.
func (s *FridaSession) pump(stdout, stderr io.Reader) {
	defer close(s.ch)
	defer close(s.done)

	var stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			if stderrBuf.Len() < 8192 {
				stderrBuf.WriteString(sc.Text() + "\n")
			}
		}
	}()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		msg, ok := parseFridaMsg(line)
		if !ok {
			// A non-JSON line (e.g. a stray print) becomes a log entry rather
			// than vanishing, so nothing is silently lost.
			msg = FridaMsg{Kind: "log", Level: "info", Payload: line}
		}
		s.ingest(msg)
	}

	wg.Wait()
	_ = s.cmd.Wait()
	// Decide a terminal status from what we saw.
	s.mu.Lock()
	final := "ended"
	note := s.note
	if s.status == "error" {
		final = "error"
	}
	if note == "" && stderrBuf.Len() > 0 && final == "error" {
		note = firstLine(stderrBuf.String())
	}
	s.status = final
	s.note = note
	s.info.Status = final
	s.info.StatusNote = note
	s.mu.Unlock()
}

// ingest stamps a message with a monotonic seq + time and stores/forwards it.
func (s *FridaSession) ingest(m FridaMsg) {
	s.mu.Lock()
	s.seq++
	m.Seq = s.seq
	if m.Time == 0 {
		m.Time = time.Now().UnixMilli()
	}
	// A fatal message marks the session errored and carries a user-facing note.
	if m.Kind == "fatal" {
		s.status = "error"
		s.note = fatalNote(m)
		s.info.Status = "error"
		s.info.StatusNote = s.note
	}
	if len(s.ring) >= fridaSessionRing {
		s.ring = append(s.ring[1:], m)
	} else {
		s.ring = append(s.ring, m)
	}
	s.mu.Unlock()

	select {
	case s.ch <- m:
	default: // never block the reader if the consumer is slow
	}
}

// Messages is the live channel of session messages (closed when the driver exits).
func (s *FridaSession) Messages() <-chan FridaMsg { return s.ch }

// Info returns the current session metadata snapshot.
func (s *FridaSession) Info() FridaSessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// LogSince returns ring messages with Seq > sinceSeq (0 = all retained). Used by
// the frontend to backfill after subscribing, then de-dupe by seq.
func (s *FridaSession) LogSince(sinceSeq int) []FridaMsg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FridaMsg, 0, len(s.ring))
	for _, m := range s.ring {
		if m.Seq > sinceSeq {
			out = append(out, m)
		}
	}
	return out
}

// Stop signals the driver to detach and exit (close stdin = portable stop),
// waits briefly, then kills it, and cleans up the temp dir.
func (s *FridaSession) Stop() {
	s.stopOnce.Do(func() {
		if s.stdin != nil {
			_, _ = io.WriteString(s.stdin, "stop\n")
			_ = s.stdin.Close()
		}
		select {
		case <-s.done:
		case <-time.After(4 * time.Second):
			if s.cmd != nil && s.cmd.Process != nil {
				_ = s.cmd.Process.Kill()
			}
			<-s.done
		}
		if s.tmp != "" {
			_ = os.RemoveAll(s.tmp)
		}
	})
}

// parseFridaMsg converts one driver JSON line into a FridaMsg (seq/time stamped
// later by ingest). Returns false for unparseable input.
func parseFridaMsg(line string) (FridaMsg, bool) {
	var raw struct {
		Type    string          `json:"type"`
		Script  string          `json:"script"`
		Level   string          `json:"level"`
		Payload json.RawMessage `json:"payload"`
		Stack   string          `json:"stack"`
		Detail  string          `json:"detail"`
		Error   string          `json:"error"`
		Stage   string          `json:"stage"`
		Reason  string          `json:"reason"`
		PID     int             `json:"pid"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil || raw.Type == "" {
		return FridaMsg{}, false
	}
	m := FridaMsg{Kind: raw.Type, Script: raw.Script, Level: raw.Level, Stack: raw.Stack}
	m.Payload = jsonRawToString(raw.Payload)
	switch raw.Type {
	case "fatal":
		m.Detail = raw.Detail
		if m.Payload == "" {
			m.Payload = raw.Error
		}
	case "status", "ready":
		m.Detail = raw.Stage
	case "detached":
		m.Detail = raw.Reason
	}
	return m, true
}

// jsonRawToString renders a JSON value as a display string: bare strings unquoted,
// everything else compact-encoded.
func jsonRawToString(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return string(r)
}

func fatalNote(m FridaMsg) string {
	switch {
	case strings.Contains(m.Payload, "version-mismatch") || strings.Contains(m.Detail, "major version"):
		return "frida version mismatch — the host runtime doesn't match the device's frida-server"
	case m.Detail != "":
		return m.Detail
	default:
		return m.Payload
	}
}
