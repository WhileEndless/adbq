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
	Root           bool   `json:"root"`       // su (or a root adbd) actually ran a command
	RootMethod     string `json:"rootMethod"` // how, or why not (see RootPending)
	// RootPending marks a device that advertises a root manager but has not
	// granted it — typically a Magisk prompt awaiting approval. Root is false:
	// privileged calls will fail until the user grants it.
	RootPending    bool   `json:"rootPending"`
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
	return ParseDeviceList(out), nil
}

// ParseDeviceList turns a device listing into Devices.
//
// Shared by the `adb devices -l` command and by the push tracker (track.go),
// which receives the same line format over the host protocol. One parser
// because the two must agree exactly: if push and poll disagreed about a
// device's identity or transport, the UI would flip between two versions of
// the same phone depending on which path last delivered.
//
// Tolerates the CLI's "List of devices attached" banner, which the protocol
// stream does not send.
func ParseDeviceList(out string) []Device {
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
	return devices
}

// Enrich pulls additional metadata for a single device.
//
// It runs on the device-list poll, so its cost is paid over and over for as
// long as adbq is open. It used to make nine to thirteen separate `adb shell`
// calls per device per poll — one process each, one adb-server connection each
// — and all but a handful were for values that cannot change while a device
// stays connected: the model, the build id, the kernel version.
//
// Now the fixed half comes from the capability scan (one batched round trip,
// cached for the connection's lifetime, dropped by DomProps on reboot) and the
// changing half — root state, addresses, link — comes from a single batched
// probe. Warm, that is one round trip; cold, two.
//
// Best-effort throughout: an offline or dozing device returns empties rather
// than an error, because a half-known device still has to render.
func (c *Client) Enrich(parent context.Context, d *Device) {
	if !d.Online {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()

	// ── Fixed for this connection ────────────────────────────────────────
	caps := c.Capabilities(ctx, d.ID)
	d.HardwareSerial = caps.Serial
	d.AndroidVersion = caps.Release
	d.SDK = caps.SDK
	d.Arch = caps.ABI
	d.BuildID = caps.BuildID
	d.CPU = caps.Hardware
	d.Kernel = caps.Kernel
	// `adb devices -l` already gave us model/product for most transports; the
	// capability scan fills the gap for the ones it does not (notably
	// emulators, which report no model there).
	if d.Model == "" {
		d.Model = caps.Model
	}
	if d.Manufacturer == "" {
		d.Manufacturer = caps.Manufacturer
	}
	if d.Product == "" {
		d.Product = caps.Product
	}

	// ── Changes while connected ──────────────────────────────────────────
	dyn := c.probeDynamic(ctx, d.ID)
	d.Root, d.RootMethod, d.RootPending = c.rootFrom(ctx, d.ID, dyn, caps)

	var link wlanState
	d.IP, link = ipFrom(dyn)
	if ssid, err := c.SSID(ctx, d.ID, link); err == nil {
		d.WiFi = ssid
	}
	d.MAC = firstLine(dyn.mac)

	if d.Label == "" {
		d.Label = d.Model
	}
}

// dynamicProbe is the per-poll half of enrichment: everything that can change
// without the device rebooting.
type dynamicProbe struct {
	id       string // `id` output — uid=0 means the shell is already root
	magisk   string // marker paths that exist
	magiskV  string // `magisk -V` output
	wlanAddr string // `ip -f inet addr show wlan0`
	anyAddr  string // `ip -f inet addr` — fallback when wlan0 has none
	mac      string
}

// probeDynamic reads the whole changing half in one round trip.
//
// The magisk markers are gathered unconditionally even though root detection
// only consults them when `id` is not already uid 0. Asking for them costs
// nothing here — they ride along in a command we are running anyway — whereas
// branching on the answer would cost a second round trip on precisely the
// devices that need one (unrooted, or rooted through su rather than adbd).
func (c *Client) probeDynamic(ctx context.Context, serial string) dynamicProbe {
	const cmd = "id" +
		"; echo '@@@'; ls -d /sbin/.magisk /data/adb/magisk /data/adb/modules 2>/dev/null" +
		"; echo '@@@'; magisk -V 2>/dev/null" +
		"; echo '@@@'; ip -f inet addr show wlan0 2>/dev/null" +
		"; echo '@@@'; ip -f inet addr 2>/dev/null" +
		"; echo '@@@'; cat /sys/class/net/wlan0/address 2>/dev/null"
	out, err := c.Shell(ctx, serial, cmd)
	if err != nil && strings.TrimSpace(out) == "" {
		return dynamicProbe{}
	}
	return parseDynamicProbe(out)
}

// parseDynamicProbe splits the batched output. Pure, so the section handling is
// testable without a device.
func parseDynamicProbe(out string) dynamicProbe {
	parts := strings.Split(out, "@@@")
	get := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	return dynamicProbe{
		id:       get(0),
		magisk:   get(1),
		magiskV:  get(2),
		wlanAddr: get(3),
		anyAddr:  get(4),
		mac:      get(5),
	}
}

// ipFrom picks the address to display and the wlan0 link state that keys the
// Wi-Fi facts, from the batched probe.
//
// The two differ on purpose: the address falls back to any non-loopback
// interface so the UI shows something useful, while the link state comes from
// wlan0 alone. Every caller must derive the link state the same way, or two
// call sites would keep invalidating each other's cached Wi-Fi value.
func ipFrom(dyn dynamicProbe) (string, wlanState) {
	link := wlanStateFromIfaces(parseIfaces(dyn.wlanAddr))
	if ip := link.IP; ip != "" {
		return ip, link
	}
	// Some ROMs' `ip` output does not shape into interface records even though
	// the inet line is there.
	if ip := firstInetAddr(dyn.wlanAddr, false); ip != "" {
		return ip, link
	}
	return firstInetAddr(dyn.anyAddr, true), link
}

// rootFrom decides the device's root status from an already-taken probe.
//
// It reads `id` and the Magisk markers out of the batched dynamic probe rather
// than making its own calls — those three round trips were part of what made
// the device poll expensive. The one call it still may make, suStyleFor, is the
// authoritative test and caches its answer.
func (c *Client) rootFrom(ctx context.Context, serial string, dyn dynamicProbe, caps *Capabilities) (root bool, method string, pending bool) {
	// Shortest path first: on userdebug builds and emulators with `adb root`
	// already applied, the shell uid is 0 — invoking su would needlessly fail
	// on devices that don't ship it.
	if hasUID0(dyn.id) {
		return true, "adb root", false
	}
	// Magisk markers — recorded but not trusted on their own, since the dirs
	// can linger after a hide/uninstall.
	magisk := dyn.magisk != "" || dyn.magiskV != ""
	// Authoritative test: does ANY su form actually run a command as root?
	// suStyleFor probes the simple / sh-wrap / uid-positional forms and caches
	// the winner, so this both decides root status the same way every privileged
	// caller (ShellSU) will, and primes that cache.
	if style, err := c.suStyleFor(ctx, serial); err == nil && style != suUnknown {
		if style == suBareRoot {
			// suStyleFor restarted adbd as root for us (emulator/userdebug).
			return true, "adb root", false
		}
		if magisk {
			return true, "Magisk", false
		}
		// ro.build.tags is fixed for the connection, so it comes from the
		// capability scan rather than a getprop of its own.
		if caps != nil && strings.Contains(caps.BuildTags, "test-keys") {
			return true, "su (test-keys)", false
		}
		return true, "su", false
	}
	// su did not elevate. A Magisk marker means a grant may be pending or denied,
	// which is worth surfacing — but not as Root: reporting rooted here made the
	// UI open root-only actions that then failed on every call, and stock
	// emulator images ship a `magisk` binary, so the marker fired on devices with
	// no usable root at all. It is a separate, weaker signal.
	if magisk {
		return false, "Magisk (grant su)", true
	}
	return false, "", false
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
