package adb

import (
	"context"
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

// ListDir runs `ls -lAp` (optionally via su) and parses the rows.
func (c *Client) ListDir(ctx context.Context, serial, path string, asRoot bool) ([]FileEntry, error) {
	if path == "" {
		path = "/"
	}
	// Quote path
	q := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	command := "ls -lAp " + q
	var out string
	var err error
	if asRoot {
		out, _, err = c.ShellSU(ctx, serial, command)
	} else {
		out, err = c.Shell(ctx, serial, command)
	}
	if err != nil && out == "" {
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
		if !ok {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// parseLsLine handles both common `ls -l` layouts seen on Android, anchoring
// on the ISO date column so it works whether or not the link-count column is
// present (older toybox on e.g. API-21 emulators omits it):
//
//	-rw-r--r-- 1 user grp 1024 2026-05-22 12:18 name        (GNU/newer toybox)
//	-rwxr-xr-x   root root 53489240 2026-06-18 14:37 name   (older toybox, no link count)
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
	// Find the ISO date column (YYYY-MM-DD). Everything else is positioned
	// relative to it: size is the token before it, group/owner the two before
	// that (a link-count column, when present, sits further left and is
	// ignored), and the time + name follow.
	di := -1
	for i := 1; i < len(fields); i++ {
		if isISODate(fields[i]) {
			di = i
			break
		}
	}
	if di < 4 || di+1 >= len(fields) {
		return FileEntry{}, false
	}
	owner := fields[di-3]
	group := fields[di-2]
	size, _ := strconv.ParseInt(fields[di-1], 10, 64)
	date := fields[di] + " " + fields[di+1]
	// Name is everything after the time field, preserving embedded spaces.
	// Walk the original string consuming (di+2) whitespace-separated tokens.
	pos := 0
	for i := 0; i < di+2; i++ {
		// skip spaces
		for pos < len(s) && s[pos] == ' ' {
			pos++
		}
		// skip token
		for pos < len(s) && s[pos] != ' ' {
			pos++
		}
	}
	rest := strings.TrimSpace(s[pos:])
	name := rest
	link := ""
	if i := strings.Index(rest, " -> "); i >= 0 {
		name = rest[:i]
		link = rest[i+4:]
	}
	typ := "file"
	switch perms[0] {
	case 'd':
		typ = "dir"
	case 'l':
		typ = "link"
	}
	// `-p` adds trailing slash on directories
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

// isISODate reports whether s is a YYYY-MM-DD date, the format Android's ls
// uses for the modification-time column.
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
