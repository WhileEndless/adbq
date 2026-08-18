package adb

import (
	"context"
	"strings"
)

// wlanState is cheap, side-effect-free evidence of the device's Wi-Fi link: the
// address it holds and whether the link is up. It keys the freshness of every
// Wi-Fi fact — when the device joins, leaves, or switches a network, at least
// one of the two moves, so an expensive probe can back a value shown on a
// polling path without going stale (see Requirements.Costly).
//
// It is deliberately derived from wlan0 alone, never from a fallback interface:
// every call site must produce the same key for the same device state, or they
// would invalidate each other's cached value on every poll.
type wlanState struct {
	IP string
	Up bool
}

// freshKey renders the state as a Resolver freshness key. The empty state gives
// a stable key of its own, so "no Wi-Fi right now" is cached rather than
// re-probed.
func (w wlanState) freshKey() string {
	if w.Up {
		return w.IP + "|up"
	}
	return w.IP + "|down"
}

// wlanStateFromIfaces reads the link state off an already-parsed interface list,
// so callers that have one pay nothing extra for it.
func wlanStateFromIfaces(ifaces []NetIface) wlanState {
	for _, ifc := range ifaces {
		if ifc.Name == "wlan0" {
			return wlanState{IP: ifc.IPv4, Up: ifc.Up}
		}
	}
	return wlanState{}
}

// ssidResolver routes the "which network is this device on" question to the
// cheapest command the device's API level offers.
// The fact name is domain-prefixed ("net.") so InvalidateDomains(DomNet)
// reaches it — see cachedomain.go. A bare name would be cached forever by
// any invalidation short of dropping the whole device.
var ssidResolver = NewResolver[string]("net.ssid",
	ssidViaWifiShell{},
	ssidViaDumpsysWifi{},
)

// ssidViaWifiShell asks the Wi-Fi shell service, which exists from API 30. It
// answers in a few lines — far cheaper than dumping the whole Wi-Fi service —
// but it is still a round trip to a binder service, and device enrichment runs
// on a poll.
type ssidViaWifiShell struct{}

func (ssidViaWifiShell) Name() string { return "cmd-wifi-status" }

func (ssidViaWifiShell) Requires() Requirements {
	// Costly is about "must not run on every poll", not about how heavy the
	// command is. Left un-Costly, this ran on every device-list refresh — an
	// adb process every few seconds to re-read a value that only moves when the
	// Wi-Fi link does, which is precisely what the freshness key already
	// tracks. The dump-based fallback below has always been marked this way.
	return Requirements{MinSDK: 30, Bins: []string{"cmd"}, Costly: true}
}

func (s ssidViaWifiShell) Run(ctx context.Context, c *Client, serial string) (string, error) {
	out, err := c.Shell(ctx, serial, "cmd wifi status")
	// Some builds restrict the Wi-Fi shell service to root and some drop the
	// subcommand entirely; both report through the output, and on some ROMs with
	// a zero exit status. Treat either as permanent so the device settles onto
	// the older path after one probe.
	if shellRefusedCommand(out) {
		return "", ErrUnsupported
	}
	if err != nil {
		return "", err
	}
	return parseCmdWifiStatusSSID(out), nil
}

// ssidViaDumpsysWifi is the path for devices below API 30, where the full
// Wi-Fi service dump is the only source that authoritatively names the network
// the device is currently joined to. It is marked Costly: the dump is orders of
// magnitude larger than the other probes and on some builds has side effects on
// the device (the Wi-Fi stack writes diagnostics to internal storage on every
// call, in a location nothing prunes), so it must never run on a polling path.
type ssidViaDumpsysWifi struct{}

func (ssidViaDumpsysWifi) Name() string { return "dumpsys-wifi" }

func (ssidViaDumpsysWifi) Requires() Requirements {
	return Requirements{Costly: true}
}

func (ssidViaDumpsysWifi) Run(ctx context.Context, c *Client, serial string) (string, error) {
	out, err := c.Shell(ctx, serial, "dumpsys wifi")
	if err != nil {
		return "", err
	}
	return parseWifiSSID(out), nil
}

// SSID returns the name of the network the device is joined to, or "" when it is
// not on Wi-Fi or the name is redacted. link is the caller's already-known Wi-Fi
// link state, which decides whether a cached value still applies.
func (c *Client) SSID(ctx context.Context, serial string, link wlanState) (string, error) {
	return ssidResolver.Resolve(ctx, c, serial, link.freshKey())
}

// RefreshSSID re-reads the SSID even when the link state has not moved, and
// updates what later SSID calls will serve. Use it for on-demand reads — a
// screen the user opened, a refresh they asked for — and SSID everywhere that
// repeats on a timer, so an expensive probe never lands on a polling path.
func (c *Client) RefreshSSID(ctx context.Context, serial string, link wlanState) (string, error) {
	ssidResolver.Invalidate(c, serial)
	return c.SSID(ctx, serial, link)
}

// parseCmdWifiStatusSSID pulls the network name out of the Wi-Fi shell status
// report. "Wi-Fi is off" and "not connected" are ordinary states, not failures,
// and yield "".
func parseCmdWifiStatusSSID(out string) string {
	const connectedTo = "is connected to "
	for _, raw := range strings.Split(out, "\n") {
		ln := strings.TrimSpace(raw)
		_, after, ok := strings.Cut(ln, connectedTo)
		if !ok {
			continue
		}
		if ssid := cleanSSID(after); ssid != "" {
			return ssid
		}
	}
	// Newer releases also print the full connection info on one line; it carries
	// the same name in the shape the service dump uses.
	return parseWifiSSID(out)
}

// cleanSSID trims the quoting and placeholder forms the platform uses for a
// network name it cannot report.
func cleanSSID(s string) string {
	s = strings.TrimSpace(s)
	if before, _, ok := strings.Cut(s, ","); ok {
		s = before
	}
	s = strings.Trim(s, `" `)
	switch s {
	case "", "<unknown ssid>", "<none>", "null":
		return ""
	}
	return s
}

// shellRefusedCommand reports whether output is the platform refusing a shell
// command outright — an unknown subcommand or a denied caller — rather than an
// answer. Such a refusal is a property of the build, so it is permanent.
func shellRefusedCommand(out string) bool {
	for _, marker := range []string{
		"Unknown command",
		"Security exception",
		"SecurityException",
		"Permission Denial",
		"Exception occurred while executing",
	} {
		if strings.Contains(out, marker) {
			return true
		}
	}
	return false
}
