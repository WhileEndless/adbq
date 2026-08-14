package adb

import (
	"os"
	"strings"
	"testing"
)

func TestParseThreadtime(t *testing.T) {
	cases := []struct {
		in       string
		ok       bool
		lvl, tag string
		pid      int
	}{
		{"05-22 12:18:04.123  1582 1612 I ActivityManager: Start proc 19842", true, "I", "ActivityManager", 1582},
		{"05-22 12:18:04.123  1582 1612 E OkHttp: Connection reset by peer", true, "E", "OkHttp", 1582},
		{"--------- beginning of system", false, "", "", 0},
	}
	for _, tc := range cases {
		e, ok := parseThreadtime(tc.in)
		if ok != tc.ok {
			t.Fatalf("parseThreadtime(%q) ok=%v want %v", tc.in, ok, tc.ok)
		}
		if !tc.ok {
			continue
		}
		if e.Level != tc.lvl || e.Tag != tc.tag || e.PID != tc.pid {
			t.Errorf("parseThreadtime(%q) got %+v", tc.in, e)
		}
	}
}

func TestParseLsLine(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		name string
		typ  string
		size int64
	}{
		{"-rw-r--r-- 1 shell shell 1298 2026-05-18 09:42 burp-cert.der", true, "burp-cert.der", "file", 1298},
		{"drwxr-xr-x 2 shell shell 4096 2026-05-01 18:12 scripts/", true, "scripts", "dir", 4096},
		{"lrwxrwxrwx 1 root  root  21   2026-05-22 12:08 sdcard -> /storage/self/primary", true, "sdcard", "link", 21},
		// Older toybox (e.g. API-21 emulator) omits the link-count column.
		{"-rwxr-xr-x root root 53489240 2026-06-18 14:37 frida-server-17.14.1-android-arm64", true, "frida-server-17.14.1-android-arm64", "file", 53489240},
		{"drwxr-xr-x root root 4096 2026-06-18 14:37 sub/", true, "sub", "dir", 4096},
		// toolbox `ls -l` on API ≤22 uses the BSD "MMM DD time|year" date format.
		{"-rw-r--r-- 1 root root 1024 Jun 18 14:37 hosts", true, "hosts", "file", 1024},
		{"-rw-r--r-- 1 root root 2048 Jun 18 2024 old.txt", true, "old.txt", "file", 2048},
		{"drwxr-xr-x 5 root root 4096 Jan  5 09:00 data", true, "data", "dir", 4096},
		// BSD format without the link-count column.
		{"-rw-r--r-- root root 512 Mar 3 08:00 note", true, "note", "file", 512},
		{"total 1234", false, "", "", 0},
	}
	for _, tc := range cases {
		e, ok := parseLsLine(tc.in)
		if ok != tc.ok {
			t.Fatalf("parseLsLine(%q) ok=%v want %v", tc.in, ok, tc.ok)
		}
		if !tc.ok {
			continue
		}
		if e.Name != tc.name || e.Type != tc.typ || e.Size != tc.size {
			t.Errorf("parseLsLine(%q) got %+v", tc.in, e)
		}
	}
}

func TestParseForwardList(t *testing.T) {
	out := "abcdef01 tcp:8080 tcp:8080\nabcdef01 tcp:9229 tcp:9229\nemulator-5554 tcp:27042 tcp:27042"
	all := parseForwardList(out, "")
	if len(all) != 3 {
		t.Fatalf("expected 3 forwards, got %d", len(all))
	}
	filt := parseForwardList(out, "abcdef01")
	if len(filt) != 2 {
		t.Fatalf("expected 2 filtered forwards, got %d", len(filt))
	}
}

func TestParseIfaces(t *testing.T) {
	out := `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536
    inet 127.0.0.1/8 scope host lo
2: wlan0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500
    link/ether 9c:6b:00:a1:42:7e brd ff:ff:ff:ff:ff:ff
    inet 192.168.1.42/24 brd 192.168.1.255 scope global wlan0
`
	ifs := parseIfaces(out)
	if len(ifs) < 2 {
		t.Fatalf("expected 2 ifaces, got %d (%+v)", len(ifs), ifs)
	}
	var wlan *NetIface
	for i := range ifs {
		if ifs[i].Name == "wlan0" {
			wlan = &ifs[i]
		}
	}
	if wlan == nil || wlan.IPv4 != "192.168.1.42" || wlan.MAC != "9c:6b:00:a1:42:7e" {
		t.Fatalf("wlan0 parse wrong: %+v", wlan)
	}
}

func TestExtractFridaVersionArch(t *testing.T) {
	v := extractFridaVersion("frida-server-16.4.7-android-arm64")
	if v != "16.4.7" {
		t.Errorf("version got %q", v)
	}
	a := extractFridaArch("frida-server-16.4.7-android-arm64")
	if a != "arm64" {
		t.Errorf("arch got %q", a)
	}
}

func TestPrettyName(t *testing.T) {
	if got := prettyName("com.android.chrome"); got != "Chrome" {
		t.Errorf("prettyName chrome = %q", got)
	}
}

func TestParseBracketList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"[ SYSTEM DEBUGGABLE ALLOW_BACKUP ]", []string{"SYSTEM", "DEBUGGABLE", "ALLOW_BACKUP"}},
		{"[base, config.xxxhdpi, config.en]", []string{"base", "config.xxxhdpi", "config.en"}},
		{"[]", nil},
		{"flags=[ FOO ]", []string{"FOO"}},
	}
	for _, tc := range cases {
		got := parseBracketList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parseBracketList(%q) len=%d want=%d (%v vs %v)", tc.in, len(got), len(tc.want), got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseBracketList(%q)[%d]=%q want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestAndroidVersionForSdk(t *testing.T) {
	if got := AndroidVersionForSdk("33"); got != "Android 13 (Tiramisu)" {
		t.Errorf("sdk 33 = %q", got)
	}
	if got := AndroidVersionForSdk("21"); !strings.HasPrefix(got, "Android 5.0") {
		t.Errorf("sdk 21 = %q", got)
	}
	if got := AndroidVersionForSdk("999"); got != "999" {
		t.Errorf("unknown sdk: %q", got)
	}
	if got := AndroidVersionForSdk("notanumber"); got != "notanumber" {
		t.Errorf("non-numeric: %q", got)
	}
}

func TestDescribeAppParsesNewFields(t *testing.T) {
	// Synthetic dumpsys package fragment covering the new fields we parse.
	out := `Packages:
  Package [com.example.app]:
    userId=10293
    pkg=Package{abc com.example.app}
    codePath=/data/app/~~hash==/com.example.app-1
    resourcePath=/data/app/~~hash==/com.example.app-1
    dataDir=/data/user/0/com.example.app
    primaryCpuAbi=arm64-v8a
    versionCode=1234 minSdk=24 targetSdk=33
    minSdk=24
    targetSdk=33
    compileSdk=34
    versionName=2.5.1
    splits=[base, config.xxxhdpi, config.en]
    installerPackageName=com.android.vending
    installLocation=1
    pkgFlags=[ SYSTEM DEBUGGABLE ]
    privatePkgFlags=[ PRIVATE_FLAG_HAS_DOMAIN_URLS ]
    firstInstallTime=2024-01-12 03:14:00
    lastUpdateTime=2026-02-01 09:33:00
    Signatures: PackageSignatures{abcd version:3, signatures:[deadbeef]}
    requested permissions:
      android.permission.INTERNET
      android.permission.CAMERA
    install permissions:
      android.permission.INTERNET: granted=true
`
	d, err := (&Client{}).describeAppFromText(out, "com.example.app")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if d.CompileSdk != "34" {
		t.Errorf("compileSdk=%q", d.CompileSdk)
	}
	if d.PrimaryAbi != "arm64-v8a" {
		t.Errorf("abi=%q", d.PrimaryAbi)
	}
	if d.InstallLocation != "1" {
		t.Errorf("installLoc=%q", d.InstallLocation)
	}
	if len(d.Splits) != 3 || d.Splits[0] != "base" {
		t.Errorf("splits=%v", d.Splits)
	}
	if len(d.Flags) != 2 || d.Flags[0] != "SYSTEM" {
		t.Errorf("flags=%v", d.Flags)
	}
	if d.Signature == "" {
		t.Errorf("signature missing")
	}
}

func TestDescribeAppRealDumpsys(t *testing.T) {
	b, err := os.ReadFile("testdata/dumpsys_real.txt")
	if err != nil {
		t.Skip("no fixture:", err)
	}
	d, err := (&Client{}).describeAppFromText(string(b), "com.github.uiautomator")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"VersionCode": "2004001",
		"Version":     "2.4.0",
		"MinSdk":      "19",
		"TargetSdk":   "32",
		"UID":         "10366",
	}
	got := map[string]string{
		"VersionCode": d.VersionCode, "Version": d.Version,
		"MinSdk": d.MinSdk, "TargetSdk": d.TargetSdk, "UID": d.UID,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(d.Flags) == 0 {
		t.Errorf("Flags empty")
	}
	if len(d.RequestedPerms) == 0 {
		t.Errorf("RequestedPerms empty")
	}
	if len(d.GrantedPerms) == 0 {
		t.Errorf("GrantedPerms empty")
	}
	if d.DataDir == "" {
		t.Errorf("DataDir empty")
	}
	if d.Path == "" {
		t.Errorf("codePath empty")
	}
	if d.FirstInstall == "" {
		t.Errorf("FirstInstall empty")
	}
	if d.LastUpdate == "" {
		t.Errorf("LastUpdate empty")
	}
	if d.ApkSigningVersion == "" {
		t.Errorf("ApkSigningVersion empty")
	}
	if d.Installed != "true" {
		t.Errorf("Installed=%q want true", d.Installed)
	}
	if d.Enabled == "" {
		t.Errorf("Enabled empty")
	}
	if d.Signature == "" {
		t.Errorf("Signature empty")
	}
	t.Logf("Parsed: minSdk=%s targetSdk=%s perms=%d/%d enabled=%s sigVer=%s",
		d.MinSdk, d.TargetSdk, len(d.GrantedPerms), len(d.RequestedPerms), d.Enabled, d.ApkSigningVersion)
}
