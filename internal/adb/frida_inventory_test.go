package adb

import "testing"

func srv(name string) FridaServer {
	return FridaServer{Name: name, Path: fridaServerDir + "/" + name}
}

// The shell drops the NULs separating argv, so a command line arrives as one
// run-together blob. The port has to be recovered from that, not from an
// assumption — the UI used to report 27042 for every running server regardless
// of what it was launched with, and the session path silently depends on it.
func TestPortFromCmdline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"default when no -l", "/data/local/tmp/frida-server-17.5.1-android-arm64", FridaDefaultPort},
		{"nuls collapsed", "/data/local/tmp/frida-server-17.5.1-android-arm64-l0.0.0.0:27042-D", 27042},
		{"non-default port", "/data/local/tmp/frida-server-17.5.1-android-arm64-l0.0.0.0:27043-D", 27043},
		{"loopback bind", "/data/local/tmp/frida-server-l127.0.0.1:1337-D", 1337},
		{"spaced argv", "/data/local/tmp/frida-server -l 0.0.0.0:5555 -D", 5555},
		{"empty", "", FridaDefaultPort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := portFromCmdline(tc.in); got != tc.want {
				t.Errorf("portFromCmdline(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// The exe link is authoritative when root makes it readable.
func TestMarkRunningServersPrefersExe(t *testing.T) {
	servers := []FridaServer{srv("frida-server-17.5.1-android-arm64"), srv("frida-server-17.5.2-android-arm64")}
	markRunningServers(servers, []fridaProc{{
		pid: 42, comm: "frida-server-17", port: 27042,
		exe: fridaServerDir + "/frida-server-17.5.2-android-arm64",
	}})
	if servers[0].Active {
		t.Error("17.5.1 marked active, but 17.5.2 is the one running")
	}
	if !servers[1].Active || servers[1].PID != 42 || servers[1].Port != 27042 {
		t.Errorf("17.5.2 not identified: %+v", servers[1])
	}
}

// Without root the exe link is unreadable, leaving the command line. Matching it
// as if it were a bare path fails (the arguments are glued on), and falling all
// the way back to the 15-char comm marks BOTH 17.5.x builds active off one
// process — which is what the UI used to show.
func TestMarkRunningServersUsesCmdlineWhenExeHidden(t *testing.T) {
	servers := []FridaServer{srv("frida-server-17.5.1-android-arm64"), srv("frida-server-17.5.2-android-arm64")}
	markRunningServers(servers, []fridaProc{{
		pid: 7, comm: "frida-server-17", port: 27043,
		cmd0: fridaServerDir + "/frida-server-17.5.1-android-arm64-l0.0.0.0:27043-D",
	}})
	if !servers[0].Active || servers[0].PID != 7 || servers[0].Port != 27043 {
		t.Errorf("17.5.1 not identified from the command line: %+v", servers[0])
	}
	if servers[1].Active {
		t.Error("17.5.2 also marked active from a single process")
	}
}

// One installed name can extend another, so the longest matching path wins.
func TestMarkRunningServersPrefersLongestPathMatch(t *testing.T) {
	servers := []FridaServer{srv("frida-server-17.5.1-android-arm64"), srv("frida-server-17.5.1-android-arm64-copy")}
	markRunningServers(servers, []fridaProc{{
		pid: 9, comm: "frida-server-17", port: FridaDefaultPort,
		cmd0: fridaServerDir + "/frida-server-17.5.1-android-arm64-copy-D",
	}})
	if servers[0].Active {
		t.Error("shorter-named binary matched a longer binary's command line")
	}
	if !servers[1].Active {
		t.Error("the -copy binary that is actually running was not identified")
	}
}

// With neither exe nor cmdline readable, a truncated comm that fits several
// installs is genuinely ambiguous. Say so instead of picking one or claiming
// both are running.
func TestMarkRunningServersReportsAmbiguity(t *testing.T) {
	servers := []FridaServer{srv("frida-server-17.5.1-android-arm64"), srv("frida-server-17.5.2-android-arm64")}
	markRunningServers(servers, []fridaProc{{pid: 3, comm: "frida-server-17", port: FridaDefaultPort}})
	for _, s := range servers {
		if s.Active {
			t.Errorf("%s claimed active on ambiguous evidence", s.Name)
		}
		if !s.Ambiguous {
			t.Errorf("%s should be flagged ambiguous", s.Name)
		}
	}
}

// A truncated comm matching exactly one install is not ambiguous.
func TestMarkRunningServersAcceptsUniqueComm(t *testing.T) {
	servers := []FridaServer{srv("frida-server-17.5.1-android-arm64"), srv("frida-server-16.1.4-android-arm64")}
	markRunningServers(servers, []fridaProc{{pid: 5, comm: "frida-server-16", port: FridaDefaultPort}})
	if !servers[1].Active || servers[1].PID != 5 {
		t.Errorf("unique comm match not accepted: %+v", servers[1])
	}
	if servers[0].Active || servers[0].Ambiguous {
		t.Errorf("unrelated binary touched: %+v", servers[0])
	}
}

// Download artifacts sit next to the binaries and match the same glob. Offering
// one as a startable server only ever produced a confusing failure.
func TestIsRunnableServer(t *testing.T) {
	cases := []struct {
		name, perms string
		want        bool
	}{
		{"frida-server-16.0.8-android-arm64", "-rwxr-xr-x", true},
		{"frida-server-16.0.8-android-arm64.xz", "-rwxr-xr-x", false},
		{"frida-server-16.0.8-android-arm64.tar.gz", "-rwxr-xr-x", false},
		{"frida-server-16.0.8-android-arm64", "-rw-r--r--", false},
		{"frida-server", "-rwx------", true},
	}
	for _, tc := range cases {
		if got := isRunnableServer(tc.name, tc.perms); got != tc.want {
			t.Errorf("isRunnableServer(%q, %q) = %v, want %v", tc.name, tc.perms, got, tc.want)
		}
	}
}
