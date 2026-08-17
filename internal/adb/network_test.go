package adb

import (
	"strings"
	"testing"
)

func TestParseIfconfigBusybox(t *testing.T) {
	out := `wlan0     Link encap:Ethernet  HWaddr 02:00:00:11:22:33
          inet addr:192.168.0.5  Bcast:192.168.0.255  Mask:255.255.255.0
          UP BROADCAST RUNNING MULTICAST  MTU:1500  Metric:1

lo        Link encap:Local Loopback
          inet addr:127.0.0.1  Mask:255.0.0.0
          UP LOOPBACK RUNNING  MTU:65536  Metric:1`
	ifs := parseIfconfig(out)
	wlan := findIface(ifs, "wlan0")
	if wlan == nil || wlan.IPv4 != "192.168.0.5" || wlan.MAC != "02:00:00:11:22:33" || !wlan.Up {
		t.Fatalf("busybox wlan0 parse: %+v", wlan)
	}
}

func TestParseIfconfigToybox(t *testing.T) {
	out := `wlan0: flags=4163<UP,BROADCAST,RUNNING,MULTICAST> mtu 1500
        inet 10.0.2.16  netmask 255.255.255.0  broadcast 10.0.2.255
        ether 52:54:00:12:34:56  txqueuelen 1000  (Ethernet)`
	ifs := parseIfconfig(out)
	wlan := findIface(ifs, "wlan0")
	if wlan == nil || wlan.IPv4 != "10.0.2.16" || wlan.MAC != "52:54:00:12:34:56" || !wlan.Up {
		t.Fatalf("toybox wlan0 parse: %+v", wlan)
	}
}

func TestParseNetcfg(t *testing.T) {
	out := `lo       UP    127.0.0.1/8    0x00000049 00:00:00:00:00:00
wlan0    UP    192.168.0.5/24 0x00001043 02:00:00:11:22:33
rmnet0   DOWN  0.0.0.0/0      0x00000000 00:00:00:00:00:00`
	ifs := parseNetcfg(out)
	wlan := findIface(ifs, "wlan0")
	if wlan == nil || wlan.IPv4 != "192.168.0.5" || wlan.MAC != "02:00:00:11:22:33" || !wlan.Up {
		t.Fatalf("netcfg wlan0 parse: %+v", wlan)
	}
	if rm := findIface(ifs, "rmnet0"); rm == nil || rm.IPv4 != "" || rm.Up {
		t.Fatalf("netcfg rmnet0 (down, 0.0.0.0): %+v", rm)
	}
}

func TestParseProcRouteGateway(t *testing.T) {
	// wlan0 default route, gateway 192.168.2.1 (little-endian 0102A8C0).
	out := `Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask	MTU	Window	IRTT
wlan0	00000000	0102A8C0	0003	0	0	0	00000000	0	0	0
wlan0	0002A8C0	00000000	0001	0	0	0	00FFFFFF	0	0	0`
	if gw := parseProcRouteGateway(out); gw != "192.168.2.1" {
		t.Fatalf("gateway = %q, want 192.168.2.1", gw)
	}
}

func findIface(ifs []NetIface, name string) *NetIface {
	for i := range ifs {
		if ifs[i].Name == name {
			return &ifs[i]
		}
	}
	return nil
}

// The proxy command used to be assembled in the UI, which is how the displayed
// line and the executed one could disagree. Both now come from here.
func TestProxyCommandMatchesWhatSetProxyRuns(t *testing.T) {
	if got := ProxyCommand("SER", "10.0.0.5:8080"); got != "adb -s SER shell settings put global http_proxy 10.0.0.5:8080" {
		t.Errorf("ProxyCommand = %q", got)
	}
	// Clearing is not a delete: the framework reads :0 as "no proxy".
	for _, empty := range []string{"", "   "} {
		if got := ProxyCommand("SER", empty); !strings.HasSuffix(got, "http_proxy :0") {
			t.Errorf("ProxyCommand(%q) = %q, want it to clear with :0", empty, got)
		}
	}
	if proxyValue("1.2.3.4:9") != "1.2.3.4:9" || proxyValue("") != ":0" {
		t.Error("proxyValue disagrees with the rendered command")
	}
}
