package adb

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
)

// HostsApplyResult describes how the hosts override write actually landed on
// the device. The frontend uses this to tell the user whether a reboot is
// required (Magisk module path) or whether the change is live immediately.
type HostsApplyResult struct {
	Path        string `json:"path"`        // canonical hosts path written
	Strategy    string `json:"strategy"`    // direct, magisk-remount, overlayfs-remount, magisk-module
	NeedsReboot bool   `json:"needsReboot"` // true when Magisk module was scaffolded
	NetdFlushed bool   `json:"netdFlushed"` // true if we managed to clear netd cache
	Content     string `json:"content"`     // final on-device content (read back)
	Diagnostics string `json:"diagnostics"` // human-readable transcript
}

// canonicalHostsPath resolves whichever of /system/etc/hosts and /etc/hosts is
// the actual file (the other is usually a symlink). Returns the resolved path
// or "/system/etc/hosts" as a safe default.
func (c *Client) canonicalHostsPath(ctx context.Context, serial string) string {
	out, _ := c.Shell(ctx, serial, "readlink -f /system/etc/hosts 2>/dev/null; readlink -f /etc/hosts 2>/dev/null")
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" && strings.HasSuffix(ln, "/hosts") {
			return ln
		}
	}
	return "/system/etc/hosts"
}

// ApplyHostsRobust writes hosts content using whichever strategy actually
// sticks on this device. It tries, in order:
//
//  1. Plain write — works only if /system is already rw (rare).
//  2. `magisk --remount-system` then write — Magisk 23+ on Android 10+.
//  3. `mount -o rw,remount /` (system-as-root) then write.
//  4. `mount -o rw,remount /system` (legacy split layout) then write.
//  5. `mount -o rw,remount,bind <hosts>` — overlayfs trick for stock Android.
//  6. Magisk module scaffolding — creates /data/adb/modules/adbq-hosts/system
//     /etc/hosts and module.prop. Survives reboots; requires one reboot to
//     take effect.
//
// After every successful write we md5-verify by re-reading through a fresh
// shell (avoiding the cat-after-write page-cache trap) and flush netd's DNS
// cache so apps see the new mapping immediately.
func (c *Client) ApplyHostsRobust(ctx context.Context, serial, content string) (*HostsApplyResult, error) {
	res := &HostsApplyResult{}
	var diag strings.Builder

	content = normalizeHostsContent(content)
	wantHash := md5sum(content)

	hostsPath := c.canonicalHostsPath(ctx, serial)
	res.Path = hostsPath
	diag.WriteString("canonical hosts path: " + hostsPath + "\n")

	// Stage the new content in a tmp file we can read+write without root.
	stagePath := hostsStage
	if _, err := c.Shell(ctx, serial, hostsStageCmd(content)); err != nil {
		return res, fmt.Errorf("stage failed: %w", err)
	}
	diag.WriteString("staged: " + stagePath + "\n")

	for _, s := range hostsStrategies(stagePath, hostsPath) {
		out, _, err := c.ShellSU(ctx, serial, s.Cmd)
		diag.WriteString("\n[" + s.Name + "]\n" + strings.TrimSpace(out) + "\n")
		if err != nil {
			continue
		}
		// Verify via md5 against a *fresh* read. Use `sync; echo 3 >/proc/sys/vm/drop_caches`
		// when possible to defeat page cache lying about overlay/dm-verity writes.
		got, _, _ := c.ShellSU(ctx, serial, hostsVerifyCmd(hostsPath))
		got = strings.TrimSpace(got)
		diag.WriteString("verify md5: want=" + wantHash + " got=" + got + "\n")
		if got == wantHash {
			res.Strategy = s.Name
			res.Content = content
			break
		}
	}

	if res.Strategy == "" {
		// Last-ditch: scaffold a Magisk module that pins the hosts file.
		// Survives reboots; takes effect after the next boot.
		out, _, err := c.ShellSU(ctx, serial, hostsMagiskModuleCmd(stagePath, wantHash))
		diag.WriteString("\n[magisk-module]\n" + strings.TrimSpace(out) + "\n")
		if err != nil || !strings.Contains(out, "OK") {
			diag.WriteString("\nALL STRATEGIES FAILED\n")
			res.Diagnostics = diag.String()
			return res, fmt.Errorf("could not write hosts file — see diagnostics. Likely dm-verity / read-only /system with no Magisk")
		}
		res.Strategy = "magisk-module"
		res.NeedsReboot = true
		res.Content = content
	}

	// Clean up the staging file (but not for bind-mount, which needs it alive).
	if res.Strategy != "bind-mount" {
		_, _ = c.Shell(ctx, serial, "rm -f "+stagePath)
	}

	// Flush netd's DNS cache — without this, running apps keep using the
	// pre-edit IP until their own caches expire.
	if _, err := c.FlushDNS(ctx, serial); err == nil {
		res.NetdFlushed = true
	}

	res.Diagnostics = diag.String()
	return res, nil
}

// hostsStage is the file the new content is written to before any root command
// runs: writable without root, and the bind-mount strategy keeps using it as
// the live source.
const hostsStage = "/data/local/tmp/adbq-hosts.new"

// normalizeHostsContent fixes the line endings before anything hashes or writes
// the content: toybox awk chokes on CRLF, and a missing trailing newline makes
// the md5 verification disagree with what the device holds.
func normalizeHostsContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content
}

// hostsStageCmd writes content to the staging path. printf rather than a
// heredoc: `echo` mangles backslashes on some ROMs.
func hostsStageCmd(content string) string {
	esc := strings.ReplaceAll(content, "'", `'\''`)
	return fmt.Sprintf("printf '%%s' '%s' > %s && chmod 644 %s", esc, hostsStage, hostsStage)
}

// hostsStrategies are the writes tried in order, each idempotent and each
// failing loudly so the next one gets a turn. Shared with the preview so the
// user reads the same escalation adbq will walk.
func hostsStrategies(stagePath, hostsPath string) []shellStrategy {
	return []shellStrategy{
		{"direct", fmt.Sprintf("cp %s %s && sync && echo OK", stagePath, hostsPath)},
		{"magisk-remount", fmt.Sprintf(
			"magisk --remount-system 2>&1 && cp %s %s && sync && echo OK", stagePath, hostsPath)},
		{"remount-root", fmt.Sprintf(
			"mount -o rw,remount / 2>&1 && cp %s %s && sync && mount -o ro,remount / 2>/dev/null; echo OK",
			stagePath, hostsPath)},
		{"remount-system", fmt.Sprintf(
			"mount -o rw,remount /system 2>&1 && cp %s %s && sync && mount -o ro,remount /system 2>/dev/null; echo OK",
			stagePath, hostsPath)},
		// File-level live override for read-only /system (A10+ non-Magisk root) —
		// the single-file equivalent of the certs tmpfs overlay. Label the bind
		// source with the hosts file's SELinux type first so the overlay presents
		// the right context on enforcing devices; otherwise apps reading
		// /system/etc/hosts hit an avc denial.
		{"bind-mount", fmt.Sprintf(
			"chcon u:object_r:system_file:s0 %s 2>/dev/null; mount --bind %s %s 2>&1 && echo OK",
			stagePath, stagePath, hostsPath)},
	}
}

// hostsVerifyCmd re-reads the file through a fresh shell, dropping caches
// first: the page cache happily reports a write that dm-verity swallowed.
func hostsVerifyCmd(hostsPath string) string {
	return "sync; (echo 3 > /proc/sys/vm/drop_caches 2>/dev/null || true); md5sum " +
		hostsPath + " 2>/dev/null | awk '{print $1}'"
}

// hostsMagiskModuleCmd scaffolds a Magisk module that pins the hosts file.
// Survives reboots; takes effect after the next boot.
func hostsMagiskModuleCmd(stagePath, wantHash string) string {
	const moduleDir = "/data/adb/modules/adbq-hosts"
	modProp := fmt.Sprintf(`id=adbq-hosts
name=adbq hosts override
version=v1
versionCode=1
author=adbq
description=Persists /system/etc/hosts (md5=%s)
`, wantHash)
	modPropEsc := strings.ReplaceAll(modProp, "'", `'\''`)
	// /system/etc/hosts inside the module is overlay-mounted by Magisk at boot,
	// so the device sees our file as if it were the real one.
	return fmt.Sprintf(`
test -d /data/adb/modules || { echo "no magisk modules dir — install Magisk first"; exit 1; }
mkdir -p %s/system/etc || exit 1
cp %s %s/system/etc/hosts || exit 1
chmod 644 %s/system/etc/hosts
printf '%%s' '%s' > %s/module.prop
touch %s/skip_mount
rm -f %s/disable %s/remove
echo OK
`, moduleDir, stagePath, moduleDir, moduleDir, modPropEsc, moduleDir, moduleDir, moduleDir, moduleDir)
}

func md5sum(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
