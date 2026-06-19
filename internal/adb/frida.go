package adb

import (
	"context"
	"strconv"
	"strings"
)

// FridaServer represents a frida-server binary in /data/local/tmp.
type FridaServer struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
	Size    int64  `json:"size"`
	Perms   string `json:"perms"`
	Active  bool   `json:"active"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
}

// FridaProcess is a process the frida server is observing (we cannot list
// attachments without talking to frida itself; this is a best-effort fallback
// based on `ps -A`).
type FridaProcess struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// ListFridaServers scans /data/local/tmp for frida-server* binaries.
func (c *Client) ListFridaServers(ctx context.Context, serial string) ([]FridaServer, error) {
	out, err := c.Shell(ctx, serial, "ls -l /data/local/tmp/ 2>/dev/null | grep -i frida-server || true")
	if err != nil && out == "" {
		return nil, err
	}
	res := []FridaServer{}
	running := c.runningFrida(ctx, serial)
	for _, ln := range strings.Split(out, "\n") {
		entry, ok := parseLsLine(strings.TrimRight(ln, "\r"))
		if !ok {
			continue
		}
		if !strings.Contains(entry.Name, "frida-server") {
			continue
		}
		fs := FridaServer{
			Name:    entry.Name,
			Path:    "/data/local/tmp/" + entry.Name,
			Size:    entry.Size,
			Perms:   entry.Perms,
			Version: extractFridaVersion(entry.Name),
			Arch:    extractFridaArch(entry.Name),
		}
		for _, rp := range running {
			if rp.matches(fs.Path, fs.Name) {
				fs.Active = true
				fs.PID = rp.pid
				fs.Port = 27042
				break
			}
		}
		res = append(res, fs)
	}
	return res, nil
}

// fridaProc is a running frida-server discovered via procfs.
type fridaProc struct {
	pid  int
	comm string // /proc/<pid>/comm (truncated to 15 chars by the kernel)
	exe  string // readlink /proc/<pid>/exe — the full binary path, when available
}

// matches reports whether this process is the given on-device binary. Prefers an
// exact /proc/<pid>/exe match; falls back to the (truncated) comm being a prefix
// of the filename, which is how the kernel reports e.g. "frida-server-17".
func (p fridaProc) matches(path, name string) bool {
	if p.exe != "" {
		return p.exe == path
	}
	return p.comm != "" && strings.HasPrefix(name, p.comm)
}

// runningFrida scans procfs for live frida-server processes. It avoids ps/grep/
// tr/pkill — minimal ROMs ship none of those — using only shell builtins plus
// readlink. Reads run as root since frida-server runs as root.
func (c *Client) runningFrida(ctx context.Context, serial string) []fridaProc {
	const script = `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; case "$c" in *frida-server*) echo "${p##*/} $c $(readlink "$p/exe" 2>/dev/null)";; esac; done`
	out, _, _ := c.ShellSU(ctx, serial, script)
	if strings.TrimSpace(out) == "" {
		out, _ = c.Shell(ctx, serial, script)
	}
	var procs []fridaProc
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimSpace(ln))
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		p := fridaProc{pid: pid, comm: f[1]}
		if len(f) >= 3 {
			p.exe = f[2]
		}
		procs = append(procs, p)
	}
	return procs
}

func extractFridaVersion(name string) string {
	// e.g. frida-server-16.4.7-android-arm64
	parts := strings.Split(name, "-")
	for _, p := range parts {
		if len(p) > 0 && (p[0] >= '0' && p[0] <= '9') && strings.Contains(p, ".") {
			return p
		}
	}
	return ""
}

func extractFridaArch(name string) string {
	for _, a := range []string{"arm64", "arm", "x86_64", "x86"} {
		if strings.Contains(name, a) {
			return a
		}
	}
	return ""
}

// StartFrida launches the given frida-server bound to iface:port. Requires
// root; if iface is "", binds 0.0.0.0.
//
// It uses frida-server's own -D/--daemonize rather than setsid/nohup/& — many
// stripped ROMs lack setsid/nohup, and -D makes frida fork a proper daemon that
// survives the shell exit on every device.
func (c *Client) StartFrida(ctx context.Context, serial, serverPath, iface string, port int) (string, error) {
	if port <= 0 {
		port = 27042
	}
	if iface == "" {
		iface = "0.0.0.0"
	}
	q := shQuote(serverPath)
	cmd := "chmod 755 " + q + " && " + q + " -l " + iface + ":" + strconv.Itoa(port) + " -D"
	out, _, err := c.ShellSU(ctx, serial, cmd)
	return out, err
}

// StopFrida kills running frida-server processes. Uses a procfs scan + the kill
// builtin instead of pkill/killall, which minimal ROMs don't ship.
func (c *Client) StopFrida(ctx context.Context, serial string) (string, error) {
	const script = `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; case "$c" in *frida-server*) kill "${p##*/}" 2>/dev/null; kill -9 "${p##*/}" 2>/dev/null;; esac; done; echo stopped`
	out, _, err := c.ShellSU(ctx, serial, script)
	return out, err
}
