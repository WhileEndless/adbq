package adb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// FileEntry represents one row in a directory listing.
type FileEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // file, dir, link, up
	Size  int64  `json:"size"`
	Perms string `json:"perms"`
	Owner string `json:"owner"`
	Group string `json:"group"`
	Mtime string `json:"mtime"`
	Link  string `json:"link,omitempty"`
}

// ListDir lists a directory via `ls -l`, parses the rows, and degrades
// gracefully on old/minimal ROMs. The preferred form is `ls -lAp` (almost-all,
// trailing-slash on dirs), but the toolbox `ls` on API ≤22 lacks `-A`/`-p` and
// can reject the combined flags — so when that yields no parseable rows we retry
// with the universally-supported `ls -la` and filter `.`/`..` ourselves.
func (c *Client) ListDir(ctx context.Context, serial, path string, asRoot bool) ([]FileEntry, error) {
	if path == "" {
		path = "/"
	}
	q := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"

	out, err := c.runLs(ctx, serial, "ls -lAp "+q, asRoot)
	parsed, content := countLsRows(out)
	// Format/flag mismatch (e.g. toolbox ls): output arrived but nothing parsed.
	// Retry with the portable form before giving up.
	if parsed == 0 && content > 0 {
		if out2, err2 := c.runLs(ctx, serial, "ls -la "+q, asRoot); err2 == nil || out2 != "" {
			out, err = out2, err2
		}
	}
	if (err != nil && strings.TrimSpace(out) == "") || isPermissionDenied(out) {
		if isPermissionDenied(out) || isPermissionDenied(errString(err)) {
			return nil, fmt.Errorf("permission denied listing %s — enable Root to browse this path", path)
		}
		return nil, err
	}

	entries := []FileEntry{}
	if path != "/" {
		entries = append(entries, FileEntry{Name: "..", Type: "up"})
	}
	for _, ln := range strings.Split(out, "\n") {
		t := strings.TrimRight(ln, "\r")
		if t == "" || strings.HasPrefix(t, "total ") {
			continue
		}
		e, ok := parseLsLine(t)
		if !ok || e.Name == "." || e.Name == ".." {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (c *Client) runLs(ctx context.Context, serial, command string, asRoot bool) (string, error) {
	if asRoot {
		out, _, err := c.ShellSU(ctx, serial, command)
		return out, err
	}
	return c.Shell(ctx, serial, command)
}

// countLsRows reports how many content lines parsed as ls entries (parsed) and
// how many non-empty, non-"total" content lines there were (content). A
// content>0, parsed==0 result means the output didn't match any known layout.
func countLsRows(out string) (parsed, content int) {
	for _, ln := range strings.Split(out, "\n") {
		t := strings.TrimRight(ln, "\r")
		if t == "" || strings.HasPrefix(t, "total ") {
			continue
		}
		content++
		if _, ok := parseLsLine(t); ok {
			parsed++
		}
	}
	return parsed, content
}

func isPermissionDenied(s string) bool {
	return strings.Contains(strings.ToLower(s), "permission denied")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// parseLsLine handles the `ls -l` layouts seen across Android, anchoring on the
// modification-time column so it works whether or not the link-count column is
// present (older toybox on e.g. API-21 emulators omits it) and across both the
// ISO date format (toybox, API 23+) and the BSD/toolbox "MMM DD" format
// (toolbox, API ≤22):
//
//	-rw-r--r-- 1 user grp 1024 2026-05-22 12:18 name        (toybox ISO)
//	-rwxr-xr-x   root root 53489240 2026-06-18 14:37 name   (toybox ISO, no link count)
//	-rw-r--r-- 1 root root 1024 Jun 18 14:37 name           (toolbox, current year)
//	-rw-r--r-- 1 root root 1024 Jun 18 2024 name            (toolbox, old file → year)
//	lrwxrwxrwx 1 root root 8 2026-... link -> target
func parseLsLine(s string) (FileEntry, bool) {
	fields := strings.Fields(s)
	if len(fields) < 6 {
		return FileEntry{}, false
	}
	perms := fields[0]
	if len(perms) < 10 {
		return FileEntry{}, false
	}
	// Locate the date column and how many tokens it spans (dn): 2 for ISO
	// "date time", 3 for BSD "month day time/year". Everything to its left is
	// positioned relative to it — size is the token before, owner/group the two
	// before that (an optional link-count column sits further left and is
	// ignored) — and the name follows the date.
	di, dn := findLsDateColumn(fields)
	if di < 4 || di+dn >= len(fields) {
		return FileEntry{}, false
	}
	owner := fields[di-3]
	group := fields[di-2]
	size, _ := strconv.ParseInt(fields[di-1], 10, 64)
	date := strings.Join(fields[di:di+dn], " ")
	// Name is everything after the date tokens, preserving embedded spaces.
	rest := strings.TrimSpace(skipFields(s, di+dn))
	if rest == "" {
		return FileEntry{}, false
	}
	name := rest
	link := ""
	if before, after, found := strings.Cut(rest, " -> "); found {
		name = before
		link = after
	}
	typ := "file"
	switch perms[0] {
	case 'd':
		typ = "dir"
	case 'l':
		typ = "link"
	}
	// `-p` adds a trailing slash on directories.
	if strings.HasSuffix(name, "/") {
		typ = "dir"
		name = strings.TrimSuffix(name, "/")
	}
	return FileEntry{
		Name:  name,
		Type:  typ,
		Size:  size,
		Perms: perms,
		Owner: owner,
		Group: group,
		Mtime: date,
		Link:  link,
	}, true
}

// findLsDateColumn returns the index of the modification-time column and the
// number of tokens it spans (dn), or (-1, 0) when no date is found. It scans
// from index 4 since the date can never precede perms+owner+group+size.
func findLsDateColumn(fields []string) (di, dn int) {
	for i := 4; i < len(fields); i++ {
		if isISODate(fields[i]) {
			if i+1 < len(fields) && isClockHHMM(fields[i+1]) {
				return i, 2
			}
			return i, 1
		}
		if i+2 < len(fields) && isMonthAbbr(fields[i]) && isDayNum(fields[i+1]) &&
			(isClockHHMM(fields[i+2]) || isYear4(fields[i+2])) {
			return i, 3
		}
	}
	return -1, 0
}

// isISODate reports whether s is a YYYY-MM-DD date.
func isISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, r := range s {
		if i == 4 || i == 7 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isMonthAbbr(s string) bool {
	switch s {
	case "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec":
		return true
	}
	return false
}

// isDayNum reports whether s is a 1- or 2-digit day-of-month.
func isDayNum(s string) bool {
	if len(s) < 1 || len(s) > 2 || !isAllDigits(s) {
		return false
	}
	n, _ := strconv.Atoi(s)
	return n >= 1 && n <= 31
}

// isClockHHMM reports whether s looks like HH:MM or HH:MM:SS.
func isClockHHMM(s string) bool {
	if len(s) < 4 || strings.IndexByte(s, ':') < 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != ':' && (s[i] < '0' || s[i] > '9') {
			return false
		}
	}
	return true
}

func isYear4(s string) bool { return len(s) == 4 && isAllDigits(s) }

// PushFile copies a local file to remote on the device.
func (c *Client) PushFile(ctx context.Context, serial, local, remote string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "push", local, remote)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// PullFile copies a remote file to local.
func (c *Client) PullFile(ctx context.Context, serial, remote, local string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "pull", remote, local)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// RemoveFile deletes a file or directory; pass recursive for directories.
func (c *Client) RemoveFile(ctx context.Context, serial, path string, recursive, asRoot bool) (string, error) {
	flag := "-f"
	if recursive {
		flag = "-rf"
	}
	q := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	cmd := "rm " + flag + " " + q
	if asRoot {
		out, _, err := c.ShellSU(ctx, serial, cmd)
		return out, err
	}
	return c.Shell(ctx, serial, cmd)
}

// Chmod changes the mode of a file/dir; mode is in `chmod`-compatible form
// (e.g. "755", "u+x").
func (c *Client) Chmod(ctx context.Context, serial, path, mode string, asRoot bool) (string, error) {
	q := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	cmd := "chmod " + mode + " " + q
	if asRoot {
		out, _, err := c.ShellSU(ctx, serial, cmd)
		return out, err
	}
	return c.Shell(ctx, serial, cmd)
}

// Chown changes the owner (user[:group]) of a file/dir.
func (c *Client) Chown(ctx context.Context, serial, path, owner string, asRoot bool) (string, error) {
	q := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	cmd := "chown " + owner + " " + q
	if asRoot {
		out, _, err := c.ShellSU(ctx, serial, cmd)
		return out, err
	}
	return c.Shell(ctx, serial, cmd)
}

// MoveFileOnDevice renames a remote path.
func (c *Client) MoveFileOnDevice(ctx context.Context, serial, src, dst string, asRoot bool) (string, error) {
	cmd := "mv '" + strings.ReplaceAll(src, "'", `'\''`) + "' '" + strings.ReplaceAll(dst, "'", `'\''`) + "'"
	if asRoot {
		out, _, err := c.ShellSU(ctx, serial, cmd)
		return out, err
	}
	return c.Shell(ctx, serial, cmd)
}

// Mkdir creates a directory.
func (c *Client) Mkdir(ctx context.Context, serial, path string, asRoot bool) (string, error) {
	q := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	cmd := "mkdir -p " + q
	if asRoot {
		out, _, err := c.ShellSU(ctx, serial, cmd)
		return out, err
	}
	return c.Shell(ctx, serial, cmd)
}
