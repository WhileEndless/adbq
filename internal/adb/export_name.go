package adb

import (
	"context"
	"strings"
)

// Exports are named after the package *and* its version, because the package
// name alone is not enough to tell two saved files apart: pull the same app
// before and after an update and the second export silently overwrites the
// first, or sits beside it under a name that says nothing about which build it
// holds.
//
// Everything here degrades rather than fails. A ROM that will not report a
// version, a version name full of characters no file system wants, a version
// name that merely repeats the code — each ends up with a usable name.

// AppVersion is the version pair an export name is distinguished by. Either
// field may be empty.
type AppVersion struct {
	Name string `json:"name"` // versionName, e.g. "1.2.3"
	Code string `json:"code"` // versionCode, e.g. "10203"
}

// maxVersionInName bounds the version part of a file name. Version names are
// free text and occasionally hold a whole changelog; file systems are less
// forgiving.
const maxVersionInName = 40

// ExportBaseName builds the file name stem for an export, without extension.
func ExportBaseName(pkg string, v AppVersion) string {
	name := sanitizeVersionForName(v.Name)
	code := sanitizeVersionForName(v.Code)
	parts := []string{pkg}
	if name != "" {
		parts = append(parts, name)
	}
	// A version name that is just the code again would only lengthen the file
	// name without telling the user anything new.
	if code != "" && code != name {
		parts = append(parts, code)
	}
	return strings.Join(parts, "-")
}

// sanitizeVersionForName reduces a version string to something safe and legible
// in a file name: runs of anything unusual collapse into a single dash.
func sanitizeVersionForName(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-._")
	if len(out) > maxVersionInName {
		out = strings.Trim(out[:maxVersionInName], "-._")
	}
	return out
}

// AppVersionOf reads the package's version pair. It is best-effort: an empty
// result costs the export a nicer name, nothing more.
func (c *Client) AppVersionOf(ctx context.Context, serial, pkg string) AppVersion {
	out, err := c.Shell(ctx, serial, "dumpsys package "+pkg+" | grep -E 'versionCode=|versionName=' | head -n 20")
	if err != nil {
		return AppVersion{}
	}
	return parseAppVersion(out)
}

// parseAppVersion picks the version pair out of `dumpsys package` output. The
// first occurrence of each wins: later blocks describe other users or a pending
// install, not the package as it is installed now.
func parseAppVersion(out string) AppVersion {
	var v AppVersion
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if v.Code == "" {
			if got := kvFromLine(ln, "versionCode"); got != "" {
				v.Code = got
			}
		}
		if v.Name == "" {
			if got := kvFromLine(ln, "versionName"); got != "" {
				v.Name = got
			}
		}
		if v.Code != "" && v.Name != "" {
			break
		}
	}
	return v
}

// ExportBaseNameFor is the device-side convenience: read the version, build the
// name. Used wherever a save dialog needs a default file name.
func (c *Client) ExportBaseNameFor(ctx context.Context, serial, pkg string) string {
	return ExportBaseName(pkg, c.AppVersionOf(ctx, serial, pkg))
}
