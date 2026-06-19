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

	c.capMu.Lock()
	if c.caps == nil {
		c.caps = map[string]*Capabilities{}
	}
	c.caps[serial] = caps
	c.capMu.Unlock()
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
	return caps
}
