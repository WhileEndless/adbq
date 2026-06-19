package adb

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// App is a brief installed-package record.
type App struct {
	Pkg     string `json:"pkg"`
	Path    string `json:"path"`
	System  bool   `json:"system"`
	Name    string `json:"name"` // best-effort label (may equal pkg)
	Version string `json:"v"`
	UID     string `json:"uid"`
}

// GrantedPerm is one runtime/install permission with its granted state.
type GrantedPerm struct {
	Name    string `json:"name"`
	Granted bool   `json:"granted"`
}

// AppDetail expands an App with extra fields gathered from `dumpsys package`.
type AppDetail struct {
	App
	VersionCode       string   `json:"versionCode"`
	FirstInstall      string   `json:"firstInstall"`
	LastUpdate        string   `json:"lastUpdate"`
	TimeStamp         string   `json:"timeStamp"`
	TargetSdk         string   `json:"targetSdk"`
	MinSdk            string   `json:"minSdk"`
	CompileSdk        string   `json:"compileSdk"`
	DataDir           string   `json:"dataDir"`
	NativeLibraryDir  string   `json:"nativeLibraryDir"`
	Installer         string   `json:"installer"`
	InstallLocation   string   `json:"installLocation"`
	PrimaryAbi        string   `json:"primaryAbi"`
	SecondaryAbi      string   `json:"secondaryAbi"`
	Splits            []string `json:"splits"`
	Flags             []string `json:"flags"`
	PrivateFlags      []string `json:"privateFlags"`
	SupportsScreens   []string `json:"supportsScreens"`
	Signature         string   `json:"signature"`
	ApkSigningVersion string   `json:"apkSigningVersion"`
	// Per-user state (we currently surface User 0). Empty when we couldn't parse.
	Enabled        string        `json:"enabled"`     // "enabled"/"disabled"/raw value
	Installed      string        `json:"installed"`   // "true"/"false"
	Stopped        string        `json:"stopped"`     // "true"/"false"
	NotLaunched    string        `json:"notLaunched"` // "true"/"false"
	Suspended      string        `json:"suspended"`   // "true"/"false"
	Instant        string        `json:"instant"`     // "true"/"false"
	GIDs           []string      `json:"gids"`        // group IDs (first few)
	RequestedPerms []string      `json:"requestedPerms"`
	GrantedPerms   []GrantedPerm `json:"grantedPerms"`
}

// ListApps returns installed packages. If onlyUser true, restricts to third-party.
func (c *Client) ListApps(ctx context.Context, serial string, onlyUser bool) ([]App, error) {
	flag := "-f"
	args := []string{"pm", "list", "packages", flag}
	if onlyUser {
		args = append(args, "-3")
	}
	out, err := c.Shell(ctx, serial, strings.Join(args, " "))
	if err != nil {
		return nil, err
	}
	var apps []App
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "package:") {
			continue
		}
		// package:/path/base.apk=com.foo
		rest := strings.TrimPrefix(ln, "package:")
		eq := strings.LastIndex(rest, "=")
		if eq < 0 {
			continue
		}
		path := rest[:eq]
		pkg := rest[eq+1:]
		apps = append(apps, App{
			Pkg:    pkg,
			Path:   path,
			System: isSystemPath(path),
			Name:   prettyName(pkg),
		})
	}
	return apps, nil
}

// isSystemPath returns true for paths backed by read-only system partitions.
func isSystemPath(path string) bool {
	for _, p := range []string{"/system/", "/product/", "/vendor/", "/apex/", "/system_ext/", "/odm/"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// parseBracketList extracts items from a "[ a, b, c ]" or "[a b c]" string.
// Returns nil for an empty list. Handles both comma-separated and
// whitespace-separated forms that dumpsys uses across Android versions.
func parseBracketList(s string) []string {
	open := strings.Index(s, "[")
	close := strings.LastIndex(s, "]")
	if open < 0 || close <= open {
		return nil
	}
	inner := strings.TrimSpace(s[open+1 : close])
	if inner == "" {
		return nil
	}
	sep := func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }
	parts := strings.FieldsFunc(inner, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func prettyName(pkg string) string {
	// last segment, title-cased
	last := pkg
	if i := strings.LastIndex(pkg, "."); i >= 0 {
		last = pkg[i+1:]
	}
	if last == "" {
		return pkg
	}
	return strings.ToUpper(last[:1]) + last[1:]
}

// DescribeApp pulls dumpsys package details for one pkg.
func (c *Client) DescribeApp(ctx context.Context, serial, pkg string) (*AppDetail, error) {
	out, err := c.Shell(ctx, serial, "dumpsys package "+pkg)
	if err != nil {
		return nil, err
	}
	return c.describeAppFromText(out, pkg)
}

// kvFromLine returns the value for one space-separated key=value pair on the
// line, or "" when the key isn't present. dumpsys packs multiple pairs onto
// the same line for some sections (e.g. "versionCode=N minSdk=N targetSdk=N"),
// so a HasPrefix check against the line start misses everything but the first.
func kvFromLine(line, key string) string {
	for {
		i := strings.Index(line, key+"=")
		if i < 0 {
			return ""
		}
		// Must be at the start of the line or preceded by whitespace —
		// otherwise we'd match "supportsSmallScreens=" when looking for
		// "smallScreens=".
		if i > 0 {
			prev := line[i-1]
			if prev != ' ' && prev != '\t' {
				line = line[i+1:]
				continue
			}
		}
		v := line[i+len(key)+1:]
		// Value ends at the next whitespace, unless it starts with '[' in
		// which case it spans through the matching ']'.
		if strings.HasPrefix(v, "[") {
			if end := strings.Index(v, "]"); end >= 0 {
				return v[:end+1]
			}
			return v
		}
		if sp := strings.IndexAny(v, " \t"); sp >= 0 {
			return v[:sp]
		}
		return v
	}
}

// describeAppFromText parses the output of `dumpsys package <pkg>`. Kept as
// its own function (no I/O) so it's directly testable against fixtures.
//
// dumpsys's per-package output is loosely structured: every line is a free-
// form mix of "key=value" pairs (single or many per line), bracketed lists,
// and indented sub-sections (requested/install/runtime permissions, per-user
// state). We extract pairs with kvFromLine so combined lines like
// "versionCode=N minSdk=N targetSdk=N" all get picked up.
func (c *Client) describeAppFromText(out, pkg string) (*AppDetail, error) {
	d := &AppDetail{App: App{Pkg: pkg, Name: prettyName(pkg)}}
	collectingReq := false
	collectingGranted := false
	for _, raw := range strings.Split(out, "\n") {
		t := strings.TrimSpace(raw)
		if collectingReq && (raw == "" || !strings.HasPrefix(raw, " ")) {
			collectingReq = false
		}
		if collectingGranted && (raw == "" || !strings.HasPrefix(raw, " ")) {
			collectingGranted = false
		}

		// Pick up scalar fields from anywhere on the line.
		if v := kvFromLine(t, "versionName"); v != "" && d.Version == "" {
			d.Version = v
		}
		if v := kvFromLine(t, "versionCode"); v != "" && d.VersionCode == "" {
			d.VersionCode = v
		}
		if v := kvFromLine(t, "minSdk"); v != "" && d.MinSdk == "" {
			d.MinSdk = v
		}
		if v := kvFromLine(t, "targetSdk"); v != "" && d.TargetSdk == "" {
			d.TargetSdk = v
		}
		if v := kvFromLine(t, "compileSdk"); v != "" && d.CompileSdk == "" {
			d.CompileSdk = v
		} else if v := kvFromLine(t, "compileSdkVersion"); v != "" && d.CompileSdk == "" {
			d.CompileSdk = v
		}
		if v := kvFromLine(t, "userId"); v != "" && d.UID == "" {
			d.UID = v
		}
		if v := kvFromLine(t, "codePath"); v != "" && d.Path == "" {
			d.Path = v
		}
		if v := kvFromLine(t, "dataDir"); v != "" && d.DataDir == "" {
			d.DataDir = v
		}
		if v := kvFromLine(t, "legacyNativeLibraryDir"); v != "" && d.NativeLibraryDir == "" {
			d.NativeLibraryDir = v
		} else if v := kvFromLine(t, "nativeLibraryDir"); v != "" && d.NativeLibraryDir == "" {
			d.NativeLibraryDir = v
		}
		if v := kvFromLine(t, "primaryCpuAbi"); v != "" && v != "null" && d.PrimaryAbi == "" {
			d.PrimaryAbi = v
		}
		if v := kvFromLine(t, "secondaryCpuAbi"); v != "" && v != "null" && d.SecondaryAbi == "" {
			d.SecondaryAbi = v
		}
		if v := kvFromLine(t, "installerPackageName"); v != "" && v != "null" && d.Installer == "" {
			d.Installer = v
		}
		if v := kvFromLine(t, "installLocation"); v != "" && d.InstallLocation == "" {
			d.InstallLocation = v
		}
		if v := kvFromLine(t, "apkSigningVersion"); v != "" && d.ApkSigningVersion == "" {
			d.ApkSigningVersion = v
		}
		if v := kvFromLine(t, "firstInstallTime"); v != "" && d.FirstInstall == "" {
			// firstInstallTime is "YYYY-MM-DD HH:MM:SS" with a space; kvFromLine
			// stops at the first space so we only get the date. Fall back to
			// the prefix split for these specific multi-word values.
			d.FirstInstall = strings.TrimSpace(strings.TrimPrefix(t, "firstInstallTime="))
		}
		if v := kvFromLine(t, "lastUpdateTime"); v != "" && d.LastUpdate == "" {
			d.LastUpdate = strings.TrimSpace(strings.TrimPrefix(t, "lastUpdateTime="))
		}
		if v := kvFromLine(t, "timeStamp"); v != "" && d.TimeStamp == "" {
			d.TimeStamp = strings.TrimSpace(strings.TrimPrefix(t, "timeStamp="))
		}

		// User-0 line: "User 0: ceDataInode=… installed=… hidden=… …"
		if strings.HasPrefix(t, "User 0:") {
			if v := kvFromLine(t, "installed"); v != "" {
				d.Installed = v
			}
			if v := kvFromLine(t, "stopped"); v != "" {
				d.Stopped = v
			}
			if v := kvFromLine(t, "notLaunched"); v != "" {
				d.NotLaunched = v
			}
			if v := kvFromLine(t, "suspended"); v != "" {
				d.Suspended = v
			}
			if v := kvFromLine(t, "instant"); v != "" {
				d.Instant = v
			}
			if v := kvFromLine(t, "enabled"); v != "" {
				d.Enabled = enabledStateLabel(v)
			}
		}
		if v := kvFromLine(t, "gids"); v != "" && len(d.GIDs) == 0 {
			d.GIDs = parseBracketList(v)
		}

		// Bracketed lists.
		if v := kvFromLine(t, "splits"); v != "" && len(d.Splits) == 0 {
			d.Splits = parseBracketList(v)
		}
		if v := kvFromLine(t, "supportsScreens"); v != "" && len(d.SupportsScreens) == 0 {
			d.SupportsScreens = parseBracketList(v)
		}
		// flags vs pkgFlags: dumpsys emits both, identical content. Prefer
		// whichever lands first.
		if len(d.Flags) == 0 {
			if v := kvFromLine(t, "pkgFlags"); v != "" {
				d.Flags = parseBracketList(v)
			} else if v := kvFromLine(t, "flags"); v != "" && strings.HasPrefix(v, "[") {
				d.Flags = parseBracketList(v)
			}
		}
		if len(d.PrivateFlags) == 0 {
			if v := kvFromLine(t, "privatePkgFlags"); v != "" {
				d.PrivateFlags = parseBracketList(v)
			} else if v := kvFromLine(t, "privateFlags"); v != "" && strings.HasPrefix(v, "[") {
				d.PrivateFlags = parseBracketList(v)
			}
		}

		// Signature lines come in a couple of shapes across Android versions.
		if d.Signature == "" {
			if strings.HasPrefix(t, "Signatures: PackageSignatures") {
				d.Signature = strings.TrimSpace(strings.TrimPrefix(t, "Signatures:"))
			} else if strings.HasPrefix(t, "signatures=PackageSignatures") {
				d.Signature = strings.TrimSpace(strings.TrimPrefix(t, "signatures="))
			}
		}

		// Permission sections.
		switch {
		case strings.HasPrefix(t, "requested permissions:"):
			collectingReq = true
			continue
		case strings.HasPrefix(t, "install permissions:") || strings.HasPrefix(t, "runtime permissions:"):
			collectingReq = false
			collectingGranted = true
			continue
		}
		if collectingReq && t != "" && strings.Contains(t, ".") && !strings.Contains(t, "=") {
			d.RequestedPerms = append(d.RequestedPerms, t)
		}
		if collectingGranted && strings.Contains(t, ": granted=") {
			idx := strings.Index(t, ":")
			name := strings.TrimSpace(t[:idx])
			granted := strings.Contains(t, "granted=true")
			d.GrantedPerms = append(d.GrantedPerms, GrantedPerm{Name: name, Granted: granted})
		}
	}
	d.System = strings.HasPrefix(d.Path, "/system/") || strings.HasPrefix(d.Path, "/product/") || strings.HasPrefix(d.Path, "/vendor/") || strings.HasPrefix(d.Path, "/apex/")
	return d, nil
}

// enabledStateLabel maps Android's COMPONENT_ENABLED_STATE_* integer to a label.
func enabledStateLabel(v string) string {
	switch v {
	case "0":
		return "default"
	case "1":
		return "enabled"
	case "2":
		return "disabled"
	case "3":
		return "disabled-user"
	case "4":
		return "disabled-until-used"
	}
	// Sometimes the value is already textual (e.g. "true"/"false"); pass through.
	return v
}

// ListPackageUIDs returns a uid → package map (best effort) for translating
// /proc/net/* UID columns into human-readable owners.
func (c *Client) ListPackageUIDs(ctx context.Context, serial string) (map[int]string, error) {
	out, err := c.Shell(ctx, serial, "pm list packages -U")
	if err != nil {
		return nil, err
	}
	m := map[int]string{}
	for _, ln := range strings.Split(out, "\n") {
		// "package:com.example uid:10042"
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "package:") {
			continue
		}
		rest := strings.TrimPrefix(ln, "package:")
		sp := strings.Index(rest, " ")
		if sp < 0 {
			continue
		}
		pkg := rest[:sp]
		uidIdx := strings.Index(rest, "uid:")
		if uidIdx < 0 {
			continue
		}
		uidStr := strings.TrimSpace(rest[uidIdx+4:])
		var uid int
		for _, r := range uidStr {
			if r < '0' || r > '9' {
				break
			}
			uid = uid*10 + int(r-'0')
		}
		if uid > 0 {
			m[uid] = pkg
		}
	}
	return m, nil
}

// PathsOfApp returns every APK path for the package — base plus any config /
// density / language split APKs (App Bundle apps). Order is as `pm path`
// reports it, base first in practice.
func (c *Client) PathsOfApp(ctx context.Context, serial, pkg string) ([]string, error) {
	out, err := c.Shell(ctx, serial, "pm path "+pkg)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, ln := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(ln), "package:"); ok && p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// PathOfApp returns the first (base) APK path on device.
func (c *Client) PathOfApp(ctx context.Context, serial, pkg string) (string, error) {
	paths, err := c.PathsOfApp(ctx, serial, pkg)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// pmResultErr maps adb install/uninstall output to an error. adb prints
// "Success" or "Failure [REASON]" and frequently exits 0 even on failure, so a
// non-error return from Run can still be a failed operation.
func pmResultErr(out string, err error) error {
	for _, ln := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "Failure") {
			return fmt.Errorf("%s", t)
		}
	}
	return err
}

// InstallAPK runs `adb install -r <localPath>`.
func (c *Client) InstallAPK(ctx context.Context, serial, localPath string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "install", "-r", localPath)
	if err != nil {
		return "", err
	}
	out, err := Run(cmd)
	return out, pmResultErr(out, err)
}

// UninstallApp runs `adb uninstall <pkg>`.
func (c *Client) UninstallApp(ctx context.Context, serial, pkg string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "uninstall", pkg)
	if err != nil {
		return "", err
	}
	out, err := Run(cmd)
	return out, pmResultErr(out, err)
}

// ClearApp wipes user data via `pm clear`.
func (c *Client) ClearApp(ctx context.Context, serial, pkg string) (string, error) {
	return c.Shell(ctx, serial, "pm clear "+pkg)
}

// ForceStopApp via `am force-stop`.
func (c *Client) ForceStopApp(ctx context.Context, serial, pkg string) (string, error) {
	return c.Shell(ctx, serial, "am force-stop "+pkg)
}

// AppRunning is the lightweight "is this app alive right now" snapshot the
// Apps detail panel polls. PID is the main package process when there is one
// (matches `pidof <pkg>`); 0 when only sub-processes are present.
type AppRunning struct {
	Running bool `json:"running"`
	PID     int  `json:"pid"`
}

// IsAppRunning returns the live state for one package. Uses `pidof` (works
// from Android 6+) with a procfs scan fallback for stripped ROMs that omit
// pidof and the legacy `ps -A`/`-o` flags.
// Never returns an error for "not running" — that's expressed via Running.
func (c *Client) IsAppRunning(ctx context.Context, serial, pkg string) (*AppRunning, error) {
	if out, err := c.Shell(ctx, serial, "pidof "+pkg+" 2>/dev/null"); err == nil {
		if f := strings.Fields(strings.TrimSpace(out)); len(f) > 0 {
			if pid, err := strconv.Atoi(f[0]); err == nil {
				return &AppRunning{Running: true, PID: pid}, nil
			}
		}
	}
	// Procfs fallback: scan /proc/<pid>/cmdline. The main package process has a
	// cmdline of exactly <pkg>; sub-processes are <pkg>:tag. We prefer the main
	// process PID and fall back to the lowest sub-process PID.
	main, subs := c.pidsForPackage(ctx, serial, pkg)
	if main > 0 {
		return &AppRunning{Running: true, PID: main}, nil
	}
	if len(subs) > 0 {
		return &AppRunning{Running: true, PID: subs[0]}, nil
	}
	return &AppRunning{Running: false}, nil
}

// safePackageName allows only Android package-name characters so the value can
// be interpolated into a shell command without quoting hazards. Returns "" if
// the input contains anything else.
func safePackageName(pkg string) string {
	if pkg == "" {
		return ""
	}
	for i := 0; i < len(pkg); i++ {
		ch := pkg[i]
		ok := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '_'
		if !ok {
			return ""
		}
	}
	return pkg
}

// pidsForPackage scans /proc/<pid>/cmdline on the device and returns the PID of
// the main package process (cmdline == pkg, or 0 if none) and a sorted list of
// sub-process PIDs (cmdline == pkg:tag). Pure shell builtins + procfs, so it
// works on stripped ROMs lacking pidof / ps -A / pgrep.
func (c *Client) pidsForPackage(ctx context.Context, serial, pkg string) (main int, subs []int) {
	safe := safePackageName(pkg)
	if safe == "" {
		return 0, nil
	}
	// cmdline args are NUL-separated; `cat` yields "<pkg>\0[args...]". The case
	// globs below match the process name regardless of trailing args.
	snippet := `pkg=` + safe + `; for p in /proc/[0-9]*; do ` +
		`c=$(cat "$p/cmdline" 2>/dev/null) || continue; ` +
		`case "$c" in "$pkg") echo "M ${p##*/}";; "$pkg:"*) echo "S ${p##*/}";; esac; done`
	out, _ := c.Shell(ctx, serial, snippet)
	for _, ln := range strings.Split(out, "\n") {
		fs := strings.Fields(strings.TrimSpace(ln))
		if len(fs) != 2 {
			continue
		}
		pid, err := strconv.Atoi(fs[1])
		if err != nil {
			continue
		}
		if fs[0] == "M" {
			if main == 0 || pid < main {
				main = pid
			}
		} else {
			subs = append(subs, pid)
		}
	}
	sort.Ints(subs)
	return main, subs
}

// LaunchApp uses monkey to launch the default activity.
func (c *Client) LaunchApp(ctx context.Context, serial, pkg string) (string, error) {
	return c.Shell(ctx, serial, "monkey -p "+pkg+" -c android.intent.category.LAUNCHER 1")
}

// PullAPK copies the base APK to localPath. For split-APK (App Bundle) apps it
// also pulls the split APKs into the same directory, so the export is complete
// and reinstallable (a base-only export fails later with INSTALL_FAILED_MISSING_SPLIT).
func (c *Client) PullAPK(ctx context.Context, serial, pkg, localPath string) (string, error) {
	paths, err := c.PathsOfApp(ctx, serial, pkg)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no APK path found for %s", pkg)
	}
	if out, err := c.pullOne(ctx, serial, paths[0], localPath); err != nil {
		return out, err
	}
	if len(paths) == 1 {
		return localPath, nil
	}
	// Pull splits next to the base, keeping their on-device filenames.
	dir := filepath.Dir(localPath)
	for _, p := range paths[1:] {
		dst := filepath.Join(dir, path.Base(p))
		if out, err := c.pullOne(ctx, serial, p, dst); err != nil {
			return out, fmt.Errorf("pulled base but a split failed (%s): %w", path.Base(p), err)
		}
	}
	return fmt.Sprintf("%s (+%d split APK(s) in %s)", localPath, len(paths)-1, dir), nil
}

func (c *Client) pullOne(ctx context.Context, serial, remote, local string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "pull", remote, local)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}
