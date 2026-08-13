package adb

import (
	"context"
	"strings"
	"time"
)

// Device represents an attached ADB device with enriched properties.
type Device struct {
	ID             string `json:"id"`
	State          string `json:"state"` // device, offline, unauthorized
	Online         bool   `json:"online"`
	Via            string `json:"via"`       // USB, Wi-Fi, Emulator
	Transport      string `json:"transport"` // transport_id:N
	Label          string `json:"label"`     // user-friendly name (model)
	Model          string `json:"model"`
	Product        string `json:"product"`
	Manufacturer   string `json:"manufacturer"`
	AndroidVersion string `json:"androidVersion"`
	SDK            int    `json:"sdk"`
	BuildID        string `json:"buildId"`
	Kernel         string `json:"kernel"`
	CPU            string `json:"cpu"`
	Arch           string `json:"arch"`
	Root           bool   `json:"root"`
	RootMethod     string `json:"rootMethod"`
	IP             string `json:"ip"`
	WiFi           string `json:"wifi"`
	MAC            string `json:"mac"`
	HardwareSerial string `json:"hardwareSerial"` // ro.serialno — stable across USB↔Wi-Fi
}

// DeviceKey returns a stable per-device identity for profile binding. It prefers
// the hardware serial (ro.serialno), which survives USB↔Wi-Fi/IP changes, and
// falls back to the adb id when that's empty or redacted.
func DeviceKey(d *Device) string {
	if s := strings.TrimSpace(d.HardwareSerial); s != "" && s != "unknown" {
		return s
	}
	return d.ID
}

// ListDevices runs `adb devices -l` and parses the result. Does not enrich.
func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	cmd, err := c.Command(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	out, err := Run(cmd)
	if err != nil {
		return nil, err
	}
	devices := []Device{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "* ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := Device{ID: fields[0], State: fields[1]}
		d.Online = d.State == "device"
		// Determine transport type
		switch {
		case strings.HasPrefix(d.ID, "emulator-"):
			d.Via = "Emulator"
		case strings.Contains(d.ID, ":"):
			d.Via = "Wi-Fi"
		default:
			d.Via = "USB"
		}
		for _, kv := range fields[2:] {
			if i := strings.Index(kv, ":"); i > 0 {
				k, v := kv[:i], kv[i+1:]
				switch k {
				case "product":
					d.Product = v
				case "model":
					d.Model = strings.ReplaceAll(v, "_", " ")
				case "transport_id":
					d.Transport = "transport_id:" + v
				}
			}
		}
		if d.Label == "" {
			d.Label = d.Model
			if d.Label == "" {
				d.Label = d.ID
			}
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// Enrich pulls additional metadata for a single device. Best-effort; ignores
// errors on individual properties (offline devices return mostly empties).
func (c *Client) Enrich(parent context.Context, d *Device) {
	if !d.Online {
		return
	}
	// Identity first, on its own short budget: ro.serialno keys profile binding
	// and must resolve deterministically, not get starved by the slower root/
	// network probes that share the enrichment timeout below.
	sctx, scancel := context.WithTimeout(parent, 3*time.Second)
	if out, err := c.Shell(sctx, d.ID, "getprop ro.serialno"); err == nil {
		d.HardwareSerial = strings.TrimSpace(out)
	}
	scancel()

	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	get := func(key string) string {
		out, err := c.Shell(ctx, d.ID, "getprop "+key)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	if d.Model == "" {
		d.Model = get("ro.product.model")
	}
	if d.Manufacturer == "" {
		d.Manufacturer = get("ro.product.manufacturer")
	}
	if d.Product == "" {
		d.Product = get("ro.product.name")
	}
	// Release, SDK level and ABI come from the capability scan rather than three
	// more getprops: it batches them into one round trip and caches the result
	// for the device's connected lifetime, and none of the three can change
	// while a device stays connected. Enrichment runs on a poll, so every probe
	// it avoids is paid back on every cycle.
	caps := c.Capabilities(ctx, d.ID)
	d.AndroidVersion = caps.Release
	d.SDK = caps.SDK
	d.Arch = caps.ABI
	d.BuildID = get("ro.build.id")
	d.CPU = get("ro.hardware") // generic; not great but stdlib only
	// Kernel
	if k, err := c.Shell(ctx, d.ID, "uname -r"); err == nil {
		d.Kernel = strings.TrimSpace(k)
	}
	// Root detection
	d.Root, d.RootMethod = c.detectRoot(ctx, d.ID)
	// Network
	var link wlanState
	d.IP, link = c.detectIP(ctx, d.ID)
	if ssid, err := c.SSID(ctx, d.ID, link); err == nil {
		d.WiFi = ssid
	}
	if m, err := c.Shell(ctx, d.ID, "cat /sys/class/net/wlan0/address"); err == nil {
		d.MAC = strings.TrimSpace(m)
	}
	if d.Label == "" {
		d.Label = d.Model
	}
}

func (c *Client) detectRoot(ctx context.Context, serial string) (bool, string) {
	// Shortest path first: on userdebug builds and emulators with `adb root`
	// already applied, the shell uid is 0 — invoking su would needlessly
	// fail on devices that don't ship it.
	if id, _ := c.Shell(ctx, serial, "id"); hasUID0(id) {
		return true, "adb root"
	}
	// Magisk markers next (cheap stat) — recorded but not trusted on their own,
	// since the dirs can linger after a hide/uninstall.
	magisk := false
	if out, _ := c.Shell(ctx, serial, "ls -d /sbin/.magisk /data/adb/magisk /data/adb/modules 2>/dev/null"); strings.TrimSpace(out) != "" {
		magisk = true
	} else if out, _ := c.Shell(ctx, serial, "magisk -V 2>/dev/null"); strings.TrimSpace(out) != "" {
		magisk = true
	}
	// Authoritative test: does ANY su form actually run a command as root?
	// suStyleFor probes the simple / sh-wrap / uid-positional forms and caches
	// the winner, so this both decides root status the same way every privileged
	// caller (ShellSU) will, and primes that cache. This replaces the old
	// `which su` probe (which is absent on stripped ROMs) and the simple-form-only
	// check (which mislabeled AOSP-style-su devices as unrooted).
	if style, err := c.suStyleFor(ctx, serial); err == nil && style != suUnknown {
		if magisk {
			return true, "Magisk"
		}
		tags, _ := c.Shell(ctx, serial, "getprop ro.build.tags")
		if strings.Contains(tags, "test-keys") {
			return true, "su (test-keys)"
		}
		return true, "su"
	}
	// su did not elevate. Magisk dirs present means a grant may be pending or
	// denied — report rooted optimistically so the UI lets the user retry after
	// approving the prompt (the negative su probe is intentionally not cached).
	if magisk {
		return true, "Magisk (grant su)"
	}
	return false, ""
}

// detectIP returns the address to display for the device and the wlan0 link
// state that keys its Wi-Fi facts, both from a single `ip` call.
//
// The two differ on purpose: the address falls back to any non-loopback
// interface so the UI shows something useful, while the link state is derived
// from wlan0 through parseIfaces alone. Every caller must key Wi-Fi facts the
// same way, or two call sites would keep invalidating each other's cached value
// (see wlanState).
func (c *Client) detectIP(ctx context.Context, serial string) (string, wlanState) {
	var link wlanState
	ip := ""
	// awk/head are absent on stripped ROMs; pull the raw `ip` output and parse
	// the `inet <addr>/<cidr>` lines host-side instead.
	if out, err := c.Shell(ctx, serial, "ip -f inet addr show wlan0"); err == nil {
		link = wlanStateFromIfaces(parseIfaces(out))
		ip = link.IP
		if ip == "" {
			// Some ROMs' `ip` output doesn't shape into interface records even
			// though the inet line is there.
			ip = firstInetAddr(out, false)
		}
	}
	if ip != "" {
		return ip, link
	}
	// fallback: any non-loopback inet
	if out, err := c.Shell(ctx, serial, "ip -f inet addr"); err == nil {
		if ip := firstInetAddr(out, true); ip != "" {
			return ip, link
		}
	}
	return "", link
}

// firstInetAddr returns the first IPv4 address from `ip ... addr` output,
// stripping any /CIDR suffix. When skipLoopback is true, 127.0.0.1 is ignored.
func firstInetAddr(out string, skipLoopback bool) string {
	for _, raw := range strings.Split(out, "\n") {
		ln := strings.TrimSpace(raw)
		if !strings.HasPrefix(ln, "inet ") {
			continue
		}
		fs := strings.Fields(ln)
		if len(fs) < 2 {
			continue
		}
		ip := fs[1]
		if i := strings.Index(ip, "/"); i > 0 {
			ip = ip[:i]
		}
		if skipLoopback && ip == "127.0.0.1" {
			continue
		}
		if ip != "" {
			return ip
		}
	}
	return ""
}

// parseWifiSSID extracts the connected Wi-Fi SSID from a Wi-Fi service dump,
// host-side (no device `grep`). Returns "" when not present or redacted.
func parseWifiSSID(out string) string {
	for _, ln := range strings.Split(out, "\n") {
		_, after, ok := strings.Cut(ln, "SSID:")
		if !ok {
			continue
		}
		if ssid := cleanSSID(after); ssid != "" {
			return ssid
		}
	}
	return ""
}

// (firstLine and other shared string helpers live in strutil.go)
