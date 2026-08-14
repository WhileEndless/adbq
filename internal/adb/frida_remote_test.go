package adb

import "testing"

// `adb forward tcp:0` prints the host port it allocated; letting adb choose is
// what keeps concurrent sessions (and several devices) from colliding on one.
func TestParseForwardedPort(t *testing.T) {
	cases := []struct {
		name, in string
		want     int
		wantErr  bool
	}{
		{"bare port", "59242", 59242, false},
		{"trailing newline", "59242\n", 59242, false},
		{"leading blank line", "\n59242\n", 59242, false},
		{"crlf", "59242\r\n", 59242, false},
		{"nothing reported", "", 0, true},
		{"not a port", "OK\n", 0, true},
		{"out of range", "70000\n", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseForwardedPort(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseForwardedPort(%q) = %d, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseForwardedPort(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseForwardedPort(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
