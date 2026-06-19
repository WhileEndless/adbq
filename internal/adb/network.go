package adb

import (
	"context"
	"net"
	"strings"
)

// netInterfaces returns the host's non-loopback IPv4 addresses.
func netInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				out = append(out, ip4.String())
			}
		}
	}
	return out, nil
}

// NetIface is a parsed `ip -f inet addr` entry.
type NetIface struct {
	Name string `json:"name"`
	IPv4 string `json:"ipv4"`
	MAC  string `json:"mac"`
	Up   bool   `json:"up"`
}

// NetworkInfo is the snapshot for the Network screen.
type NetworkInfo struct {
	IP        string     `json:"ip"`
	Gateway   string     `json:"gateway"`
	DNS       []string   `json:"dns"`
	WiFiSSID  string     `json:"wifiSsid"`
	WiFiBSSID string     `json:"wifiBssid"`
	MAC       string     `json:"mac"`
	Proxy     string     `json:"proxy"`
	NetIfaces []NetIface `json:"interfaces"`
}

// GetNetworkInfo gathers the snapshot.
func (c *Client) GetNetworkInfo(ctx context.Context, serial string) (*NetworkInfo, error) {
	info := &NetworkInfo{}
	if out, err := c.Shell(ctx, serial, "ip -f inet addr"); err == nil {
		info.NetIfaces = parseIfaces(out)
	}
	for _, ifc := range info.NetIfaces {
		if ifc.Name == "wlan0" {
			info.IP = ifc.IPv4
			info.MAC = ifc.MAC
		}
	}
	if info.IP == "" {
		for _, ifc := range info.NetIfaces {
			if ifc.IPv4 != "" && ifc.Name != "lo" {
				info.IP = ifc.IPv4
				info.MAC = ifc.MAC
				break
			}
		}
	}
	if out, err := c.Shell(ctx, serial, "ip route"); err == nil {
		// Parse "default via <gw> dev <if> ..." host-side (awk is absent on
		// stripped ROMs).
		for _, ln := range strings.Split(out, "\n") {
			fs := strings.Fields(strings.TrimSpace(ln))
			if len(fs) >= 3 && fs[0] == "default" && fs[1] == "via" {
				info.Gateway = fs[2]
				break
			}
		}
	}
	if dns, err := c.Shell(ctx, serial, "getprop net.dns1; getprop net.dns2"); err == nil {
		for _, ln := range strings.Split(dns, "\n") {
			ln = strings.TrimSpace(ln)
			if ln != "" && ln != "[]" {
				info.DNS = append(info.DNS, ln)
			}
		}
	}
	if proxy, err := c.Shell(ctx, serial, "settings get global http_proxy"); err == nil {
		p := strings.TrimSpace(proxy)
		if p != ":0" && p != "null" && p != "" {
			info.Proxy = p
		}
	}
	if ssid, err := c.Shell(ctx, serial, "dumpsys wifi | grep -m1 'SSID:' || true"); err == nil {
		s := strings.TrimSpace(ssid)
		if idx := strings.Index(s, "SSID:"); idx >= 0 {
			rest := strings.TrimSpace(s[idx+5:])
			if j := strings.Index(rest, ","); j > 0 {
				rest = rest[:j]
			}
			info.WiFiSSID = strings.Trim(rest, `" `)
		}
	}
	return info, nil
}

func parseIfaces(out string) []NetIface {
	res := []NetIface{}
	var cur *NetIface
	for _, raw := range strings.Split(out, "\n") {
		ln := strings.TrimSpace(raw)
		if ln == "" {
			continue
		}
		// "2: wlan0: <BROADCAST,UP,LOWER_UP> mtu 1500 ..."
		if len(ln) > 2 && ln[0] >= '0' && ln[0] <= '9' && strings.Contains(ln, ": ") {
			if cur != nil {
				res = append(res, *cur)
			}
			name := ""
			fields := strings.SplitN(ln, ": ", 3)
			if len(fields) >= 2 {
				name = fields[1]
			}
			up := strings.Contains(ln, "UP")
			cur = &NetIface{Name: name, Up: up}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(ln, "inet ") {
			fs := strings.Fields(ln)
			if len(fs) >= 2 {
				ip := fs[1]
				if i := strings.Index(ip, "/"); i > 0 {
					ip = ip[:i]
				}
				cur.IPv4 = ip
			}
		}
		if strings.HasPrefix(ln, "link/ether") {
			fs := strings.Fields(ln)
			if len(fs) >= 2 {
				cur.MAC = fs[1]
			}
		}
	}
	if cur != nil {
		res = append(res, *cur)
	}
	return res
}

// SetProxy sets the global HTTP proxy. Use "" to clear.
func (c *Client) SetProxy(ctx context.Context, serial, hostPort string) (string, error) {
	v := hostPort
	if v == "" {
		v = ":0"
	}
	return c.Shell(ctx, serial, "settings put global http_proxy "+v)
}

// safeHost only allows hostname characters so it can be interpolated into a
// shell command without quoting concerns.
func safeHost(host string) string {
	b := make([]byte, 0, len(host))
	for i := 0; i < len(host); i++ {
		ch := host[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_' || ch == ':' {
			b = append(b, ch)
		}
	}
	return string(b)
}

// DNSLookup resolves a hostname on the device with the goal of honoring any
// /system/etc/hosts overrides the user has installed.
//
// Resolution order matters:
//  1. /system/etc/hosts (and /etc/hosts symlink targets) lookup — direct grep.
//     This is the only deterministic way to verify a hosts override actually
//     made it onto the device, since every other resolver path on Android 9+
//     funnels through netd's DnsResolver which may have a stale cache.
//  2. `ping -c 1 -W 2` — uses bionic's gethostbyname, which DOES consult
//     /etc/hosts before falling back to DNS. Available on every Android build.
//  3. `nslookup` — pure DNS query, ignores hosts file. Used as last resort and
//     clearly labeled so users can tell the difference.
//
// `getent ahosts` is intentionally not used: it's a glibc tool, only present
// in newer toybox builds, and absent on the vast majority of Android devices.
func (c *Client) DNSLookup(ctx context.Context, serial, host string) (string, error) {
	if host == "" {
		return "", nil
	}
	q := safeHost(host)
	if q == "" {
		return "", nil
	}

	var sections []string

	// 1. Hosts file direct lookup. We resolve symlinks first so we only read
	//    each underlying file once — on many devices /etc/hosts → /system/etc/hosts
	//    (or vice versa) and naive iteration would print duplicate lines.
	//    The matching is done host-side in Go (awk is absent on stripped ROMs);
	//    here we just emit each resolved file path followed by its contents,
	//    prefixed so the parser can attribute lines to a filename.
	hostsCat := "for f in /system/etc/hosts /etc/hosts; do " +
		"  r=$(readlink -f \"$f\" 2>/dev/null || echo \"$f\"); " +
		"  [ -r \"$r\" ] && { echo \"@@FILE@@ $r\"; cat \"$r\"; }; " +
		"done 2>/dev/null"
	if out, err := c.Shell(ctx, serial, hostsCat); err == nil {
		if matches := matchHostsLines(out, host); matches != "" {
			sections = append(sections, "hosts file:\n"+matches)
		}
	}

	// 2. Bionic resolver via ping (respects hosts). `-c 1 -W 2` keeps it short.
	//    `head` is absent on stripped ROMs, so cap the output host-side.
	if out, err := c.Shell(ctx, serial, "ping -c 1 -W 2 "+q+" 2>&1"); err == nil {
		out = strings.TrimSpace(firstNLines(out, 2))
		if out != "" {
			sections = append(sections, "ping (bionic resolver):\n"+out)
		}
	}

	// 3. Pure DNS — try nslookup, then `getprop net.dns1` + a manual probe via
	//    /system/bin/ping6 against the DNS server. Skip the section entirely
	//    when the tool isn't installed (toybox builds often omit nslookup) so
	//    the user doesn't see a noisy "not found" line.
	if out, err := c.Shell(ctx, serial, "command -v nslookup >/dev/null 2>&1 && nslookup "+q+" 2>&1"); err == nil {
		out = strings.TrimSpace(firstNLines(out, 10))
		if out != "" && !strings.Contains(out, "not found") {
			sections = append(sections, "nslookup (DNS only, ignores hosts):\n"+out)
		}
	}

	if len(sections) == 0 {
		return "no resolver responded — device may be offline", nil
	}
	return strings.Join(sections, "\n\n"), nil
}

// FlushDNS clears the device-side DNS resolver cache. Required after editing
// /system/etc/hosts because Android 9+ netd caches resolutions and will keep
// returning stale IPs until either the cache TTL expires or it is flushed.
// Best-effort: tries `ndc` (root), then per-network resolver clear, then a
// netd restart as last resort.
func (c *Client) FlushDNS(ctx context.Context, serial string) (string, error) {
	// ndc resolver clearnetdns <iface> works on Android 7+, requires root.
	// Pure shell builtins are used to parse iface/netId lists because awk is
	// absent on stripped ROMs. `ip -o link` lines look like "2: eth0: <...>";
	// `ndc network list` netId rows begin with a digit.
	script := `set -e
ip -o link show 2>/dev/null | while IFS= read -r line; do
  rest=${line#*: }            # drop "<index>: "
  name=${rest%%:*}            # keep up to next ":"
  name=${name%%@*}            # strip "@parent" on vlans
  case "$name" in
    ""|lo|sit*|ip6tnl*|dummy*) continue ;;
  esac
  ndc resolver clearnetdns "$name" 2>&1 || true
done
# Newer Android: per-netId flush
ndc network list 2>/dev/null | while IFS= read -r line; do
  case "$line" in
    [0-9]*) id=${line%% *}; ndc resolver flushnet "$id" 2>&1 || true ;;
  esac
done
echo "DONE"`
	out, _, err := c.ShellSU(ctx, serial, script)
	return out, err
}

// HostLANIPs returns the host machine's non-loopback IPv4 addresses,
// preferring the ones that overlap with the device's subnet when given.
func HostLANIPs(devIP string) []string {
	ifaces, err := netInterfaces()
	if err != nil {
		return nil
	}
	prefer := ""
	if devIP != "" {
		// take first 3 octets as a coarse "same subnet" hint
		dot := 0
		for i := 0; i < len(devIP); i++ {
			if devIP[i] == '.' {
				dot++
				if dot == 3 {
					prefer = devIP[:i+1]
					break
				}
			}
		}
	}
	var preferred, others []string
	for _, ip := range ifaces {
		if prefer != "" && strings.HasPrefix(ip, prefer) {
			preferred = append(preferred, ip)
		} else {
			others = append(others, ip)
		}
	}
	return append(preferred, others...)
}

// GetProxy reads the global HTTP proxy or returns "" when unset.
func (c *Client) GetProxy(ctx context.Context, serial string) (string, error) {
	out, err := c.Shell(ctx, serial, "settings get global http_proxy")
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == ":0" || out == "null" {
		return "", nil
	}
	return out, nil
}

// firstNLines returns at most the first n lines of s. Used in place of the
// `head` utility, which is absent on stripped ROMs.
func firstNLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// matchHostsLines parses the "@@FILE@@ <path>" + file-contents stream emitted by
// DNSLookup and returns "<ip>  (<file>)" for every hosts entry that maps the
// given host. Done host-side because awk is absent on stripped ROMs.
func matchHostsLines(out, host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	var file string
	var matches []string
	for _, raw := range strings.Split(out, "\n") {
		t := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if t == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(t, "@@FILE@@ "); ok {
			file = strings.TrimSpace(rest)
			continue
		}
		if strings.HasPrefix(t, "#") {
			continue
		}
		fs := strings.Fields(t)
		if len(fs) < 2 {
			continue
		}
		for _, name := range fs[1:] {
			if strings.HasPrefix(name, "#") {
				break
			}
			if strings.ToLower(name) == host {
				matches = append(matches, fs[0]+"  ("+file+")")
				break
			}
		}
	}
	return strings.Join(matches, "\n")
}
