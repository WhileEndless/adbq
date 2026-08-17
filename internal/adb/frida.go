package adb

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	Port    int    `json:"port"` // the port it is actually listening on (only when Active)
	// Ambiguous marks a binary that a running server *might* be, but which we
	// could not pin down — the process's exe link and command line were both
	// unreadable (no root) and its truncated /proc comm matches several of the
	// installed binaries. Reporting every candidate as Active instead made the UI
	// claim two servers were running at once off a single process.
	Ambiguous bool `json:"ambiguous"`
	// Runnable is false for something the glob caught that cannot be launched —
	// a compressed archive left next to the binaries, or a file with no execute
	// bit. These used to be offered as startable servers.
	Runnable bool `json:"runnable"`
}

// FridaProcess is a process the frida server is observing (we cannot list
// attachments without talking to frida itself; this is a best-effort fallback
// based on `ps -A`).
type FridaProcess struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// fridaServerDir is where adbq installs frida-server binaries and where the
// inventory looks for them.
const fridaServerDir = "/data/local/tmp"

// ListFridaServers reports the frida-server binaries installed on the device and
// which one (if any) is running.
//
// A failure to read the directory is returned rather than swallowed. The old
// behaviour reported an empty list either way, so a device that briefly refused
// the listing was indistinguishable from one with nothing installed — the UI
// said "no frida-server" and offered to push one.
func (c *Client) ListFridaServers(ctx context.Context, serial string) ([]FridaServer, error) {
	// Glob inside the dir (so names are basenames) instead of `ls | grep` —
	// `grep` is absent on stripped ROMs. A non-matching glob just yields empty
	// output. parseLsLine tolerates both ISO and BSD/toolbox date columns.
	out, err := c.Shell(ctx, serial, fridaListRemote())
	if err != nil && strings.TrimSpace(out) == "" {
		// An empty glob also exits non-zero, so tell "nothing matched" apart from
		// "we could not ask" by checking the directory itself is reachable.
		if probe, perr := c.Shell(ctx, serial, "ls -d "+shQuote(fridaServerDir)+" 2>/dev/null"); perr != nil || strings.TrimSpace(probe) == "" {
			return nil, fmt.Errorf("could not list %s on %s: %w", fridaServerDir, serial, err)
		}
		return []FridaServer{}, nil
	}

	res := []FridaServer{}
	for _, ln := range strings.Split(out, "\n") {
		entry, ok := parseLsLine(strings.TrimRight(ln, "\r"))
		if !ok {
			continue
		}
		if !strings.Contains(entry.Name, "frida-server") || entry.Type == "dir" {
			continue
		}
		res = append(res, FridaServer{
			Name:     entry.Name,
			Path:     fridaServerDir + "/" + entry.Name,
			Size:     entry.Size,
			Perms:    entry.Perms,
			Version:  extractFridaVersion(entry.Name),
			Arch:     extractFridaArch(entry.Name),
			Runnable: isRunnableServer(entry.Name, entry.Perms),
		})
	}
	markRunningServers(res, c.runningFrida(ctx, serial))
	return res, nil
}

// fridaArchiveExts are the download artifacts that live alongside the binaries
// and match the same glob. Offering one as a startable server only ever produced
// a confusing failure.
var fridaArchiveExts = []string{".xz", ".gz", ".bz2", ".zip", ".tar", ".zst"}

// isRunnableServer reports whether an inventory entry could actually be executed.
func isRunnableServer(name, perms string) bool {
	lower := strings.ToLower(name)
	for _, ext := range fridaArchiveExts {
		if strings.HasSuffix(lower, ext) {
			return false
		}
	}
	// Any execute bit will do: adbq chmods before launching, and the owner may
	// differ from the shell user.
	return strings.Contains(perms, "x")
}

// markRunningServers assigns each running frida process to at most one installed
// binary and copies its PID and listening port onto that entry.
//
// Identification degrades in steps because how much /proc reveals depends on
// privilege: the exe link is authoritative but root-only, the command line
// survives without root, and the comm name is always readable but truncated to
// 15 characters — far too short to tell frida-server-17.5.1 from -17.5.2.
func markRunningServers(servers []FridaServer, procs []fridaProc) {
	for _, p := range procs {
		if i := indexOfServerFor(servers, p); i >= 0 {
			servers[i].Active = true
			servers[i].PID = p.pid
			servers[i].Port = p.port
			continue
		}
		// Unidentifiable: flag every candidate rather than crowning one at random.
		for i := range servers {
			if servers[i].comm() != "" && strings.HasPrefix(servers[i].Name, p.comm) {
				servers[i].Ambiguous = true
			}
		}
	}
}

// comm returns the name as the kernel would truncate it in /proc/<pid>/comm.
func (s FridaServer) comm() string {
	if len(s.Name) > 15 {
		return s.Name[:15]
	}
	return s.Name
}

// indexOfServerFor returns the installed binary this process is, or -1 when it
// cannot be pinned to exactly one.
func indexOfServerFor(servers []FridaServer, p fridaProc) int {
	if p.exe != "" {
		for i := range servers {
			if servers[i].Path == p.exe {
				return i
			}
		}
		return -1 // running from somewhere we don't list; not one of these
	}
	if p.cmd0 != "" {
		// The shell drops the NULs separating argv, so the command line arrives as
		// one run-together blob: "<path>-l0.0.0.0:27042-D". argv[0] is still its
		// prefix, and taking the LONGEST matching path disambiguates an install
		// whose name extends another's (…-arm64 vs …-arm64-copy).
		best, bestLen := -1, 0
		for i := range servers {
			if len(servers[i].Path) > bestLen && strings.HasPrefix(p.cmd0, servers[i].Path) {
				best, bestLen = i, len(servers[i].Path)
			}
		}
		if best >= 0 {
			return best
		}
	}
	// Only the truncated comm is left: accept it when exactly one binary matches.
	match := -1
	for i := range servers {
		if p.comm != "" && strings.HasPrefix(servers[i].Name, p.comm) {
			if match >= 0 {
				return -1 // several candidates share the truncated name
			}
			match = i
		}
	}
	return match
}

// fridaProc is a running frida-server discovered via procfs.
type fridaProc struct {
	pid  int
	comm string // /proc/<pid>/comm (truncated to 15 chars by the kernel)
	exe  string // readlink /proc/<pid>/exe — full binary path, when readable
	cmd0 string // /proc/<pid>/cmdline, NULs collapsed — argv[0] is its prefix
	port int    // the -l port from the command line, or the frida default
}

// reFridaListenPort pulls the port out of frida-server's -l argument. It runs
// against a command line whose NUL separators the shell dropped, so the address
// is glued to its neighbours ("…-arm64-l0.0.0.0:27042-D") and the port is the
// digits after the last colon of the -l value.
var reFridaListenPort = regexp.MustCompile(`-l\s*[^\s]*?:(\d{1,5})`)

// portFromCmdline returns the port a frida-server command line asks for,
// defaulting to frida's own when it names none.
func portFromCmdline(cmdline string) int {
	if m := reFridaListenPort.FindStringSubmatch(cmdline); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return FridaDefaultPort
}

// runningFrida scans procfs for live frida-server processes. It avoids ps/grep/
// tr/pkill — minimal ROMs ship none of those — using only shell builtins plus
// readlink. Reads run as root since frida-server runs as root. Fields are
// '|'-delimited because paths can contain spaces.
//
// `read a0 < cmdline` does not yield argv[0] alone: the shell drops the NULs
// that separate the arguments instead of stopping at the first one, so what
// comes back is every argument run together. That is still useful — argv[0] is
// its prefix, and the -l argument is in there — but it is not a path, and
// comparing it to one as if it were is why a running server used to be matched
// to the wrong binary.
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
		p.port = portFromCmdline(p.cmd0)
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

// fridaServerLogPath is where StartFrida redirects a server's own stdout and
// stderr. frida-server reports the failures that matter — an SELinux exec
// denial, a busy port, an agent that cannot read the device's ART layout — on
// those streams, and once it daemonizes there is nowhere else to read them from.
//
// The log is keyed by port because a device can run several servers at once (on
// different ports — two servers cannot share one), and a single shared file
// would have each launch truncate the previous server's diagnostics and then
// interleave their output. The port is the one thing that is necessarily
// distinct between them.
//
// The name deliberately avoids the substring "frida-server": the log lives in
// the same directory the server inventory globs, which matches on exactly that.
func fridaServerLogPath(port int) string {
	return "/data/local/tmp/adbq-frida-" + strconv.Itoa(fridaPortOrDefault(port)) + ".log"
}

// FridaDefaultPort is the port frida's own client tooling connects to.
const FridaDefaultPort = 27042

func fridaPortOrDefault(port int) int {
	if port <= 0 {
		return FridaDefaultPort
	}
	return port
}

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
	port = fridaPortOrDefault(port)
	if iface == "" {
		iface = "0.0.0.0"
	}
	cmd := fridaStartRemote(serverPath, iface, port)

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
	logOut, _ := c.FridaServerLog(ctx, serial, port)

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

// fridaStartRemote is the command that launches frida-server. The redirections
// are load-bearing: a daemonized server inherits the adb shell's fds and adbd
// holds the connection open until every holder is gone, so detaching all three
// is what lets the call return, and the log file is what keeps the diagnostics
// that used to vanish with them.
func fridaStartRemote(serverPath, iface string, port int) string {
	port = fridaPortOrDefault(port)
	if iface == "" {
		iface = "0.0.0.0"
	}
	q := shQuote(serverPath)
	log := shQuote(fridaServerLogPath(port))
	return ": > " + log + " 2>/dev/null; " +
		"chmod 755 " + q + " && " + q + " -l " + iface + ":" + strconv.Itoa(port) +
		" -D </dev/null >>" + log + " 2>&1"
}

// fridaLogRemote reads back what a start wrote.
func fridaLogRemote(port int) string {
	return "cat " + shQuote(fridaServerLogPath(port)) + " 2>/dev/null"
}

// fridaStopScript kills running servers with a procfs scan and the kill builtin,
// because minimal ROMs ship neither pkill nor killall.
const fridaStopScript = `for p in /proc/[0-9]*; do read c < "$p/comm" 2>/dev/null || continue; case "$c" in *frida-server*) kill "${p##*/}" 2>/dev/null; kill -9 "${p##*/}" 2>/dev/null;; esac; done; echo stopped`

// fridaListRemote globs inside the server dir (so names come back as basenames)
// rather than piping ls through grep, which stripped ROMs do not ship.
func fridaListRemote() string {
	return "cd " + shQuote(fridaServerDir) + " 2>/dev/null && ls -l *frida-server* 2>/dev/null"
}

// fridaChmodRemote makes a pushed server executable.
func fridaChmodRemote(remote string) string { return "chmod 755 " + shQuote(remote) }

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

// FridaServerLog returns the output captured by the last StartFrida for the
// server on the given port (see fridaServerLogPath). Reads as root when
// available and falls back to the plain shell, so the log is still readable on
// a device where su is denied.
func (c *Client) FridaServerLog(ctx context.Context, serial string, port int) (string, error) {
	cmd := fridaLogRemote(port)
	if out, _, err := c.ShellSU(ctx, serial, cmd); err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}
	return c.Shell(ctx, serial, cmd)
}

// StopFrida kills running frida-server processes. Uses a procfs scan + the kill
// builtin instead of pkill/killall, which minimal ROMs don't ship.
func (c *Client) StopFrida(ctx context.Context, serial string) (string, error) {
	out, _, err := c.ShellSU(ctx, serial, fridaStopScript)
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

// CompareFridaVersions orders two dotted frida version strings, returning 1, 0
// or -1. Exported so app-level server selection can prefer the newest build
// without duplicating the parsing.
func CompareFridaVersions(a, b string) int { return compareVersions(a, b) }
