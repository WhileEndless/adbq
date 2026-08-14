package adb

import (
	"strings"
	"testing"
)

// A clean launch writes nothing to the log — treating that as a failure would
// break every successful start, so it is the case worth pinning down first.
func TestFridaStartFailureSilentLogIsSuccess(t *testing.T) {
	for _, logOut := range []string{"", "   ", "\n\n"} {
		if msg := fridaStartFailure(logOut, "/data/local/tmp/frida-server"); msg != "" {
			t.Errorf("fridaStartFailure(%q) = %q, want empty", logOut, msg)
		}
	}
}

func TestFridaStartFailureClassifies(t *testing.T) {
	cases := []struct {
		name   string
		logOut string
		want   string // substring the message must carry
	}{
		{
			name:   "selinux exec denial",
			logOut: "avc:  denied  { execute } for  pid=123 comm=\"sh\" path=\"/data/local/tmp/frida-server\"",
			want:   "SELinux",
		},
		{
			name:   "denied executing without the avc prefix",
			logOut: "sh: can't execute: permission denied (execute)",
			want:   "SELinux",
		},
		{
			name:   "port taken",
			logOut: "Unable to start: Address already in use",
			want:   "port is already taken",
		},
		{
			name:   "wrong architecture",
			logOut: "sh: /data/local/tmp/frida-server: Exec format error",
			want:   "device architecture",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := fridaStartFailure(tc.logOut, "/data/local/tmp/frida-server")
			if msg == "" {
				t.Fatalf("expected a failure message for %q", tc.logOut)
			}
			if !strings.Contains(msg, tc.want) {
				t.Errorf("message %q does not mention %q", msg, tc.want)
			}
		})
	}
}

// A fault the server survives must NOT be reported as a failed start: the
// server is up, the UI is about to see it as active, and calling that a failure
// would contradict itself. This is the Android 5 case, where frida-server runs
// but its agent cannot map that ART layout — it belongs in the log panel, not
// in a start error.
func TestFridaStartFailureIgnoresSurvivableAgentFault(t *testing.T) {
	logOut := `{"type":"error","description":"Error: Unable to find fields in java/lang/Thread; please file a bug"}`
	if msg := fridaStartFailure(logOut, "/data/local/tmp/frida-server"); msg != "" {
		t.Errorf("agent fault reported as a launch failure: %q", msg)
	}
}

// The log lives in the directory ListFridaServers globs, and that glob matches
// on the substring "frida-server" — so a log named after it would show up as a
// launchable binary in the UI.
func TestFridaServerLogPathIsNotMistakenForAServer(t *testing.T) {
	for _, port := range []int{0, 27042, 27043} {
		if p := fridaServerLogPath(port); strings.Contains(p, "frida-server") {
			t.Errorf("%s would be picked up by the frida-server inventory glob", p)
		}
	}
}

// Two frida-servers can run on one device at once — on different ports, since
// they cannot share one. A single shared log would have each launch truncate the
// other's diagnostics and then interleave their output, so the path is keyed by
// port, with an unset port meaning frida's default.
func TestFridaServerLogPathIsPerPort(t *testing.T) {
	if a, b := fridaServerLogPath(27042), fridaServerLogPath(27043); a == b {
		t.Errorf("servers on different ports share the log path %s", a)
	}
	if a, b := fridaServerLogPath(0), fridaServerLogPath(FridaDefaultPort); a != b {
		t.Errorf("unset port %s should resolve to the default port's log %s", a, b)
	}
}
