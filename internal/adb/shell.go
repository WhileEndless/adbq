package adb

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
)

// ShellSession is an interactive `adb shell` running inside a host-side PTY
// (allocated via github.com/creack/pty). Using a PTY on our side fixes line
// buffering (so device echo arrives byte-by-byte), gives the device a real
// controlling terminal, and lets us send signals via Ctrl-byte sequences.
type ShellSession struct {
	ID       string
	Root     bool
	cmd      *exec.Cmd
	pty      *os.File // bidirectional: read AND write through the same fd
	out      chan []byte
	stopOnce sync.Once
	done     chan struct{}

	// firstData is closed by pump() once the device emits its first output
	// chunk (the PS1 prompt). Callers can use it to wait for the shell to be
	// truly interactive before injecting commands like `su root`.
	firstData chan struct{}
	firstOnce sync.Once

	// Optional tee writer for persisting scrollback to disk.
	teeMu sync.Mutex
	tee   io.Writer
}

// StartShell opens an interactive `adb -s serial shell -t -t` inside a host
// PTY. The `-t -t` forces the device-side shell to allocate its own PTY too,
// so the user gets proper $PS1, tab completion, vi/top alt-screen handling,
// and ANSI colors.
//
// If root is true, `su\n` is written immediately so the user starts at a #
// prompt on a Magisk-style device.
func (c *Client) StartShell(ctx context.Context, serial, id string, root bool) (*ShellSession, error) {
	bin, err := c.Binary()
	if err != nil {
		return nil, err
	}
	args := []string{"-s", serial, "shell", "-t", "-t"}
	cmd := exec.CommandContext(ctx, bin, args...)
	// pty.Start allocates a master/slave pair, attaches the slave to the
	// child's stdio (including stderr), and returns the master *os.File.
	ptyFile, err := pty.Start(cmd)
	if err != nil {
		// creack/pty has no Windows implementation — it returns ErrUnsupported,
		// which reaches the user as an unexplained failure. Say what is actually
		// missing and what to do instead; the rest of adbq works there.
		if runtime.GOOS == "windows" {
			return nil, fmt.Errorf("the interactive shell needs a pseudo-terminal, which adbq does not implement on Windows yet — " +
				"use `adb -s " + serial + " shell` in a terminal, or the one-shot command box on other screens")
		}
		return nil, err
	}
	countStreamSpawn(cmd.Args)
	s := &ShellSession{
		ID:        id,
		Root:      root,
		cmd:       cmd,
		pty:       ptyFile,
		out:       make(chan []byte, 64),
		done:      make(chan struct{}),
		firstData: make(chan struct{}),
	}
	go s.pump()
	if root {
		// Pick the elevation command from the same probe every other privileged
		// path uses, so the interactive shell agrees with ShellSU: skip su
		// entirely when the shell is already uid 0 (adbd root / userdebug), use
		// the uid-positional `su 0` form where that's what works, else `su root`.
		elevate := c.interactiveElevateCmd(ctx, serial)
		if elevate != "" {
			// We MUST wait until the device-side shell has rendered its first
			// prompt before writing — otherwise the bytes arrive before adb's
			// PTY is interactive and get dropped (the "su root yazmadı gibi"
			// symptom). Listen for the first PTY output chunk, then inject.
			go func() {
				select {
				case <-s.firstData:
				case <-time.After(3 * time.Second):
				}
				// Brief settle delay so the shell isn't mid-prompt when we type.
				time.Sleep(120 * time.Millisecond)
				_, _ = ptyFile.Write([]byte(elevate + "\n"))
			}()
		}
	}
	return s, nil
}

// interactiveElevateCmd returns the command to type into an interactive shell to
// become root, based on the device's probed su style. Returns "" when the shell
// is already root (no su needed).
func (c *Client) interactiveElevateCmd(ctx context.Context, serial string) string {
	style, _ := c.suStyleFor(ctx, serial)
	switch style {
	case suBareRoot:
		return ""
	case suZeroSimple, suZeroShWrap:
		return "su 0"
	default:
		// su / sh-wrap / unknown: `su root` is the form Magisk and standard su
		// honor; an unknown style still best-effort tries it so the user sees
		// the device's own su error in the terminal if it's not granted.
		return "su root"
	}
}

func (s *ShellSession) pump() {
	defer close(s.out)
	defer close(s.done)
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			cp := make([]byte, n)
			copy(cp, buf[:n])
			// Signal first-data once so `su root` injection can wait until the
			// device shell is interactive. After that the Once is a no-op.
			s.firstOnce.Do(func() { close(s.firstData) })
			// fan out: live channel + persisted tee (if any)
			s.out <- cp
			s.teeMu.Lock()
			t := s.tee
			s.teeMu.Unlock()
			if t != nil {
				_, _ = t.Write(cp)
			}
		}
		if err != nil {
			return
		}
	}
}

// Output returns the read-only channel of PTY output chunks.
func (s *ShellSession) Output() <-chan []byte { return s.out }

// Write injects bytes into the PTY (stdin of the device shell).
func (s *ShellSession) Write(data string) error {
	_, err := s.pty.Write([]byte(data))
	return err
}

// Resize tells the kernel + device about a new terminal geometry. xterm.js
// emits resize events whenever the user changes the window; we forward them.
func (s *ShellSession) Resize(cols, rows uint16) error {
	if s.pty == nil {
		return nil
	}
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// FlushTee pushes anything the tee has buffered to disk.
//
// The scrollback writer batches its appends, so a log being read while its
// session is still open would otherwise be missing its newest output — the
// part someone reading it is most likely after.
func (s *ShellSession) FlushTee() {
	s.teeMu.Lock()
	t := s.tee
	s.teeMu.Unlock()
	if f, ok := t.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}
}

// SetTee redirects a copy of all PTY output to w. Used to persist scrollback
// to ~/.adbq/scrollback so it survives an app restart.
func (s *ShellSession) SetTee(w io.Writer) {
	s.teeMu.Lock()
	s.tee = w
	s.teeMu.Unlock()
}

// Stop kills the underlying adb process and closes the PTY.
func (s *ShellSession) Stop() {
	s.stopOnce.Do(func() {
		if s.pty != nil {
			_ = s.pty.Close()
		}
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
	<-s.done
}
