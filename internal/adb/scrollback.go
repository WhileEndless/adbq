package adb

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const scrollbackCap = 256 * 1024 // bytes

// ScrollbackEntry is a saved per-session log file we can replay into a fresh
// xterm.js Terminal on next launch.
type ScrollbackEntry struct {
	Path      string `json:"path"`
	Serial    string `json:"serial"`
	Label     string `json:"label"`
	UpdatedAt int64  `json:"updatedAt"`
	Bytes     int64  `json:"bytes"`
}

// scrollbackDir is ~/.adbq/scrollback/.
func scrollbackDir() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, "scrollback")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// ScrollbackPath returns the per-session log file path. label is the human
// label shown in the UI (used as a stable disambiguator).
func ScrollbackPath(serial, label string) (string, error) {
	d, err := scrollbackDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, sanitize(serial)+"-"+sanitize(label)+".log"), nil
}

// OpenScrollbackWriter returns an io.Writer that appends to the session log
// AND keeps the file capped to scrollbackCap. The truncation happens at
// rotation time (when the file exceeds 2*cap we trim it back to cap).
func OpenScrollbackWriter(serial, label string) (io.WriteCloser, error) {
	p, err := ScrollbackPath(serial, label)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &cappedWriter{f: f, path: p}, nil
}

type cappedWriter struct {
	f     *os.File
	path  string
	count int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.count += n
	// Every ~64KB written, check size and trim if needed.
	if w.count > 64*1024 {
		w.count = 0
		_ = w.trim()
	}
	return n, err
}

func (w *cappedWriter) Close() error { return w.f.Close() }

// trim rotates the log: if it exceeds 2*cap, keep only the last cap bytes.
func (w *cappedWriter) trim() error {
	info, err := os.Stat(w.path)
	if err != nil {
		return err
	}
	if info.Size() < 2*scrollbackCap {
		return nil
	}
	// Read tail, rewrite file.
	tail, err := os.ReadFile(w.path)
	if err != nil {
		return err
	}
	if len(tail) > scrollbackCap {
		tail = tail[len(tail)-scrollbackCap:]
	}
	// Re-open w/ truncate
	if err := w.f.Close(); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(tail); err != nil {
		return err
	}
	w.f = f
	return nil
}

// ReadScrollback returns the persisted log content (capped at scrollbackCap).
func ReadScrollback(serial, label string) (string, error) {
	p, err := ScrollbackPath(serial, label)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if len(b) > scrollbackCap {
		b = b[len(b)-scrollbackCap:]
	}
	return string(b), nil
}

// ClearScrollback removes the on-disk log for the session.
func ClearScrollback(serial, label string) error {
	p, err := ScrollbackPath(serial, label)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListScrollbacks enumerates all persisted session logs so the UI can show a
// "History" list for previous adbq runs.
func ListScrollbacks() ([]ScrollbackEntry, error) {
	d, err := scrollbackDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil, err
	}
	var out []ScrollbackEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".log")
		serial, label, _ := strings.Cut(name, "-")
		out = append(out, ScrollbackEntry{
			Path:      filepath.Join(d, e.Name()),
			Serial:    serial,
			Label:     label,
			UpdatedAt: info.ModTime().Unix(),
			Bytes:     info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
