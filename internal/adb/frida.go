package adb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	// Glob inside the dir (so names are basenames) instead of `ls | grep` —
	// `grep` is absent on stripped ROMs. A non-matching glob just yields empty
	// output. parseLsLine tolerates both ISO and BSD/toolbox date columns.
	out, err := c.Shell(ctx, serial, "cd /data/local/tmp 2>/dev/null && ls -l *frida-server* 2>/dev/null")
	if err != nil && out == "" {
		return []FridaServer{}, nil
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
	exe  string // readlink /proc/<pid>/exe — full binary path, when readable
	cmd0 string // /proc/<pid>/cmdline argv[0] — full path, when readable
}

// matches reports whether this process is the given on-device binary. Prefers an
// exact full-path match (exe, then cmdline argv[0]); only when neither is
// readable (e.g. a root-owned process scanned without root) does it fall back to
// the truncated comm being a prefix of the filename. The cmdline path resolves
// the 15-char comm ambiguity between e.g. frida-server-16.x and -16.y.
func (p fridaProc) matches(path, name string) bool {
	if p.exe != "" {
		return p.exe == path // authoritative when readable
	}
	if p.cmd0 != "" && p.cmd0 == path {
		return true
	}
	// exe unreadable and cmdline argv[0] didn't match (or a shell that strips
	// NULs mangled it) → fall back to the truncated comm prefix.
	return p.comm != "" && strings.HasPrefix(name, p.comm)
}

// runningFrida scans procfs for live frida-server processes. It avoids ps/grep/
// tr/pkill — minimal ROMs ship none of those — using only shell builtins plus
// readlink. Reads run as root since frida-server runs as root. Fields are
// '|'-delimited because paths can contain spaces. `read a0 < cmdline` naturally
// yields argv[0]: shell vars can't hold the NUL that separates cmdline args.
func (c *Client) runningFrida(ctx context.Context, serial string) []fridaProc {
	const script = `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; case "$c" in *frida-server*) read a0 < "$p/cmdline" 2>/dev/null; echo "${p##*/}|$c|$(readlink "$p/exe" 2>/dev/null)|$a0";; esac; done`
	out, _, _ := c.ShellSU(ctx, serial, script)
	if strings.TrimSpace(out) == "" {
		out, _ = c.Shell(ctx, serial, script)
	}
	var procs []fridaProc
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		f := strings.Split(ln, "|")
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(f[0]))
		if err != nil {
			continue
		}
		p := fridaProc{pid: pid, comm: strings.TrimSpace(f[1])}
		if len(f) >= 3 {
			p.exe = strings.TrimSpace(f[2])
		}
		if len(f) >= 4 {
			p.cmd0 = strings.TrimSpace(f[3])
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

// FridaServerLogPath is where StartFrida redirects the server's own stdout and
// stderr. frida-server reports the failures that matter — an SELinux exec
// denial, a busy port, an agent that cannot read the device's ART layout — on
// those streams, and once it daemonizes there is nowhere else to read them from.
//
// The name deliberately avoids the substring "frida-server": the log lives in
// the same directory the server inventory globs, which matches on exactly that.
const FridaServerLogPath = "/data/local/tmp/adbq-frida.log"

// fridaStartTimeout bounds the launch command. The command itself returns in
// well under a second once the daemon's fds are detached; anything longer means
// the device is wedged, and a caller passing a background context (app.go does)
// would otherwise wait forever.
const fridaStartTimeout = 20 * time.Second

// StartFrida launches the given frida-server bound to iface:port. Requires
// root; if iface is "", binds 0.0.0.0.
//
// It uses frida-server's own -D/--daemonize rather than setsid/nohup/& — many
// stripped ROMs lack setsid/nohup, and -D makes frida fork a proper daemon that
// survives the shell exit on every device.
//
// The redirections are not cosmetic. A daemonized frida-server inherits the adb
// shell's stdin/stdout/stderr, and adbd keeps the connection open until every
// holder of those fds is gone — so `-D` alone makes `adb shell` (and this call)
// hang for the daemon's entire lifetime while the server nonetheless runs.
// Detaching all three fds is what lets the command return, and pointing the
// output at a file is what keeps the diagnostics that used to vanish with it.
func (c *Client) StartFrida(ctx context.Context, serial, serverPath, iface string, port int) (string, error) {
	if port <= 0 {
		port = 27042
	}
	if iface == "" {
		iface = "0.0.0.0"
	}
	q := shQuote(serverPath)
	log := shQuote(FridaServerLogPath)
	cmd := ": > " + log + " 2>/dev/null; " +
		"chmod 755 " + q + " && " + q + " -l " + iface + ":" + strconv.Itoa(port) +
		" -D </dev/null >>" + log + " 2>&1"

	sctx, cancel := context.WithTimeout(ctx, fridaStartTimeout)
	defer cancel()
	out, _, shellErr := c.ShellSU(sctx, serial, cmd)

	// Whether the daemon survived its fork is only visible in its log, and a
	// non-zero exit tells us nothing beyond "exit status 1" — so read the log
	// either way. Give the server a moment to write it first.
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
	}
	logOut, _ := c.FridaServerLog(ctx, serial)

	if msg := fridaStartFailure(logOut, serverPath); msg != "" {
		return logOut, errors.New(msg)
	}
	if shellErr != nil {
		// The log is the better explanation when there is one; the shell's own
		// stderr (e.g. a chmod failure, which is not redirected) is the fallback.
		if detail := firstLine(strings.TrimSpace(logOut)); detail != "" {
			return logOut, fmt.Errorf("frida-server did not start: %s", detail)
		}
		return out, shellErr
	}
	if strings.TrimSpace(logOut) != "" {
		out = logOut
	}
	return out, nil
}

// fridaStartFailure inspects the server log for a failure that prevented the
// server from coming up at all, and returns a user-facing explanation ("" when
// there is none). A daemonized server writes nothing on a clean start, so an
// empty log is the success case.
//
// Only launch-fatal causes belong here. The server also logs faults it survives
// — most notably an agent that cannot map this Android's ART, which leaves a
// running-but-useless server. Reporting those as a failed start would contradict
// the UI, which is about to see the server as active; they reach the user
// through the server-log panel and the session-level error mapping instead.
func fridaStartFailure(logOut, serverPath string) string {
	low := strings.ToLower(logOut)
	first := firstLine(strings.TrimSpace(logOut))
	switch {
	case strings.TrimSpace(low) == "":
		return ""
	// SELinux frequently blocks executing binaries from /data/local/tmp on
	// enforcing stock ROMs. Surface that distinctly so the UI stops blaming an
	// arch mismatch for what is really a policy denial. Match the SELinux exec
	// markers specifically — a bare "permission denied" might just be a chmod
	// failure, which this message would mislabel.
	case strings.Contains(low, "avc: denied"),
		strings.Contains(low, "denied") && strings.Contains(low, "execute"):
		return fmt.Sprintf("SELinux blocked executing frida-server from %s — push it to a Magisk-allowed path (e.g. /data/adb/…) or set the domain permissive: %s", serverPath, first)
	case strings.Contains(low, "address already in use"),
		strings.Contains(low, "address in use"):
		return "that port is already taken on the device — stop the running frida-server first, or pick another port: " + first
	case strings.Contains(low, "not executable"),
		strings.Contains(low, "exec format error"),
		strings.Contains(low, "no such file or directory"):
		return "frida-server did not start — the binary is missing or does not match the device architecture: " + first
	}
	return ""
}

// FridaServerLog returns the frida-server output captured by the last StartFrida
// (see FridaServerLogPath). Reads as root when available and falls back to the
// plain shell, so the log is still readable on a device where su is denied.
func (c *Client) FridaServerLog(ctx context.Context, serial string) (string, error) {
	cmd := "cat " + shQuote(FridaServerLogPath) + " 2>/dev/null"
	if out, _, err := c.ShellSU(ctx, serial, cmd); err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}
	return c.Shell(ctx, serial, cmd)
}

// StopFrida kills running frida-server processes. Uses a procfs scan + the kill
// builtin instead of pkill/killall, which minimal ROMs don't ship.
func (c *Client) StopFrida(ctx context.Context, serial string) (string, error) {
	const script = `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; case "$c" in *frida-server*) kill "${p##*/}" 2>/dev/null; kill -9 "${p##*/}" 2>/dev/null;; esac; done; echo stopped`
	out, _, err := c.ShellSU(ctx, serial, script)
	return out, err
}

// DetectRunningFridaVersion returns the version of the frida-server currently
// running on the device. It asks the live binary itself (`<exe> --version`),
// which is authoritative — unlike the on-disk filename, which a user can rename
// or which can lie about the real build. The host-side `frida` client must match
// this version, so it drives the venv pin. Falls back to the active server's
// filename-derived version only when the probe yields nothing.
func (c *Client) DetectRunningFridaVersion(ctx context.Context, serial string) (string, error) {
	procs := c.runningFrida(ctx, serial)
	for _, p := range procs {
		bin := p.exe
		if bin == "" {
			bin = p.cmd0
		}
		if bin == "" {
			continue
		}
		out, _, _ := c.ShellSU(ctx, serial, shQuote(bin)+" --version")
		if v := parseVersionToken(out); v != "" {
			return v, nil
		}
	}
	servers, _ := c.ListFridaServers(ctx, serial)
	for _, s := range servers {
		if s.Active && s.Version != "" {
			return s.Version, nil
		}
	}
	if len(procs) == 0 {
		return "", fmt.Errorf("no running frida-server detected on the device — start one first")
	}
	return "", fmt.Errorf("could not determine the running frida-server version")
}
