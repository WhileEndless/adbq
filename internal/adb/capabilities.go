package adb

import (
	"context"
	"strconv"
	"strings"
)

// Capabilities is the cached, per-serial set of device facts that features use
// to decide what will work on a given (often old or stripped-down) ROM. It is
// collected in a single batched shell round-trip and cached for the device's
// connected lifetime — call InvalidateCapabilities when the device disconnects.
//
// This is the central registry feature code reads instead of each re-probing
// SDK level, SELinux mode, ABI, and binary presence on its own. Root/su style
// is handled separately by suStyleFor (it needs interactive probing); use
// ShellSU for privileged commands.
type Capabilities struct {
	SDK     int             `json:"sdk"`     // ro.build.version.sdk (0 if unknown)
	Release string          `json:"release"` // ro.build.version.release
	SELinux string          `json:"selinux"` // "enforcing" | "permissive" | "disabled" | ""
	ABI     string          `json:"abi"`     // ro.product.cpu.abi (primary)
	ABIList []string        `json:"abiList"` // ro.product.cpu.abilist
	Bits64  bool            `json:"bits64"`  // a 64-bit ABI is present
	Has     map[string]bool `json:"has"`     // presence of probed binaries (command -v)

	// ─── Identity and hardware ────────────────────────────────────────────
	//
	// None of the following can change while a device stays connected: a new
	// build id or kernel means a reboot, and a reboot drops this cache (see
	// DomProps). They live here rather than being re-read on every device poll
	// because that poll used to spend nine of its ten adb processes on exactly
	// these values.
	Serial       string `json:"serial"`       // ro.serialno — survives USB↔Wi-Fi
	Model        string `json:"model"`        // ro.product.model
	Manufacturer string `json:"manufacturer"` // ro.product.manufacturer
	Product      string `json:"product"`      // ro.product.name
	BuildID      string `json:"buildId"`      // ro.build.id
	BuildTags    string `json:"buildTags"`    // ro.build.tags — "test-keys" implies root
	Fingerprint  string `json:"fingerprint"`  // ro.build.fingerprint — identifies the ROM
	Hardware     string `json:"hardware"`     // ro.hardware
	Kernel       string `json:"kernel"`       // uname -r

	// MemTotalKB and StorageTotalKB are the fixed halves of the Overview
	// gauges; only the free/available side has to be re-read.
	MemTotalKB     int64 `json:"memTotalKb"`
	StorageTotalKB int64 `json:"storageTotalKb"`
	// NCPU is the core count from /proc/stat's per-cpu lines.
	NCPU int `json:"ncpu"`
}

// capBins is the set of binaries probed in the batched capability scan. Add a
// name here when a feature wants to gate on a binary's presence.
var capBins = []string{
	"su", "iptables", "ip6tables", "nft", "tcpdump", "ip", "ifconfig",
	"netcfg", "nslookup", "ping", "logcat", "pm", "cmd", "settings",
	"getenforce", "magisk", "aapt2", "screencap",
}

// Supports reports whether the device has the named binary available.
func (caps *Capabilities) Supports(bin string) bool {
	return caps != nil && caps.Has[bin]
}

// AndroidAtLeast reports whether the device's SDK level is >= level. A 0/unknown
// SDK reports false.
func (caps *Capabilities) AndroidAtLeast(level int) bool {
	return caps != nil && caps.SDK >= level
}

// SELinuxEnforcing reports whether SELinux is known to be enforcing.
func (caps *Capabilities) SELinuxEnforcing() bool {
	return caps != nil && caps.SELinux == "enforcing"
}

// Capabilities returns the cached capabilities for serial, probing once on the
// first call. A probe failure still yields a (possibly partial) non-nil struct
// so callers degrade gracefully rather than hard-fail.
func (c *Client) Capabilities(ctx context.Context, serial string) *Capabilities {
	c.capMu.Lock()
	if c.caps == nil {
		c.caps = map[string]*Capabilities{}
	}
	if caps, ok := c.caps[serial]; ok {
		c.capMu.Unlock()
		return caps
	}
	c.capMu.Unlock()

	caps := parseCapabilities(c.probeCapabilitiesRaw(ctx, serial))

	// Only cache a probe that actually returned something. A transient failure
	// (device briefly offline, root not yet granted, slow USB) yields an
	// all-zero struct; caching it would permanently convince callers the device
	// has no ABI/binaries, with no recovery. Leaving it uncached lets the next
	// call re-probe.
	if caps.SDK > 0 || caps.ABI != "" || len(caps.Has) > 0 {
		c.capMu.Lock()
		if c.caps == nil {
			c.caps = map[string]*Capabilities{}
		}
		c.caps[serial] = caps
		c.capMu.Unlock()
	}
	return caps
}

// InvalidateCapabilities drops the cached probe for serial. Call it on
// disconnect, or when the device's root/SELinux state may have changed.
func (c *Client) InvalidateCapabilities(serial string) {
	c.capMu.Lock()
	delete(c.caps, serial)
	c.capMu.Unlock()
}

// probeCapabilitiesRaw runs one batched shell command and returns its raw
// output. Sections are separated by a sentinel so parsing needs no awk/sed
// (matching the procfs scan approach), and binary presence is reported by
// echoing the name of each found binary.
// Sections are positional, so new ones are only ever APPENDED. Inserting one
// would silently shift every field after it — and the parser has no way to
// notice, because a getprop that returns nothing is indistinguishable from a
// property that is simply unset on this ROM.
func (c *Client) probeCapabilitiesRaw(ctx context.Context, serial string) string {
	var sb strings.Builder
	sb.WriteString("getprop ro.build.version.sdk; echo '@@@'; ")
	sb.WriteString("getprop ro.build.version.release; echo '@@@'; ")
	sb.WriteString("getprop ro.product.cpu.abi; echo '@@@'; ")
	sb.WriteString("getprop ro.product.cpu.abilist; echo '@@@'; ")
	sb.WriteString("getenforce 2>/dev/null; echo '@@@'; ")
	sb.WriteString("for b in ")
	sb.WriteString(strings.Join(capBins, " "))
	sb.WriteString("; do command -v \"$b\" >/dev/null 2>&1 && echo \"$b\"; done")
	// ── Appended: identity, hardware and the fixed halves of the gauges. ──
	sb.WriteString("; echo '@@@'; getprop ro.serialno")
	sb.WriteString("; echo '@@@'; getprop ro.product.model")
	sb.WriteString("; echo '@@@'; getprop ro.product.manufacturer")
	sb.WriteString("; echo '@@@'; getprop ro.product.name")
	sb.WriteString("; echo '@@@'; getprop ro.build.id")
	sb.WriteString("; echo '@@@'; getprop ro.build.tags")
	sb.WriteString("; echo '@@@'; getprop ro.build.fingerprint")
	sb.WriteString("; echo '@@@'; getprop ro.hardware")
	sb.WriteString("; echo '@@@'; uname -r")
	sb.WriteString("; echo '@@@'; cat /proc/meminfo 2>/dev/null")
	sb.WriteString("; echo '@@@'; df /data 2>/dev/null")
	sb.WriteString("; echo '@@@'; cat /proc/stat 2>/dev/null")
	out, _ := c.Shell(ctx, serial, sb.String())
	return out
}

// parseCapabilities parses the batched probe output. It is pure (no device I/O)
// so it can be unit-tested and reused.
func parseCapabilities(out string) *Capabilities {
	caps := &Capabilities{Has: map[string]bool{}}
	parts := strings.Split(out, "@@@")
	get := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	caps.SDK, _ = strconv.Atoi(get(0))
	caps.Release = get(1)
	caps.ABI = get(2)
	for _, a := range strings.Split(get(3), ",") {
		if a = strings.TrimSpace(a); a != "" {
			caps.ABIList = append(caps.ABIList, a)
		}
	}
	caps.SELinux = strings.ToLower(get(4))
	caps.Bits64 = strings.Contains(caps.ABI, "64")
	for _, a := range caps.ABIList {
		if strings.Contains(a, "64") {
			caps.Bits64 = true
		}
	}
	for _, b := range strings.Fields(get(5)) {
		caps.Has[b] = true
	}

	// Appended sections. Each is independently optional: an old ROM may not
	// have `uname` or a readable /proc, and a missing one must leave its field
	// zero rather than disturb the others.
	caps.Serial = get(6)
	caps.Model = strings.ReplaceAll(get(7), "_", " ")
	caps.Manufacturer = get(8)
	caps.Product = get(9)
	caps.BuildID = get(10)
	caps.BuildTags = get(11)
	caps.Fingerprint = get(12)
	caps.Hardware = get(13)
	caps.Kernel = firstLine(get(14))
	caps.MemTotalKB = parseMemTotalKB(get(15))
	if total, _, ok := parseDataDF(get(16)); ok {
		caps.StorageTotalKB = total
	}
	_, caps.NCPU = parseProcStat(get(17))

	// "unknown" is what a redacted or unset ro.serialno reads as on some ROMs;
	// carrying it forward would make every such device share one identity.
	if caps.Serial == "unknown" {
		caps.Serial = ""
	}
	return caps
}
