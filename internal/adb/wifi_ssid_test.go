package adb

import "testing"

func TestParseCmdWifiStatusSSID(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "connected",
			out: "Wifi is enabled\n" +
				"Wifi is connected to \"example-net\"\n" +
				"WifiInfo: SSID: \"example-net\", BSSID: 02:00:00:00:00:00, RSSI: -55\n" +
				"Wifi scanning is always available\n",
			want: "example-net",
		},
		{
			name: "unquoted name",
			out:  "Wifi is connected to example-net\n",
			want: "example-net",
		},
		{
			name: "name containing spaces",
			out:  "Wifi is connected to \"example net 5\"\n",
			want: "example net 5",
		},
		{
			name: "not connected is a state, not a failure",
			out:  "Wifi is enabled\nWifi is not connected\n",
			want: "",
		},
		{
			name: "radio off",
			out:  "Wifi is disabled\n",
			want: "",
		},
		{
			name: "redacted name",
			out:  "Wifi is connected to <unknown ssid>\n",
			want: "",
		},
		{
			name: "falls back to the connection info line",
			out:  "WifiInfo: SSID: \"example-net\", BSSID: 02:00:00:00:00:00\n",
			want: "example-net",
		},
		{
			name: "empty output",
			out:  "",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCmdWifiStatusSSID(tc.out); got != tc.want {
				t.Errorf("parseCmdWifiStatusSSID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseWifiSSID(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "service dump",
			out:  "mNetworkInfo ...\n  SSID: \"example-net\", BSSID: 02:00:00:00:00:00, MAC: ...\n",
			want: "example-net",
		},
		{
			name: "skips redacted entries and takes the real one",
			out:  "SSID: <unknown ssid>\nSSID: \"example-net\", RSSI: -40\n",
			want: "example-net",
		},
		{
			name: "no ssid anywhere",
			out:  "Wi-Fi is disabled\n",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseWifiSSID(tc.out); got != tc.want {
				t.Errorf("parseWifiSSID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCleanSSID(t *testing.T) {
	tests := []struct{ in, want string }{
		{`"example-net"`, "example-net"},
		{` "example-net", BSSID: 02:00:00:00:00:00`, "example-net"},
		{"<unknown ssid>", ""},
		{"<none>", ""},
		{"null", ""},
		{"   ", ""},
	}
	for _, tc := range tests {
		if got := cleanSSID(tc.in); got != tc.want {
			t.Errorf("cleanSSID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShellRefusedCommand(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{"unknown subcommand", "Unknown command: status\n", true},
		{"denied caller", "Security exception: Uid 2000 does not have access\n", true},
		{"java exception form", "java.lang.SecurityException: no access\n", true},
		{"permission denial", "Permission Denial: cannot run\n", true},
		{"ordinary answer", "Wifi is connected to \"example-net\"\n", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shellRefusedCommand(tc.out); got != tc.want {
				t.Errorf("shellRefusedCommand() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWlanStateFreshKey(t *testing.T) {
	// Distinct states must produce distinct keys, and identical states the same
	// key — that is the whole invalidation contract for costly Wi-Fi probes.
	keys := map[string]wlanState{
		"joined":       {IP: "10.0.0.2", Up: true},
		"other subnet": {IP: "10.0.1.2", Up: true},
		"link down":    {IP: "10.0.0.2", Up: false},
		"no address":   {},
	}
	seen := map[string]string{}
	for name, st := range keys {
		k := st.freshKey()
		if prev, dup := seen[k]; dup {
			t.Errorf("%s and %s share key %q", name, prev, k)
		}
		seen[k] = name
	}
	st := wlanState{IP: "10.0.0.2", Up: true}
	if st.freshKey() != (wlanState{IP: "10.0.0.2", Up: true}).freshKey() {
		t.Error("equal states produced different keys")
	}
}

func TestWlanStateFromIfaces(t *testing.T) {
	tests := []struct {
		name   string
		ifaces []NetIface
		want   wlanState
	}{
		{
			name:   "picks wlan0 regardless of position",
			ifaces: []NetIface{{Name: "lo", IPv4: "127.0.0.1", Up: true}, {Name: "wlan0", IPv4: "10.0.0.2", Up: true}},
			want:   wlanState{IP: "10.0.0.2", Up: true},
		},
		{
			name:   "ignores other interfaces when wlan0 is absent",
			ifaces: []NetIface{{Name: "rmnet0", IPv4: "10.9.9.9", Up: true}},
			want:   wlanState{},
		},
		{
			name:   "wlan0 present but down",
			ifaces: []NetIface{{Name: "wlan0", Up: false}},
			want:   wlanState{},
		},
		{
			name:   "no interfaces",
			ifaces: nil,
			want:   wlanState{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wlanStateFromIfaces(tc.ifaces); got != tc.want {
				t.Errorf("wlanStateFromIfaces() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestSSIDStrategyOrder pins the contract that matters for devices whose Wi-Fi
// service dump is expensive: the cheap path is preferred, and the costly one is
// registered last so it only runs where nothing better exists.
func TestSSIDStrategyOrder(t *testing.T) {
	if len(ssidResolver.strategies) != 2 {
		t.Fatalf("registered strategies = %d, want 2", len(ssidResolver.strategies))
	}
	first, second := ssidResolver.strategies[0], ssidResolver.strategies[1]
	if first.Requires().Costly {
		t.Errorf("%s is preferred but marked Costly", first.Name())
	}
	if first.Requires().MinSDK == 0 {
		t.Errorf("%s has no API-level gate, so it would be tried on every device", first.Name())
	}
	if !second.Requires().Costly {
		t.Errorf("%s is the fallback dump path and must be marked Costly", second.Name())
	}
	if second.Requires().MinSDK != 0 {
		t.Errorf("%s is the last resort and must not be gated out by SDK level", second.Name())
	}
}
