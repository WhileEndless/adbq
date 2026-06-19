package adb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ScrcpyManager spawns and tracks per-serial scrcpy processes.
type ScrcpyManager struct {
	mu      sync.Mutex
	procs   map[string]*exec.Cmd // serial → running process
	binPath string
}

func NewScrcpyManager() *ScrcpyManager {
	return &ScrcpyManager{procs: map[string]*exec.Cmd{}}
}

func (m *ScrcpyManager) Binary() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.binPath != "" {
		if _, err := os.Stat(m.binPath); err == nil {
			return m.binPath, nil
		}
		m.binPath = ""
	}
	candidates := []string{"scrcpy"}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/opt/homebrew/bin/scrcpy",
			"/usr/local/bin/scrcpy",
		)
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			m.binPath = p
			return p, nil
		}
		if _, err := os.Stat(c); err == nil {
			m.binPath = c
			return c, nil
		}
	}
	return "", errors.New("scrcpy not found in PATH (install: brew install scrcpy / apt install scrcpy)")
}

// Available reports whether scrcpy is callable on this host.
func (m *ScrcpyManager) Available() bool {
	_, err := m.Binary()
	return err == nil
}

// IsActive returns true when a tracked scrcpy process for the serial is still
// running. Reaps any zombie before answering.
func (m *ScrcpyManager) IsActive(serial string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd := m.procs[serial]
	if cmd == nil {
		return false
	}
	// Probe: ProcessState becomes non-nil only after Wait.
	if cmd.ProcessState != nil {
		delete(m.procs, serial)
		return false
	}
	// Send signal 0 to test liveness.
	if cmd.Process == nil {
		delete(m.procs, serial)
		return false
	}
	if err := cmd.Process.Signal(syscallZero); err != nil {
		delete(m.procs, serial)
		return false
	}
	return true
}

// scrcpyEnv returns an environment slice suitable for scrcpy. It copies the
// current process env, prepends Homebrew / local-bin to PATH (so scrcpy can
// find adb when the app is launched from Finder/Dock with a minimal PATH), and
// sets the ADB variable that scrcpy 4+ uses to locate the adb executable.
func scrcpyEnv() []string {
	env := os.Environ()

	// Prepend common tool paths so scrcpy can locate adb.
	extra := "/usr/local/bin:/opt/homebrew/bin"
	newEnv := make([]string, 0, len(env)+2)
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			e = "PATH=" + extra + ":" + v
		}
		newEnv = append(newEnv, e)
	}

	// Set the ADB env var that scrcpy 4+ honours directly.
	adbPath := resolveADBForScrcpy()
	if adbPath != "" {
		// Replace existing ADB= if present, else append.
		found := false
		for i, e := range newEnv {
			if strings.HasPrefix(e, "ADB=") {
				newEnv[i] = "ADB=" + adbPath
				found = true
				break
			}
		}
		if !found {
			newEnv = append(newEnv, "ADB="+adbPath)
		}
	}
	return newEnv
}

// resolveADBForScrcpy tries common locations for the adb binary.
func resolveADBForScrcpy() string {
	candidates := []string{
		"adb",
		"/usr/local/bin/adb",
		"/opt/homebrew/bin/adb",
	}
	if h := os.Getenv("HOME"); h != "" {
		candidates = append(candidates, h+"/Library/Android/sdk/platform-tools/adb")
	}
	if ah := os.Getenv("ANDROID_HOME"); ah != "" {
		candidates = append(candidates, ah+"/platform-tools/adb")
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Start launches `scrcpy -s SERIAL [extra args...]` detached from our stdio.
// If a scrcpy is already running for this serial, it is left as-is and the
// existing window comes to front (best-effort).
//
// A 400 ms grace window after start is used to detect immediate failures (bad
// flags, device not authorised, codec error, etc.) and surface them as an error
// rather than silently doing nothing.
func (m *ScrcpyManager) Start(ctx context.Context, serial string, extraArgs []string) error {
	if m.IsActive(serial) {
		return nil
	}
	bin, err := m.Binary()
	if err != nil {
		return err
	}
	args := append([]string{"-s", serial}, extraArgs...)
	cmd := exec.Command(bin, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	// Capture stderr so we can surface early-exit error messages.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	cmd.Env = scrcpyEnv()

	if err := cmd.Start(); err != nil {
		return err
	}
	m.mu.Lock()
	m.procs[serial] = cmd
	m.mu.Unlock()

	// done is closed by the Wait goroutine when scrcpy exits.
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
		m.mu.Lock()
		if m.procs[serial] == cmd {
			delete(m.procs, serial)
		}
		m.mu.Unlock()
	}()

	// If scrcpy exits within the grace window, treat it as a start failure.
	select {
	case <-done:
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			msg = "scrcpy exited immediately — check device authorisation and connection"
		}
		// Keep only the last meaningful line to avoid flooding the toast.
		if lines := strings.Split(msg, "\n"); len(lines) > 1 {
			for i := len(lines) - 1; i >= 0; i-- {
				if t := strings.TrimSpace(lines[i]); t != "" {
					msg = t
					break
				}
			}
		}
		return fmt.Errorf("%s", msg)
	case <-time.After(400 * time.Millisecond):
		return nil
	}
}

// Stop terminates the tracked scrcpy for the serial.
func (m *ScrcpyManager) Stop(serial string) error {
	m.mu.Lock()
	cmd := m.procs[serial]
	delete(m.procs, serial)
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// StopAll kills any tracked scrcpy windows (used on app shutdown).
func (m *ScrcpyManager) StopAll() {
	m.mu.Lock()
	procs := make([]*exec.Cmd, 0, len(m.procs))
	for _, c := range m.procs {
		procs = append(procs, c)
	}
	m.procs = map[string]*exec.Cmd{}
	m.mu.Unlock()
	for _, c := range procs {
		if c.Process != nil {
			_ = c.Process.Kill()
		}
	}
}
