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

// procTableResolver picks the process-listing command this device answers to
// and remembers it.
//
// Without the resolver, ProcTable tried `ps -A -o` and fell back to a full
// procfs walk on failure — and forgot which had worked. On a ROM where the
// modern form is rejected, that meant running BOTH commands every four seconds
// for the life of the logcat feed, the fallback being a shell loop that opens
// two files per process. The resolver rules a strategy out permanently the
// first time it reports ErrUnsupported, which is exactly the bookkeeping that
// was missing.
//
// The fact is domain-prefixed so DomApps invalidation reaches it (cachedomain.go).
var procTableResolver = NewResolver[map[int]ProcOwner]("apps.proctable",
	procTableViaPS{},
	procTableViaProcfs{},
)

// procTableViaPS uses toybox `ps -o`, supported since Android 6.
type procTableViaPS struct{}

func (procTableViaPS) Name() string           { return "ps-A-o" }
func (procTableViaPS) Requires() Requirements { return Requirements{Bins: []string{"ps"}} }

func (procTableViaPS) Run(ctx context.Context, c *Client, serial string) (map[int]ProcOwner, error) {
	out, err := c.Shell(ctx, serial, procTableCmd)
	if err != nil {
		// Pre-toybox `ps` rejects -A/-o outright. That is a property of the
		// ROM, not of this moment, so report it as permanent and let the
		// resolver stop trying.
		if looksLikeRejectedFlag(err.Error() + " " + out) {
			return nil, ErrUnsupported
		}
		return nil, err
	}
	tbl := parseProcTable(out)
	if len(tbl) == 0 {
		// Exit status 0 with nothing usable is the other way an old `ps`
		// declines: it prints a usage banner and succeeds.
		return nil, ErrUnsupported
	}
	return tbl, nil
}

// procTableViaProcfs reads the same three fields straight out of procfs using
// only shell builtins, for ROMs whose `ps` cannot do it.
type procTableViaProcfs struct{}

func (procTableViaProcfs) Name() string           { return "procfs-walk" }
func (procTableViaProcfs) Requires() Requirements { return Requirements{} }

func (procTableViaProcfs) Run(ctx context.Context, c *Client, serial string) (map[int]ProcOwner, error) {
	out, err := c.Shell(ctx, serial, procTableFallbackCmd)
	if err != nil {
		return nil, err
	}
	return parseProcTable(out), nil
}

// looksLikeRejectedFlag reports whether a `ps` failure is the ROM refusing the
// flags rather than a transient problem.
func looksLikeRejectedFlag(msg string) bool {
	low := strings.ToLower(msg)
	for _, m := range []string{"unknown option", "invalid option", "bad -", "usage:", "unrecognized"} {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// ProcTable snapshots the device's process list as pid → owner. It is used to
// tell app log lines from OS ones without a second round-trip per line.
//
// Errors are not fatal to the caller: a nil map simply means "unclassified",
// and the logcat filter treats unknown pids as visible rather than hiding
// lines it cannot attribute.
func (c *Client) ProcTable(ctx context.Context, serial string) (map[int]ProcOwner, error) {
	// The freshness key is empty: this is a live read, re-run every time. The
	// resolver is here for its strategy selection, not for caching a snapshot
	// of something that changes constantly.
	return procTableResolver.Resolve(ctx, c, serial, "")
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
