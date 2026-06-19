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

	// capMu guards caps, the per-serial cache of device Capabilities (SDK,
	// SELinux, ABI, binary presence) — see capabilities.go.
	capMu sync.Mutex
	caps  map[string]*Capabilities
}

// suStyle records how a device grants root. Devices differ widely:
//   - Magisk / most modern su run `-c`'s argument through a shell, so
//     `su -c 'cmd arg1 arg2'` works (suSimple).
//   - AOSP-style su (older emulators, some ROMs) execs the whole argument as a
//     single program path — there `su -c 'tcpdump --version'` fails with "exec
//     failed for tcpdump --version: No such file or directory" and you must
//     spell out the shell yourself via `su -c sh -c '...'` (suShWrap).
//   - Some Superuser/KernelSU/APatch configs only word-split with the uid
//     positional form `su 0 -c '...'` / `su 0 sh -c '...'` (suZero*).
//   - userdebug/eng builds and `adb root` emulators run the adb shell as uid 0
//     with NO `su` binary at all; there the command must run directly, unwrapped
//     (suBareRoot). Without this, every root feature wrongly reports "root
//     unavailable" on the single most common dev/pentest target.
//
// We probe once and cache the working form so every root caller (capture,
// frida, hosts, certs, …) just works.
type suStyle int

const (
	suUnknown    suStyle = iota
	suBareRoot           // shell already uid 0; run <cmd> directly (adbd root / userdebug)
	suSimple             // su -c '<cmd>'         (Magisk / modern)
	suShWrap             // su -c sh -c '<cmd>'   (AOSP-style)
	suZeroSimple         // su 0 -c '<cmd>'       (uid-positional)
	suZeroShWrap         // su 0 sh -c '<cmd>'    (uid-positional, AOSP-style)
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
	case suBareRoot:
		// Shell is already uid 0 (adbd root / userdebug); no `su` needed.
		return inner, nil
	case suSimple:
		return "su -c " + shQuote(inner), nil
	case suShWrap:
		return "su -c sh -c " + shQuote(inner), nil
	case suZeroSimple:
		return "su 0 -c " + shQuote(inner), nil
	case suZeroShWrap:
		return "su 0 sh -c " + shQuote(inner), nil
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

	// (0) Already root? userdebug/eng builds and `adb root` emulators run the
	// adb shell as uid 0 with no `su` binary. `id` exists on every Android
	// toolbox/toybox; a uid=0 line means we can run commands unwrapped.
	if out, _ := c.Shell(ctx, serial, "id"); hasUID0(out) {
		c.setSuStyle(serial, suBareRoot)
		return suBareRoot, nil
	}

	// The marker echo carries an argument on purpose: AOSP-style su fails the
	// simple form precisely because it can't word-split `echo ADBQ_SU_OK`.
	//
	// Match on a *line equal to* the marker, not merely containing it: `adb
	// shell` folds the remote stderr into stdout, and AOSP su's failure message
	// ("su: exec failed for echo ADBQ_SU_OK …") echoes the command — so a naive
	// Contains check would see the marker inside the error and wrongly conclude
	// the simple form worked.
	//
	// Order matters: the simple/shWrap forms match first on Magisk-style su (su0
	// would also work there), so the uid-positional forms are only reached on
	// the Superuser/KernelSU configs that actually need them.
	const marker = "ADBQ_SU_OK"
	probe := "echo " + marker
	for _, cand := range []struct {
		style suStyle
		cmd   string
	}{
		{suSimple, "su -c " + shQuote(probe)},
		{suShWrap, "su -c sh -c " + shQuote(probe)},
		{suZeroSimple, "su 0 -c " + shQuote(probe)},
		{suZeroShWrap, "su 0 sh -c " + shQuote(probe)},
	} {
		if out, _ := c.Shell(ctx, serial, cand.cmd); hasMarkerLine(out, marker) {
			c.setSuStyle(serial, cand.style)
			return cand.style, nil
		}
	}
	return suUnknown, fmt.Errorf("root unavailable: `su` did not run a command on %s (device not rooted, or su denied)", serial)
}

// hasUID0 reports whether `id` output describes uid 0 (root). It matches the
// `uid=0(` / `uid=0 ` prefix rather than a bare "uid=0" substring so uids like
// 1000 followed by a gid list (e.g. "uid=1000(system) … groups=…,0(root)")
// can't be misread as root.
func hasUID0(idOut string) bool {
	s := strings.TrimSpace(idOut)
	return strings.HasPrefix(s, "uid=0(") || strings.HasPrefix(s, "uid=0 ") || s == "uid=0"
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
