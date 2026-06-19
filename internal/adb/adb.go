// Package adb wraps the local `adb` binary. All commands are run via os/exec
// with explicit context so callers can cancel long-running operations.
package adb

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Client locates the adb binary lazily and provides command builders.
type Client struct {
	mu      sync.Mutex
	binPath string
	// Live tracks active in-process pcap streams (see pcap_live.go). Lazy-init
	// on first use; nil means no live captures have been started yet.
	Live *LiveCapture

	// suMu guards suStyles, the per-serial cache of which `su -c` form the
	// device's su accepts (see rootWrap).
	suMu     sync.Mutex
	suStyles map[string]suStyle
}

// suStyle records how a device's `su` parses `-c`. Devices differ: Magisk and
// most modern su run the argument through a shell (so `su -c 'cmd arg1 arg2'`
// works), whereas AOSP-style su (older emulators, some ROMs) execs the whole
// argument as a single program path — there `su -c 'tcpdump --version'` fails
// with "exec failed for tcpdump --version: No such file or directory" and you
// must spell out the shell yourself via `su -c sh -c '...'`. We probe once and
// cache the working form so every root caller (capture, frida, …) just works.
type suStyle int

const (
	suUnknown suStyle = iota
	suSimple          // su -c '<cmd>'        (Magisk / modern)
	suShWrap          // su -c sh -c '<cmd>'  (AOSP-style)
)

func NewClient() *Client { return &Client{} }

// Binary returns the resolved adb binary path; locates it the first time.
func (c *Client) Binary() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.binPath != "" {
		return c.binPath, nil
	}
	candidates := []string{"adb"}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/usr/local/bin/adb",
			"/opt/homebrew/bin/adb",
			os.ExpandEnv("$HOME/Library/Android/sdk/platform-tools/adb"),
		)
	}
	if andHome := os.Getenv("ANDROID_HOME"); andHome != "" {
		candidates = append(candidates, andHome+"/platform-tools/adb")
	}
	if andSDK := os.Getenv("ANDROID_SDK_ROOT"); andSDK != "" {
		candidates = append(candidates, andSDK+"/platform-tools/adb")
	}
	for _, c0 := range candidates {
		if c0 == "" {
			continue
		}
		if p, err := exec.LookPath(c0); err == nil {
			c.binPath = p
			return p, nil
		}
		if _, err := os.Stat(c0); err == nil {
			c.binPath = c0
			return c0, nil
		}
	}
	return "", errors.New("adb binary not found in PATH or common locations")
}

// SetBinary lets the user override the resolved path (e.g., from settings UI).
func (c *Client) SetBinary(p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.binPath = p
}

// Command builds an exec.Cmd for `adb <args...>` without targeting a device.
func (c *Client) Command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, bin, args...), nil
}

// DeviceCommand builds `adb -s <serial> <args...>`.
func (c *Client) DeviceCommand(ctx context.Context, serial string, args ...string) (*exec.Cmd, error) {
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	full := append([]string{"-s", serial}, args...)
	return exec.CommandContext(ctx, bin, full...), nil
}

// Run executes the command and returns trimmed stdout, or an error containing stderr.
func Run(cmd *exec.Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		s := strings.TrimSpace(stderr.String())
		if s == "" {
			s = err.Error()
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("%s: %s", cmd.Path, s)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Shell runs `adb -s <serial> shell <cmd>` and returns trimmed stdout.
func (c *Client) Shell(ctx context.Context, serial, command string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "shell", command)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// rootWrap returns the full remote command string that runs `inner` as root on
// the device, choosing the `su -c` form the device actually accepts (see
// suStyle). Pass the result as a SINGLE arg to `adb shell` — adb space-joins
// separate args without re-quoting, which would split `inner` back apart.
// Returns an error when root is unavailable.
func (c *Client) rootWrap(ctx context.Context, serial, inner string) (string, error) {
	style, err := c.suStyleFor(ctx, serial)
	if err != nil {
		return "", err
	}
	switch style {
	case suSimple:
		return "su -c " + shQuote(inner), nil
	case suShWrap:
		return "su -c sh -c " + shQuote(inner), nil
	}
	return "", fmt.Errorf("root unavailable on %s", serial)
}

// suStyleFor probes (once, then cached) which `su -c` form works on the device.
// A negative result is NOT cached so a later Magisk grant can still succeed.
func (c *Client) suStyleFor(ctx context.Context, serial string) (suStyle, error) {
	c.suMu.Lock()
	if c.suStyles == nil {
		c.suStyles = map[string]suStyle{}
	}
	if st := c.suStyles[serial]; st != suUnknown {
		c.suMu.Unlock()
		return st, nil
	}
	c.suMu.Unlock()

	// The marker echo carries an argument on purpose: AOSP-style su fails the
	// simple form precisely because it can't word-split `echo ADBQ_SU_OK`.
	//
	// Match on a *line equal to* the marker, not merely containing it: `adb
	// shell` folds the remote stderr into stdout, and AOSP su's failure message
	// ("su: exec failed for echo ADBQ_SU_OK …") echoes the command — so a naive
	// Contains check would see the marker inside the error and wrongly conclude
	// the simple form worked.
	const marker = "ADBQ_SU_OK"
	probe := "echo " + marker
	if out, _ := c.Shell(ctx, serial, "su -c "+shQuote(probe)); hasMarkerLine(out, marker) {
		c.setSuStyle(serial, suSimple)
		return suSimple, nil
	}
	if out, _ := c.Shell(ctx, serial, "su -c sh -c "+shQuote(probe)); hasMarkerLine(out, marker) {
		c.setSuStyle(serial, suShWrap)
		return suShWrap, nil
	}
	return suUnknown, fmt.Errorf("root unavailable: `su` did not run a command on %s (device not rooted, or su denied)", serial)
}

// hasMarkerLine reports whether any line of out, trimmed, equals marker.
func hasMarkerLine(out, marker string) bool {
	for _, ln := range strings.Split(out, "\n") {
		if strings.TrimSpace(ln) == marker {
			return true
		}
	}
	return false
}

func (c *Client) setSuStyle(serial string, st suStyle) {
	c.suMu.Lock()
	if c.suStyles == nil {
		c.suStyles = map[string]suStyle{}
	}
	c.suStyles[serial] = st
	c.suMu.Unlock()
}

// ShellSU executes a shell command as root. Returns stdout and a boolean
// indicating whether su appears to be unavailable (e.g., non-rooted device).
func (c *Client) ShellSU(ctx context.Context, serial, command string) (string, bool, error) {
	wrapped, err := c.rootWrap(ctx, serial, command)
	if err != nil {
		// rootWrap only fails when no `su -c` form worked → treat as unavailable.
		return "", true, err
	}
	out, err := c.Shell(ctx, serial, wrapped)
	if err != nil {
		low := strings.ToLower(err.Error() + " " + out)
		if strings.Contains(low, "not found") || strings.Contains(low, "permission denied") || strings.Contains(low, "no such file") {
			return out, true, err
		}
		return out, false, err
	}
	return out, false, nil
}

// ShellTimeout convenience for short-lived shell calls.
func (c *Client) ShellTimeout(parent context.Context, serial, command string, d time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	return c.Shell(ctx, serial, command)
}

// LineScanner returns a buffered scanner with a large line cap suited to
// long log lines.
func LineScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return s
}

// ServerVersion returns the adb server version line.
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	cmd, err := c.Command(ctx, "version")
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// StartServer ensures the adb server is running.
func (c *Client) StartServer(ctx context.Context) error {
	cmd, err := c.Command(ctx, "start-server")
	if err != nil {
		return err
	}
	_, err = Run(cmd)
	return err
}

// Connect attaches a TCP device, e.g. "192.168.1.51:5555".
func (c *Client) Connect(ctx context.Context, addr string) (string, error) {
	cmd, err := c.Command(ctx, "connect", addr)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// Disconnect detaches a TCP device or all if addr is empty.
func (c *Client) Disconnect(ctx context.Context, addr string) (string, error) {
	args := []string{"disconnect"}
	if addr != "" {
		args = append(args, addr)
	}
	cmd, err := c.Command(ctx, args...)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// KillServer terminates the adb server.
func (c *Client) KillServer(ctx context.Context) error {
	cmd, err := c.Command(ctx, "kill-server")
	if err != nil {
		return err
	}
	_, err = Run(cmd)
	return err
}
