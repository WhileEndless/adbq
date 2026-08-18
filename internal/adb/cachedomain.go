package adb

import "strings"

// Cached device state is only safe to keep for minutes if every action that
// changes it says so. adbq is in an unusually good position to guarantee that:
// it is the thing performing the change. It installed the app, it opened the
// forward, it pushed the binary. So invalidation here is not guesswork about
// what the device might have done — it is bookkeeping about what we just did.
//
// The unit of that bookkeeping is a Domain: a coarse area of device state.
// Domains rather than individual cache keys, because there are ~240 bindings
// and a per-key matrix would be unreviewable — and an unreviewable rule is one
// that silently stops being true, which is exactly how this codebase ended up
// caching without invalidating at all.
//
// The set is deliberately small enough to read on one screen. Over-invalidating
// (dropping a whole domain when one key went stale) costs one extra device read;
// under-invalidating shows the user a lie. The trade is not close.
type Domain string

// Per-device domains. A fact cached under a domain MUST be named with that
// domain as its first dotted segment ("net.ssid", "apps.list") — that prefix is
// what InvalidateDomains matches on, so no separate registry can drift out of
// sync with the facts themselves.
const (
	// DomApps covers the installed package list and everything derived from it:
	// per-app details, icons, running state.
	DomApps Domain = "apps"
	// DomStorage covers free space and anything sized from it. Separate from
	// DomApps because pushing a file moves it without touching the app list.
	DomStorage Domain = "storage"
	// DomFiles covers directory listings.
	DomFiles Domain = "files"
	// DomNet covers interfaces, addresses, routes, DNS, Wi-Fi state, sockets.
	DomNet Domain = "net"
	// DomForwards covers `adb forward` / `adb reverse` tables. Host-side state
	// that adbq owns outright.
	DomForwards Domain = "forwards"
	// DomIptables covers the packet filter: backend probe, chains, rules.
	DomIptables Domain = "iptables"
	// DomCerts covers the device trust store.
	DomCerts Domain = "certs"
	// DomHosts covers /system/etc/hosts.
	DomHosts Domain = "hosts"
	// DomFrida covers frida-server presence, version and run state on device.
	DomFrida Domain = "frida"
	// DomTcpdump covers whether a usable tcpdump exists and where.
	DomTcpdump Domain = "tcpdump"
	// DomProps covers build properties and the capability scan — everything
	// treated as fixed for a device's connected lifetime. Only a reboot or a
	// reflash can move it, which is why it is its own domain and rarely touched.
	DomProps Domain = "props"
	// DomRoot covers the su style probe and the root verdict.
	DomRoot Domain = "root"
	// DomProxy covers the global HTTP proxy setting.
	DomProxy Domain = "proxy"
)

// Host-scoped domains describe this computer, not a device, and are invalidated
// with an empty serial.
const (
	// DomSDK covers the Android SDK installation and its package list.
	DomSDK Domain = "sdk"
	// DomJadx covers the jadx installation.
	DomJadx Domain = "jadx"
	// DomScrcpy covers scrcpy availability on the host.
	DomScrcpy Domain = "scrcpy"
	// DomAVD covers the emulator/AVD inventory.
	DomAVD Domain = "avd"
)

// AllDomains is every domain, for the frontend to reason about and for tests to
// assert against. Order is stable so a rendered list does not jump around.
var AllDomains = []Domain{
	DomApps, DomStorage, DomFiles, DomNet, DomForwards, DomIptables,
	DomCerts, DomHosts, DomFrida, DomTcpdump, DomProps, DomRoot, DomProxy,
	DomSDK, DomJadx, DomScrcpy, DomAVD,
}

// DomainReboot is the set a reboot dirties: after one, nothing survives — not
// even the "fixed for the device's lifetime" properties, since the device may
// come back on a different build or newly rooted.
var DomainReboot = AllDomains

// InvalidateDomains drops every cached value in the given domains for serial.
//
// Passing an empty serial invalidates host-scoped state, which is not keyed by
// device. Callers pass whichever applies; a mutation that touches both simply
// calls twice.
func (c *Client) InvalidateDomains(serial string, domains ...Domain) {
	if c == nil || len(domains) == 0 {
		return
	}
	for _, d := range domains {
		switch d {
		case DomProps:
			// The capability scan is the store for lifetime-fixed properties.
			c.InvalidateCapabilities(serial)
		case DomRoot:
			// Root has its own bookkeeping (su style, adb-root state) that a
			// fact prefix does not reach.
			c.ForgetRootProbe(serial)
			// SELinux mode lives in the capability scan and root changes it.
			c.InvalidateCapabilities(serial)
		}
		c.invalidateFactPrefix(serial, string(d)+".")
	}
}

// InvalidateFact drops one remembered fact for serial. InvalidateFacts (all
// facts for a device) is the disconnect hammer; this is the scalpel a targeted
// mutation needs.
func (c *Client) InvalidateFact(serial, fact string) {
	if c == nil {
		return
	}
	c.factMu.Lock()
	defer c.factMu.Unlock()
	delete(c.facts, factKey(fact, serial))
}

// invalidateFactPrefix drops every fact for serial whose name starts with
// prefix. Facts are keyed "<fact>\x00<serial>", so both ends are matched.
func (c *Client) invalidateFactPrefix(serial, prefix string) {
	c.factMu.Lock()
	defer c.factMu.Unlock()
	suffix := "\x00" + serial
	for k := range c.facts {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if strings.HasSuffix(k, suffix) {
			delete(c.facts, k)
		}
	}
}

// DomainOf returns the domain a dotted fact name belongs to, or "" when the
// name has no domain prefix. Used by tests to prove every registered fact is
// reachable by an invalidation.
func DomainOf(fact string) Domain {
	name, _, ok := strings.Cut(fact, ".")
	if !ok {
		return ""
	}
	for _, d := range AllDomains {
		if string(d) == name {
			return d
		}
	}
	return ""
}
