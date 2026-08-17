package adb

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ApkSet describes the APK files that make up one installed package, plus the
// exact adb commands adbq will run to export them. The UI prints the commands
// verbatim so an export is auditable and reproducible by hand.
type ApkSet struct {
	Pkg       string   `json:"pkg"`
	Base      string   `json:"base"`      // on-device path of the base APK
	Splits    []string `json:"splits"`    // on-device paths of the split APKs
	Split     bool     `json:"split"`     // true when the app is an App Bundle install
	Suggested string   `json:"suggested"` // suggested local file name
	Commands  []string `json:"commands"`
	// Version is what Suggested is named after, carried along so callers that
	// need another file name for the same app do not read the device again.
	Version AppVersion `json:"version"`
}

// ApkInstallPlan is what InstallApkBundle would do with a local file: which
// APKs inside it apply to this device, which were skipped and why, and the
// command that performs the install.
type ApkInstallPlan struct {
	File     string   `json:"file"`
	Install  []string `json:"install"` // file names inside the bundle, in install order
	Skipped  []string `json:"skipped"` // "name — reason"
	Split    bool     `json:"split"`
	Commands []string `json:"commands"`
}

// apkBundleExts are the container formats we can install. `.apks` is what adbq
// exports (and what SAI/bundletool produce); `.xapk` and plain `.zip` are the
// same idea with a different name, so we accept them too.
var apkBundleExts = map[string]bool{".apks": true, ".xapk": true, ".zip": true, ".apkm": true}

// IsApkBundle reports whether localPath looks like a multi-APK container
// rather than a single APK.
func IsApkBundle(localPath string) bool {
	return apkBundleExts[strings.ToLower(filepath.Ext(localPath))]
}

// EnsureExportExt makes the file name match what adbq actually writes into it.
// A save dialog hands back whatever the user typed, and a split export stored
// under a `.apk` name is a zip archive masquerading as a single APK: every
// installer — adbq's own included, since IsApkBundle dispatches on the
// extension — then hands it to `adb install` and the install fails.
func EnsureExportExt(dst string, split bool) string {
	ext := filepath.Ext(dst)
	stem := strings.TrimSuffix(dst, ext)
	low := strings.ToLower(ext)
	if split {
		if apkBundleExts[low] {
			return dst
		}
		if low == ".apk" {
			return stem + ".apks"
		}
		return dst + ".apks"
	}
	if low == ".apk" {
		return dst
	}
	if apkBundleExts[low] {
		return stem + ".apk"
	}
	return dst + ".apk"
}

// ApkSetOf inspects the package on the device and returns its APK layout with
// the commands an export would run. It performs a single `pm path` call.
func (c *Client) ApkSetOf(ctx context.Context, serial, pkg string) (*ApkSet, error) {
	paths, err := c.PathsOfApp(ctx, serial, pkg)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no APK path found for %s", pkg)
	}
	// A second read, and a cheap one, so the suggested file name can carry the
	// version. It is best-effort by design — see AppVersionOf.
	return apkSetFromPaths(serial, pkg, paths, c.AppVersionOf(ctx, serial, pkg)), nil
}

// apkSetFromPaths is the pure half of ApkSetOf so the command preview can be
// unit-tested without a device.
func apkSetFromPaths(serial, pkg string, paths []string, ver AppVersion) *ApkSet {
	base, splits := baseAndSplits(paths)
	// A nil slice marshals to JSON null, and the UI reads .length off these —
	// keep every slice field empty-but-present.
	if splits == nil {
		splits = []string{}
	}
	s := &ApkSet{Pkg: pkg, Base: base, Splits: splits, Split: len(splits) > 0, Version: ver}
	stem := ExportBaseName(pkg, ver)
	s.Suggested = stem + ".apk"
	if s.Split {
		s.Suggested = stem + ".apks"
	}
	s.Commands = append(s.Commands, "adb -s "+serial+" shell pm path "+pkg)
	for _, p := range append([]string{base}, splits...) {
		s.Commands = append(s.Commands, "adb -s "+serial+" pull "+p+" ./"+path.Base(p))
	}
	if s.Split {
		s.Commands = append(s.Commands, "# adbq zips the "+strconv.Itoa(len(paths))+" pulled APKs into "+s.Suggested)
	}
	return s
}

// baseAndSplits separates the base APK from the config/feature splits. `pm
// path` usually lists the base first, but not on every ROM, so we look for the
// conventional file name before falling back to order.
func baseAndSplits(paths []string) (string, []string) {
	baseIdx := 0
	for i, p := range paths {
		if strings.EqualFold(path.Base(p), "base.apk") {
			baseIdx = i
			break
		}
	}
	var splits []string
	for i, p := range paths {
		if i != baseIdx {
			splits = append(splits, p)
		}
	}
	return paths[baseIdx], splits
}

// ExportApks writes the package to dst in the form that actually matches it:
// a split (App Bundle) install becomes one `.apks` archive holding every APK,
// and a plain single-APK app is simply pulled as an `.apk`. Wrapping a lone
// APK in an archive would only make it harder to install elsewhere.
//
// Archive entries sit at the root under their on-device file names, which is
// the layout SAI and adbq's own installer read.
//
// The APK bytes are copied verbatim in both cases — nothing is re-zipped or
// re-signed, so v1/v2/v3 signatures stay valid.
func (c *Client) ExportApks(ctx context.Context, serial, pkg, dst string, progress func(string)) (string, error) {
	note := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	set, err := c.ApkSetOf(ctx, serial, pkg)
	if err != nil {
		return "", err
	}
	if !set.Split {
		note("pulling " + path.Base(set.Base))
		if out, err := c.pullOne(ctx, serial, set.Base, dst); err != nil {
			return "", fmt.Errorf("pull %s failed: %w (%s)", path.Base(set.Base), err, strings.TrimSpace(out))
		}
		return dst, nil
	}
	paths := append([]string{set.Base}, set.Splits...)

	tmp, err := os.MkdirTemp("", "adbq-apks-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	local := make([]string, 0, len(paths))
	for i, p := range paths {
		name := uniqueEntryName(path.Base(p), local)
		note(fmt.Sprintf("pulling %d/%d: %s", i+1, len(paths), name))
		dstFile := filepath.Join(tmp, name)
		if out, err := c.pullOne(ctx, serial, p, dstFile); err != nil {
			return "", fmt.Errorf("pull %s failed: %w (%s)", path.Base(p), err, strings.TrimSpace(out))
		}
		local = append(local, name)
	}

	note("packing " + filepath.Base(dst))
	meta := saiMeta{
		Package:         pkg,
		Label:           pkg,
		ExportTimestamp: time.Now().UnixMilli(),
		SplitApk:        len(paths) > 1,
	}
	if d, err := c.DescribeApp(ctx, serial, pkg); err == nil && d != nil {
		if d.Name != "" {
			meta.Label = d.Name
		}
		meta.VersionName = d.Version
		meta.VersionCode, _ = strconv.ParseInt(d.VersionCode, 10, 64)
	}
	if err := writeApksArchive(dst, tmp, local, meta); err != nil {
		return "", err
	}
	return dst, nil
}

// uniqueEntryName keeps archive entry names distinct even if two on-device
// paths happen to share a base name.
func uniqueEntryName(name string, taken []string) string {
	seen := func(n string) bool {
		for _, t := range taken {
			if strings.EqualFold(t, n) {
				return true
			}
		}
		return false
	}
	if !seen(name) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !seen(cand) {
			return cand
		}
	}
}

// saiMeta is the metadata file Split APKs Installer reads. We emit its exact
// schema so an adbq export also opens in third-party installers; adbq's own
// installer does not depend on it.
type saiMeta struct {
	Package         string   `json:"package"`
	Label           string   `json:"label"`
	VersionName     string   `json:"version_name"`
	VersionCode     int64    `json:"version_code"`
	ExportTimestamp int64    `json:"export_timestamp"`
	SplitApk        bool     `json:"split_apk"`
	BackupComps     []string `json:"backup_components"`
}

func writeApksArchive(dst, dir string, names []string, meta saiMeta) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	addFile := func(name string, body io.Reader) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		_, err = io.Copy(w, body)
		return err
	}
	err = func() error {
		for _, n := range names {
			src, err := os.Open(filepath.Join(dir, n))
			if err != nil {
				return err
			}
			err = addFile(n, src)
			src.Close()
			if err != nil {
				return err
			}
		}
		if meta.BackupComps == nil {
			meta.BackupComps = []string{}
		}
		b, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			return err
		}
		return addFile("meta.sai_v2.json", strings.NewReader(string(b)))
	}()
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

// ─── Install ─────────────────────────────────────────────────────────────

// PlanApkInstall reads the container without installing anything and reports
// which APKs apply to this device. Used for the UI's command preview.
func (c *Client) PlanApkInstall(ctx context.Context, serial, localPath string) (*ApkInstallPlan, error) {
	if !IsApkBundle(localPath) {
		return &ApkInstallPlan{
			File:     localPath,
			Install:  []string{filepath.Base(localPath)},
			Skipped:  []string{},
			Commands: []string{"adb -s " + serial + " install -r " + shellQuoteLocal(localPath)},
		}, nil
	}
	zr, err := zip.OpenReader(localPath)
	if err != nil {
		return nil, fmt.Errorf("not a readable .apks/.zip archive: %w", err)
	}
	defer zr.Close()
	var names []string
	for _, e := range zr.File {
		if !e.FileInfo().IsDir() {
			names = append(names, e.Name)
		}
	}
	caps := c.Capabilities(ctx, serial)
	sel := selectApkEntries(names, caps.ABIList, c.deviceDensity(ctx, serial))
	if sel.err != nil {
		return nil, sel.err
	}
	plan := &ApkInstallPlan{File: localPath, Install: sel.keep, Skipped: sel.skipped, Split: len(sel.keep) > 1}
	args := make([]string, 0, len(sel.keep))
	for _, n := range sel.keep {
		args = append(args, "./"+path.Base(n))
	}
	if plan.Split {
		plan.Commands = []string{"adb -s " + serial + " install-multiple -r " + strings.Join(args, " ")}
	} else {
		plan.Commands = []string{"adb -s " + serial + " install -r " + strings.Join(args, " ")}
	}
	plan.Commands = append([]string{"# adbq unpacks " + filepath.Base(localPath) + " to a temp dir first"}, plan.Commands...)
	return plan, nil
}

// InstallApkBundle installs a single APK or a multi-APK container. Split
// installs go through `adb install-multiple`, which commits every APK in one
// pm session — installing them one by one fails with INSTALL_FAILED_MISSING_SPLIT.
func (c *Client) InstallApkBundle(ctx context.Context, serial, localPath string, progress func(string)) (string, error) {
	note := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	if !IsApkBundle(localPath) {
		note("installing " + filepath.Base(localPath))
		return c.InstallAPK(ctx, serial, localPath)
	}
	tmp, err := os.MkdirTemp("", "adbq-install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	note("unpacking " + filepath.Base(localPath))
	zr, err := zip.OpenReader(localPath)
	if err != nil {
		return "", fmt.Errorf("not a readable .apks/.zip archive: %w", err)
	}
	var names []string
	for _, e := range zr.File {
		if !e.FileInfo().IsDir() {
			names = append(names, e.Name)
		}
	}
	caps := c.Capabilities(ctx, serial)
	sel := selectApkEntries(names, caps.ABIList, c.deviceDensity(ctx, serial))
	if sel.err != nil {
		zr.Close()
		return "", sel.err
	}
	wanted := map[string]bool{}
	for _, n := range sel.keep {
		wanted[n] = true
	}
	var files []string
	for _, e := range zr.File {
		if !wanted[e.Name] {
			continue
		}
		dst, err := extractZipEntry(e, tmp)
		if err != nil {
			zr.Close()
			return "", err
		}
		files = append(files, dst)
	}
	zr.Close()
	if len(files) == 0 {
		return "", errors.New("the archive contains no APK that applies to this device")
	}
	// Keep the base first: pm rejects a session whose first APK is a split.
	sort.SliceStable(files, func(i, j int) bool {
		return apkEntryRank(filepath.Base(files[i])) < apkEntryRank(filepath.Base(files[j]))
	})
	if len(files) == 1 {
		note("installing " + filepath.Base(files[0]))
		return c.InstallAPK(ctx, serial, files[0])
	}
	note(fmt.Sprintf("installing %d APKs in one session", len(files)))
	return c.InstallMultiple(ctx, serial, files)
}

// InstallMultiple runs `adb install-multiple -r <files...>`, the split-aware
// install path.
func (c *Client) InstallMultiple(ctx context.Context, serial string, localPaths []string) (string, error) {
	if len(localPaths) == 0 {
		return "", errors.New("no APK to install")
	}
	args := append([]string{"install-multiple", "-r"}, localPaths...)
	cmd, err := c.DeviceCommand(ctx, serial, args...)
	if err != nil {
		return "", err
	}
	out, err := Run(cmd)
	return out, installMultipleErr(out, err)
}

// installMultipleErr turns pm's terse session failures into something a user
// can act on. adb frequently exits 0 on a failed install, so the output is the
// real signal.
func installMultipleErr(out string, err error) error {
	e := pmResultErr(out, err)
	if e == nil {
		return nil
	}
	msg := e.Error()
	switch {
	case strings.Contains(msg, "INSTALL_FAILED_MISSING_SPLIT"):
		return fmt.Errorf("%s — the archive is missing a split this app requires; export it again with every split", msg)
	case strings.Contains(msg, "INSTALL_FAILED_VERSION_DOWNGRADE"):
		return fmt.Errorf("%s — a newer version is installed; uninstall it first", msg)
	case strings.Contains(msg, "INSTALL_FAILED_UPDATE_INCOMPATIBLE") || strings.Contains(msg, "signatures do not match"):
		return fmt.Errorf("%s — the installed copy was signed with a different key; uninstall it first", msg)
	case strings.Contains(msg, "INSTALL_FAILED_NO_MATCHING_ABIS"):
		return fmt.Errorf("%s — this archive has no native code for the device's ABI", msg)
	case strings.Contains(msg, "INSTALL_PARSE_FAILED_NO_CERTIFICATES"):
		return fmt.Errorf("%s — the APKs are unsigned", msg)
	case strings.Contains(msg, "unknown command") || strings.Contains(msg, "Unknown command"):
		return fmt.Errorf("%s — this adb is too old for split installs (install-multiple)", msg)
	}
	return e
}

// apkEntryRank orders the install session: base first, then feature modules,
// then config splits. pm derives the session's package name from the first
// APK, so a config split must never lead.
func apkEntryRank(name string) int {
	kind, _ := apkSplitKind(name)
	switch {
	case strings.EqualFold(name, "base.apk"):
		return 0
	case kind == "":
		return 1
	default:
		return 2
	}
}

func extractZipEntry(e *zip.File, dir string) (string, error) {
	// Flatten: bundletool nests APKs under splits/, and a nested path would
	// also be the classic zip-slip vector.
	name := filepath.Base(filepath.FromSlash(e.Name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", fmt.Errorf("refusing suspicious archive entry %q", e.Name)
	}
	rc, err := e.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dst := filepath.Join(dir, name)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, rc)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return dst, err
}

// ─── Split selection ─────────────────────────────────────────────────────

type apkSelection struct {
	keep    []string
	skipped []string
	err     error
}

// bundleMetaFiles are container bookkeeping files — present in every export,
// never installable, and not worth reporting as "skipped".
var bundleMetaFiles = map[string]bool{
	"meta.sai_v2.json": true, "meta.sai_v1.json": true,
	"toc.pb": true, "manifest.json": true, "icon.png": true,
}

var (
	reLocaleSplit  = regexp.MustCompile(`^[a-z]{2,3}([-_][A-Za-z]{2,4})?$`)
	densityBuckets = map[string]int{
		"ldpi": 120, "mdpi": 160, "tvdpi": 213, "hdpi": 240,
		"xhdpi": 320, "xxhdpi": 480, "xxxhdpi": 640,
	}
	abiSplits = map[string]string{
		"armeabi": "armeabi", "armeabi_v7a": "armeabi-v7a", "armeabi-v7a": "armeabi-v7a",
		"arm64_v8a": "arm64-v8a", "arm64-v8a": "arm64-v8a",
		"x86": "x86", "x86_64": "x86_64", "mips": "mips", "mips64": "mips64",
	}
)

// apkSplitKind classifies a split APK file name into "abi", "density",
// "locale", or "" for the base/feature APKs. It understands both the
// on-device naming (`split_config.arm64_v8a.apk`) and bundletool's
// (`base-arm64_v8a.apk`, `splits/base-xxhdpi.apk`).
func apkSplitKind(name string) (kind, token string) {
	n := path.Base(strings.ReplaceAll(name, "\\", "/"))
	if !strings.HasSuffix(strings.ToLower(n), ".apk") {
		return "", ""
	}
	stem := n[:len(n)-len(".apk")]
	cand := stem
	if i := strings.LastIndex(cand, "config."); i >= 0 {
		cand = cand[i+len("config."):]
	} else if i := strings.LastIndexAny(cand, "-"); i >= 0 {
		cand = cand[i+1:]
	}
	low := strings.ToLower(cand)
	if abi, ok := abiSplits[low]; ok {
		return "abi", abi
	}
	if _, ok := densityBuckets[low]; ok {
		return "density", low
	}
	if low == "nodpi" || low == "anydpi" {
		return "density", low
	}
	if reLocaleSplit.MatchString(cand) && cand != stem {
		return "locale", cand
	}
	return "", ""
}

// selectApkEntries picks the APKs inside a container that apply to this
// device: every base/feature APK, the ABI splits the device can run, the
// closest density bucket, and all language splits (they are small and always
// valid). Everything dropped is reported with a reason.
func selectApkEntries(names []string, abis []string, densityDpi int) apkSelection {
	apks := preferredApkEntries(names)
	if len(apks) == 0 {
		return apkSelection{err: errors.New("the archive contains no .apk file")}
	}
	abiOK := map[string]bool{}
	for _, a := range abis {
		abiOK[strings.ToLower(strings.TrimSpace(a))] = true
	}
	// Choose one density bucket up front so we do not keep several.
	bestDensity := closestDensityBucket(apks, densityDpi)

	var sel apkSelection
	sawAbiSplit, keptAbiSplit := false, false
	for _, n := range apks {
		kind, token := apkSplitKind(n)
		switch kind {
		case "abi":
			sawAbiSplit = true
			if len(abiOK) == 0 || abiOK[token] {
				keptAbiSplit = true
				sel.keep = append(sel.keep, n)
			} else {
				sel.skipped = append(sel.skipped, path.Base(n)+" — other ABI ("+token+")")
			}
		case "density":
			if token == "nodpi" || token == "anydpi" || bestDensity == "" || token == bestDensity {
				sel.keep = append(sel.keep, n)
			} else {
				sel.skipped = append(sel.skipped, path.Base(n)+" — other screen density ("+token+")")
			}
		default:
			sel.keep = append(sel.keep, n)
		}
	}
	if sawAbiSplit && !keptAbiSplit {
		sel.err = fmt.Errorf("this archive has native code only for other ABIs; the device supports %s", strings.Join(abis, ", "))
		return sel
	}
	for _, n := range names {
		if strings.HasSuffix(strings.ToLower(n), ".apk") || bundleMetaFiles[strings.ToLower(path.Base(n))] {
			continue
		}
		sel.skipped = append(sel.skipped, path.Base(n)+" — not an APK, ignored")
	}
	return sel
}

// preferredApkEntries resolves bundletool's layout: a `build-apks` output
// carries splits/, standalones/ and sometimes universal.apk side by side, and
// only one of those sets may be installed.
func preferredApkEntries(names []string) []string {
	var splits, standalones, universal, root []string
	for _, n := range names {
		if !strings.HasSuffix(strings.ToLower(n), ".apk") {
			continue
		}
		slash := strings.ReplaceAll(n, "\\", "/")
		switch {
		case strings.HasPrefix(slash, "splits/"):
			splits = append(splits, n)
		case strings.HasPrefix(slash, "standalones/"):
			standalones = append(standalones, n)
		case strings.EqualFold(path.Base(slash), "universal.apk"):
			universal = append(universal, n)
		default:
			root = append(root, n)
		}
	}
	switch {
	case len(splits) > 0:
		return splits
	case len(root) > 0:
		return root
	case len(universal) > 0:
		return universal
	default:
		return standalones
	}
}

// closestDensityBucket returns the density split the device should get, or ""
// when the archive has none or the device density is unknown.
func closestDensityBucket(names []string, dpi int) string {
	present := map[string]bool{}
	for _, n := range names {
		if kind, token := apkSplitKind(n); kind == "density" {
			if _, ok := densityBuckets[token]; ok {
				present[token] = true
			}
		}
	}
	if len(present) == 0 || dpi <= 0 {
		return ""
	}
	keys := make([]string, 0, len(present))
	for k := range present {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return densityBuckets[keys[i]] < densityBuckets[keys[j]] })
	best, bestDist := "", 1<<30
	for _, k := range keys {
		d := densityBuckets[k] - dpi
		if d < 0 {
			d = -d
		}
		// `<=` with the buckets in ascending order means a tie resolves to the
		// denser split — scaling artwork down beats scaling it up.
		if d <= bestDist {
			best, bestDist = k, d
		}
	}
	return best
}

// deviceDensity reads the screen density in dpi, 0 when unknown.
func (c *Client) deviceDensity(ctx context.Context, serial string) int {
	out, err := c.Shell(ctx, serial, "wm density 2>/dev/null; getprop ro.sf.lcd_density")
	if err != nil {
		return 0
	}
	// `wm density` wins when present: it reports the override the user set.
	if m := regexp.MustCompile(`Override density:\s*(\d+)`).FindStringSubmatch(out); m != nil {
		if n, _ := strconv.Atoi(m[1]); n > 0 {
			return n
		}
	}
	if m := regexp.MustCompile(`Physical density:\s*(\d+)`).FindStringSubmatch(out); m != nil {
		if n, _ := strconv.Atoi(m[1]); n > 0 {
			return n
		}
	}
	for _, ln := range strings.Split(out, "\n") {
		if n, err := strconv.Atoi(strings.TrimSpace(ln)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// shellQuoteLocal quotes a host path for display inside a copyable command.
func shellQuoteLocal(p string) string {
	if p != "" && !strings.ContainsAny(p, " \t'\"$`\\") {
		return p
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
