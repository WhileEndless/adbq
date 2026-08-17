package adb

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Staging keeps an app's APKs as loose files on the host, which is what
// host-side analysis tools take as input. ExportApks deliberately produces one
// `.apks` archive instead, so it cannot be reused here: a decompiler wants the
// parts, not the container.
//
// The directory is disposable — cache, never ~/.adbq (see frida_paths.go for
// the rule). Keying it on the version code means an update re-stages instead of
// quietly serving the previous build's code.

// StagedApks is a package's APKs after they have been copied to this computer.
type StagedApks struct {
	Dir     string   `json:"dir"`
	Files   []string `json:"files"`   // absolute host paths, base first
	Names   []string `json:"names"`   // file names, aligned with Files
	Version string   `json:"version"` // version code the staging is keyed on
	Cached  bool     `json:"cached"`  // everything was already there; nothing was pulled
}

// stageRoot is the parent of every per-package staging directory.
func stageRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "adbq", "apkwork")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// StageRoot reports where staged APKs live, for the UI's "clear cache" control.
func StageRoot() string {
	d, err := stageRoot()
	if err != nil {
		return ""
	}
	return d
}

// ClearApkStage discards every staged APK. Nothing is lost — the next open
// re-pulls.
func ClearApkStage() error {
	base, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(base, "adbq", "apkwork"))
}

// stageKey names the directory for one package at one version. Version codes
// are numeric in practice, but a ROM can report anything, so the value is
// sanitised rather than trusted.
func stageKey(pkg, version string) string {
	if version == "" {
		version = "unknown"
	}
	return sanitizePathSegment(pkg) + "-" + sanitizePathSegment(version)
}

// sanitizePathSegment reduces a value to something safe to use as one path
// element: no separators, no traversal, no surprises.
func sanitizePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "_"
	}
	return out
}

// StageApks copies every APK of the package to a cache directory and returns
// their host paths, base first.
//
// A file that is already there at non-zero size is not pulled again, so opening
// the same app twice costs nothing.
func (c *Client) StageApks(ctx context.Context, serial, pkg string, progress func(string)) (*StagedApks, error) {
	note := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	set, err := c.ApkSetOf(ctx, serial, pkg)
	if err != nil {
		return nil, err
	}
	version := ""
	if d, err := c.DescribeApp(ctx, serial, pkg); err == nil && d != nil {
		version = d.VersionCode
	}

	root, err := stageRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, stageKey(pkg, version))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	remote := append([]string{set.Base}, set.Splits...)
	out := &StagedApks{Dir: dir, Version: version, Cached: true}
	for i, p := range remote {
		name := uniqueEntryName(path.Base(p), out.Names)
		local := filepath.Join(dir, name)
		if fi, err := os.Stat(local); err == nil && fi.Size() > 0 {
			out.Files = append(out.Files, local)
			out.Names = append(out.Names, name)
			continue
		}
		out.Cached = false
		note(fmt.Sprintf("pulling %d/%d: %s", i+1, len(remote), name))
		if o, err := c.pullOne(ctx, serial, p, local); err != nil {
			return nil, fmt.Errorf("pull %s failed: %w (%s)", name, err, strings.TrimSpace(o))
		}
		out.Files = append(out.Files, local)
		out.Names = append(out.Names, name)
	}
	return out, nil
}
