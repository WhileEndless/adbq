package adb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// integrationDevice picks the first online device or skips the test.
func integrationDevice(t *testing.T) (*Client, string) {
	t.Helper()
	if os.Getenv("ADBQ_SKIP_DEVICE") == "1" {
		t.Skip("device tests disabled via ADBQ_SKIP_DEVICE=1")
	}
	c := NewClient()
	if _, err := c.Binary(); err != nil {
		t.Skipf("adb not available: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	devs, err := c.ListDevices(ctx)
	if err != nil {
		t.Skipf("list devices failed: %v", err)
	}
	for _, d := range devs {
		if d.Online {
			return c, d.ID
		}
	}
	t.Skip("no online adb device available")
	return nil, ""
}

func TestDeviceList_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	if !strings.ContainsAny(serial, "0123456789abcdefABCDEF:.") {
		t.Errorf("unexpected serial: %q", serial)
	}
	_ = c
}

func TestEnrich_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	d := Device{ID: serial, Online: true}
	c.Enrich(context.Background(), &d)
	if d.AndroidVersion == "" || d.SDK == 0 {
		t.Errorf("enrich missing core fields: %+v", d)
	}
	t.Logf("enriched: model=%s android=%s sdk=%d root=%v(%s) ip=%s",
		d.Model, d.AndroidVersion, d.SDK, d.Root, d.RootMethod, d.IP)
}

func TestApps_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	apps, err := c.ListApps(ctx, serial, true)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) == 0 {
		t.Skip("no user apps installed")
	}
	t.Logf("got %d user apps (first: %s)", len(apps), apps[0].Pkg)
}

func TestForwards_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.ListForwards(ctx, serial); err != nil {
		t.Fatalf("ListForwards: %v", err)
	}
	if _, err := c.ListReverses(ctx, serial); err != nil {
		t.Fatalf("ListReverses: %v", err)
	}
}

func TestNetwork_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	info, err := c.GetNetworkInfo(ctx, serial)
	if err != nil {
		t.Fatalf("GetNetworkInfo: %v", err)
	}
	if len(info.NetIfaces) == 0 {
		t.Error("expected at least one interface")
	}
}

func TestFiles_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entries, err := c.ListDir(ctx, serial, "/sdcard/", false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	t.Logf("/sdcard listing: %d entries", len(entries))
}

func TestStats_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := c.GetStats(ctx, serial)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if s.MemTotalKB == 0 {
		t.Error("expected MemTotal > 0")
	}
	t.Logf("stats: batt=%d%% temp=%.1fC %dmV mem=%dKB/%dKB cpu=%.1f%% load=%.2f storage=%dKB/%dKB up=%ds rx=%d tx=%d",
		s.BatteryLevel, s.BatteryTemp, s.BatteryVoltage,
		s.MemAvailKB, s.MemTotalKB,
		s.CPUPercent, s.LoadAvg1,
		s.StorageFreeKB, s.StorageTotalKB,
		s.UptimeSeconds, s.NetRxBytes, s.NetTxBytes)
}

func TestIcon_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ic := NewIconCache()
	// Settings is on every Android build.
	uri, err := c.IconFor(ctx, ic, serial, "com.android.settings")
	if err != nil {
		t.Logf("icon extraction returned error (acceptable): %v", err)
		return
	}
	if uri == "" {
		t.Logf("no icon extracted (acceptable on vector-only apps)")
		return
	}
	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Errorf("unexpected data uri prefix: %.40s", uri)
	}
	if len(uri) < 200 {
		t.Errorf("data uri suspiciously small: %d bytes", len(uri))
	}
	t.Logf("icon for com.android.settings: %d bytes data uri", len(uri))
}

func TestShell_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := c.StartShell(ctx, serial, "test", false)
	if err != nil {
		t.Fatalf("StartShell: %v", err)
	}
	defer s.Stop()

	// Send a command that should produce predictable output.
	if err := s.Write("echo ADBQ_OK_$$\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	deadline := time.After(3 * time.Second)
	var buf strings.Builder
	for {
		select {
		case chunk, ok := <-s.Output():
			if !ok {
				t.Fatalf("output closed unexpectedly, got: %q", buf.String())
			}
			buf.Write(chunk)
			if strings.Contains(buf.String(), "ADBQ_OK_") {
				return
			}
		case <-deadline:
			t.Fatalf("did not receive expected echo in 3s, got: %q", buf.String())
		}
	}
}

func TestConnections_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conns, err := c.ListConnections(ctx, serial)
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) == 0 {
		t.Skip("no connections found")
	}
	// at least one TCP listen
	any := false
	for _, ce := range conns {
		if strings.HasPrefix(ce.Proto, "tcp") {
			any = true
			break
		}
	}
	if !any {
		t.Errorf("expected at least one TCP connection")
	}
	t.Logf("connections: %d (first %s %s %s -> %s)", len(conns), conns[0].Proto, conns[0].State, conns[0].LocalAddr, conns[0].RemoteAddr)
}

func TestScreenshot_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := t.TempDir()
	path, err := c.Screenshot(ctx, serial, dir)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	if info.Size() < 1024 {
		t.Errorf("screenshot suspiciously small: %d bytes", info.Size())
	}
	t.Logf("saved screenshot: %s (%d bytes)", path, info.Size())
}

// TestSSIDStaysOffPollingPath_Integration guards the property that makes the
// SSID safe to show on a 5-second poll: while the Wi-Fi link state is unchanged,
// repeated reads must not re-run a Costly strategy. On a device where the cheap
// path applies nothing is cached and the check is vacuous but still valid.
func TestSSIDStaysOffPollingPath_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// readAt reports when a Costly strategy last produced a value, or the zero
	// time when no expensive path is in play on this device.
	readAt := func() time.Time { return costlySSIDReadAt(c, serial) }

	_, link := c.detectIP(ctx, serial)
	first, err := c.SSID(ctx, serial, link)
	if err != nil {
		t.Skipf("no SSID strategy applies to this device: %v", err)
	}
	afterFirst := readAt()

	for i := range 5 {
		got, err := c.SSID(ctx, serial, link)
		if err != nil {
			t.Fatalf("read %d: SSID: %v", i, err)
		}
		if got != first {
			t.Errorf("read %d: SSID = %q, want %q — value must be stable while the link is", i, got, first)
		}
	}
	if afterFirst.IsZero() {
		t.Log("cheap strategy in play; no caching expected")
		return
	}
	if again := readAt(); !again.Equal(afterFirst) {
		t.Errorf("costly strategy re-ran on an unchanged link (%v → %v)", afterFirst, again)
	}

	// An explicit refresh must still reach the device, or the UI could never
	// recover from a stale value.
	if _, err := c.RefreshSSID(ctx, serial, link); err != nil {
		t.Fatalf("RefreshSSID: %v", err)
	}
	if refreshed := readAt(); refreshed.Equal(afterFirst) {
		t.Error("RefreshSSID served the cached value instead of re-reading")
	}

	// Exercise the real polling caller too: Enrich derives the freshness key
	// itself, so a key it computed differently on each pass would defeat the
	// cache without any of the checks above noticing.
	afterRefresh := readAt()
	for i := range 5 {
		d := Device{ID: serial, State: "device", Online: true}
		c.Enrich(ctx, &d)
		if d.WiFi != first {
			t.Errorf("Enrich %d: WiFi = %q, want %q", i, d.WiFi, first)
		}
	}
	if again := readAt(); !again.Equal(afterRefresh) {
		t.Errorf("Enrich re-ran the costly strategy (%v → %v) — the freshness key is not stable across polls", afterRefresh, again)
	}
}

// costlySSIDReadAt reports when a Costly SSID strategy last produced a value for
// serial, or the zero time when the cheap path applies and nothing is cached.
func costlySSIDReadAt(c *Client, serial string) time.Time {
	c.factMu.Lock()
	defer c.factMu.Unlock()
	if st := c.facts[factKey("net.ssid", serial)]; st != nil && st.cached {
		return st.at
	}
	return time.Time{}
}

// TestNetworkSnapshotReadsSSIDFresh_Integration is the counterpart to
// TestSSIDStaysOffPollingPath_Integration: the network snapshot is built on
// demand, not on a timer, so it must read the SSID fresh. Without this the
// Network screen's Refresh button could never recover from a stale name.
func TestNetworkSnapshotReadsSSIDFresh_Integration(t *testing.T) {
	c, serial := integrationDevice(t)
	if serial == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, link := c.detectIP(ctx, serial)
	primed, err := c.SSID(ctx, serial, link)
	if err != nil {
		t.Skipf("no SSID strategy applies to this device: %v", err)
	}
	before := costlySSIDReadAt(c, serial)

	info, err := c.GetNetworkInfo(ctx, serial)
	if err != nil {
		t.Fatalf("GetNetworkInfo: %v", err)
	}
	if info.WiFiSSID != primed {
		t.Errorf("snapshot SSID = %q, poll SSID = %q — the two views disagree", info.WiFiSSID, primed)
	}
	if before.IsZero() {
		t.Log("cheap strategy in play; every read already reaches the device")
		return
	}
	if after := costlySSIDReadAt(c, serial); after.Equal(before) {
		t.Error("network snapshot served the cached SSID instead of re-reading")
	}
}
