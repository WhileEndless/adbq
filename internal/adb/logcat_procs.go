package adb

import (
	"context"
	"strconv"
	"strings"
)

// ProcOwner identifies the process a log line came from.
type ProcOwner struct {
	Name string // process name (package name for app processes)
	UID  int    // Linux uid; >= firstAppUID means an installed app
}

// IsApp reports whether the process belongs to an installed application rather
// than the OS. Android assigns every installed package a uid at or above
// firstAppUID, so this is the same test the platform itself uses — far more
// reliable than matching process names against a hand-written deny list.
//
// The uid is per-user: uid = userId*perUserRange + appId. Testing the appId
// (rather than the raw uid) is what keeps a secondary user's or work profile's
// *system* processes — uid 1001000 is user 10's AID_SYSTEM, comfortably above
// firstAppUID — from being mislabelled as apps.
func (p ProcOwner) IsApp() bool { return p.UID >= 0 && p.UID%perUserRange >= firstAppUID }

// firstAppUID is Android's AID_APP: the first appId handed out to an installed
// package. Everything below it (root, system, radio, media, shell, …) is OS
// infrastructure.
const firstAppUID = 10000

// perUserRange is Android's AID_USER_OFFSET — the uid span allotted to each
// Android user / work profile.
const perUserRange = 100000

// procTableCmd asks toybox for one row per process. `-o` with explicit columns
// is supported by every toybox `ps` shipped since Android 6; older ROMs are
// covered by procTableFallbackCmd.
const procTableCmd = "ps -A -o PID,UID,NAME"

// procTableFallbackCmd reads the same three fields straight out of procfs using
// nothing but shell builtins (for/while/read/echo), for stripped or pre-toybox
// ROMs whose `ps` rejects `-A`/`-o`. One line per pid: "<pid> <uid> <name>".
//
// The name comes from /proc/<pid>/cmdline, not from status's `Name:`: the
// latter is `comm`, capped at 15 characters, which truncates every real package
// name ("com.example.app" is already at the limit) and would stop the package
// filter from ever matching. cmdline is NUL-terminated with no newline, so a
// plain `read` returns non-zero at EOF while still having filled the variable —
// hence the `|| true`-style tolerance of that read's exit status.
const procTableFallbackCmd = `for p in /proc/[0-9]*; do u=; n=; while read k v _; do ` +
	`case "$k" in Name:) n=$v;; Uid:) u=$v; break;; esac; done < "$p/status" 2>/dev/null; ` +
	`if [ -n "$u" ]; then c=; read c < "$p/cmdline" 2>/dev/null; ` +
	`if [ -n "$c" ]; then n=$c; fi; echo "${p##*/} $u $n"; fi; done`

// ProcTable snapshots the device's process list as pid → owner. It is used to
// tell app log lines from OS ones without a second round-trip per line.
//
// Errors are not fatal to the caller: a nil map simply means "unclassified",
// and the logcat filter treats unknown pids as visible rather than hiding
// lines it cannot attribute.
func (c *Client) ProcTable(ctx context.Context, serial string) (map[int]ProcOwner, error) {
	out, err := c.Shell(ctx, serial, procTableCmd)
	if err == nil {
		if tbl := parseProcTable(out); len(tbl) > 0 {
			return tbl, nil
		}
	}
	out, ferr := c.Shell(ctx, serial, procTableFallbackCmd)
	if ferr != nil {
		if err != nil {
			return nil, err
		}
		return nil, ferr
	}
	return parseProcTable(out), nil
}

// parseProcTable parses "<pid> <uid> <name>" rows, skipping the `ps` header and
// any line whose first two columns are not numeric. Kernel threads (bracketed
// names like "[kthreadd]") are kept — they own log lines too.
func parseProcTable(out string) map[int]ProcOwner {
	tbl := make(map[int]ProcOwner)
	for _, ln := range strings.Split(out, "\n") {
		fs := strings.Fields(ln)
		if len(fs) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fs[0])
		if err != nil {
			continue
		}
		uid, err := strconv.Atoi(fs[1])
		if err != nil {
			continue
		}
		name := ""
		if len(fs) > 2 {
			// Rejoin so process names containing spaces survive intact.
			name = strings.Join(fs[2:], " ")
		}
		tbl[pid] = ProcOwner{Name: name, UID: uid}
	}
	return tbl
}
