package adb

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateAVDArgs(t *testing.T) {
	cases := []struct {
		name string
		spec AVDSpec
		want []string
	}{
		{
			name: "minimum",
			spec: AVDSpec{Name: "Test", Pkg: "system-images;android-34;google_apis;arm64-v8a"},
			want: []string{"--silent", "create", "avd", "-n", "Test", "-k", "system-images;android-34;google_apis;arm64-v8a"},
		},
		{
			name: "with device and sdcard",
			spec: AVDSpec{Name: "T", Pkg: "system-images;android-33;google_apis;x86_64", Device: "pixel_8", SDCard: "512M"},
			want: []string{"--silent", "create", "avd", "-n", "T", "-k", "system-images;android-33;google_apis;x86_64", "-d", "pixel_8", "-c", "512M"},
		},
		{
			name: "force overwrites",
			spec: AVDSpec{Name: "T", Pkg: "system-images;android-33;google_apis;x86_64", Force: true},
			want: []string{"--silent", "create", "avd", "-n", "T", "-k", "system-images;android-33;google_apis;x86_64", "--force"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CreateAVDArgs(tc.spec)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("CreateAVDArgs()\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

// config.ini-only settings must not leak into the command line as flags
// avdmanager doesn't have.
func TestCreateAVDArgsIgnoresConfigOnlyFields(t *testing.T) {
	got := strings.Join(CreateAVDArgs(AVDSpec{
		Name: "T", Pkg: "system-images;android-34;google_apis;arm64-v8a",
		RAMMB: 4096, Cores: 4, DataSize: "8G", Keyboard: true, GPUMode: "host",
	}), " ")
	for _, bad := range []string{"4096", "-cores", "8G", "hw.keyboard", "host"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q must not reach the avdmanager command line: %s", bad, got)
		}
	}
}

func TestAVDNameValidation(t *testing.T) {
	good := []string{"Pixel_8", "Medium_Phone_API_36.1", "a", "test-avd-1"}
	for _, n := range good {
		if !avdNameOK(n) {
			t.Errorf("avdNameOK(%q) = false, want true", n)
		}
	}
	bad := []string{"", "with space", "semi;colon", "quote'", "back`tick", "slash/es", "dollar$", strings.Repeat("x", 101)}
	for _, n := range bad {
		if avdNameOK(n) {
			t.Errorf("avdNameOK(%q) = true, want false", n)
		}
	}
}

func TestDeleteAVDCommandIsQuoted(t *testing.T) {
	got := DeleteAVDCommand("/path with space/avdmanager", "Pixel_8")
	if !strings.Contains(got, "'/path with space/avdmanager'") {
		t.Errorf("binary path must be quoted: %s", got)
	}
	if !strings.HasSuffix(got, "delete avd -n Pixel_8") {
		t.Errorf("unexpected command: %s", got)
	}
}

const deviceListSample = `Available devices definitions:
id: 0 or "ai_glasses_device"
    Name: AI Glasses
    OEM : Google
    Tag : ai-glasses
---------
id: 1 or "automotive_1024p_landscape"
    Name: Automotive (1024p landscape)
    OEM : Google
    Tag : android-automotive-playstore
---------
id: 32 or "pixel_8"
    Name: Pixel 8
    OEM : Google
---------
`

func TestParseDeviceProfiles(t *testing.T) {
	got := parseDeviceProfiles(deviceListSample)
	if len(got) != 3 {
		t.Fatalf("parsed %d profiles, want 3: %+v", len(got), got)
	}
	if got[0].ID != "ai_glasses_device" || got[0].Name != "AI Glasses" || got[0].OEM != "Google" {
		t.Errorf("first profile wrong: %+v", got[0])
	}
	if got[2].ID != "pixel_8" || got[2].Name != "Pixel 8" {
		t.Errorf("a profile without a Tag line must still parse: %+v", got[2])
	}
	// The numeric index shifts whenever Google adds a device, so only the
	// string id may ever be used as a reference.
	for _, p := range got {
		if p.ID == "" || p.ID == "0" || p.ID == "1" || p.ID == "32" {
			t.Errorf("profile id must be the quoted string id, got %q", p.ID)
		}
	}
}

func TestParseDeviceProfilesOnEmptyOutput(t *testing.T) {
	got := parseDeviceProfiles("")
	if got == nil || len(got) != 0 {
		t.Errorf("want an empty non-nil slice, got %#v", got)
	}
}

func TestAVDManagerErrorMapping(t *testing.T) {
	cases := []struct{ out, want string }{
		{"Error: Package path is not valid. Valid system image paths are:", "not installed"},
		{"Error: AVD 'Test' already exists.", "already exists"},
		{"Error: No device found matching --device 'nope'.", "device profile"},
		{"ERROR: JAVA_HOME is not set and no 'java' command could be found", "Java runtime"},
	}
	for _, tc := range cases {
		got := avdManagerError(tc.out, errTest)
		if !strings.Contains(got, tc.want) {
			t.Errorf("avdManagerError(%q) = %q, want it to mention %q", tc.out, got, tc.want)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func TestAVDHardwareChanges(t *testing.T) {
	cases := []struct {
		name string
		hw   AVDHardware
		want map[string]string
	}{
		{
			name: "empty edit changes nothing",
			hw:   AVDHardware{},
			want: map[string]string{},
		},
		{
			name: "cpu and memory",
			hw:   AVDHardware{RAMMB: 4096, Cores: 4},
			want: map[string]string{"hw.ramSize": "4096", "hw.cpu.ncore": "4"},
		},
		{
			name: "sd card also enables the sd card",
			hw:   AVDHardware{SDCard: "2G"},
			want: map[string]string{"sdcard.size": "2G", "hw.sdCard": "yes"},
		},
		{
			name: "gpu off disables rather than enabling with mode off",
			hw:   AVDHardware{GPUMode: "off"},
			want: map[string]string{"hw.gpu.mode": "off", "hw.gpu.enabled": "no"},
		},
		{
			name: "gpu host",
			hw:   AVDHardware{GPUMode: "host"},
			want: map[string]string{"hw.gpu.mode": "host", "hw.gpu.enabled": "yes"},
		},
		{
			name: "resolution and density",
			hw:   AVDHardware{Width: 1080, Height: 2400, Density: 420},
			want: map[string]string{"hw.lcd.width": "1080", "hw.lcd.height": "2400", "hw.lcd.density": "420"},
		},
		{
			// A pointer is what makes "turn the keyboard off" expressible; a
			// plain bool would be indistinguishable from "not edited".
			name: "keyboard can be turned off explicitly",
			hw:   AVDHardware{Keyboard: boolPtr(false)},
			want: map[string]string{"hw.keyboard": "no"},
		},
		{
			name: "keyboard on",
			hw:   AVDHardware{Keyboard: boolPtr(true)},
			want: map[string]string{"hw.keyboard": "yes"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AVDHardwareChanges(tc.hw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("changes = %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q (full: %v)", k, got[k], v, got)
				}
			}
		})
	}
}

// A bad value here produces an emulator that refuses to boot with an opaque
// error, so it must be caught before it reaches config.ini.
func TestAVDHardwareChangesRejectsBadValues(t *testing.T) {
	bad := []struct {
		name string
		hw   AVDHardware
	}{
		{"ram too small to boot", AVDHardware{RAMMB: 128}},
		{"ram implausibly large", AVDHardware{RAMMB: 999999}},
		{"zero-ish core count", AVDHardware{Cores: -1}},
		{"too many cores", AVDHardware{Cores: 64}},
		{"unparseable data size", AVDHardware{DataSize: "8 gigs"}},
		{"unparseable sdcard size", AVDHardware{SDCard: "big"}},
		{"unknown gpu mode", AVDHardware{GPUMode: "metal"}},
		{"width without height", AVDHardware{Width: 1080}},
		{"height without width", AVDHardware{Height: 2400}},
		{"absurd resolution", AVDHardware{Width: 10, Height: 10}},
		{"density out of range", AVDHardware{Density: 5}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AVDHardwareChanges(tc.hw); err == nil {
				t.Errorf("AVDHardwareChanges(%+v) must fail", tc.hw)
			}
		})
	}
}

func TestAVDSizeOK(t *testing.T) {
	for _, good := range []string{"", "512M", "10G", "2048", "800k"} {
		if !avdSizeOK(good) {
			t.Errorf("avdSizeOK(%q) = false, want true", good)
		}
	}
	for _, bad := range []string{"M", "-1G", "abc", "1.5G", "10 G"} {
		if avdSizeOK(bad) {
			t.Errorf("avdSizeOK(%q) = true, want false", bad)
		}
	}
}

// An edit must touch only the keys it was asked to, leaving the rest of a
// config.ini written by Android Studio intact.
func TestWriteIniPreservesUnrelatedKeys(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.ini")
	original := map[string]string{
		"AvdId": "Test", "hw.ramSize": "2048", "skin.name": "pixel_8",
		"image.sysdir.1": "system-images/android-34/google_apis/arm64-v8a/",
	}
	if err := writeIni(p, original); err != nil {
		t.Fatal(err)
	}
	cfg, err := readIni(p)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := AVDHardwareChanges(AVDHardware{RAMMB: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range changes {
		cfg[k] = v
	}
	if err := writeIni(p, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := readIni(p)
	if err != nil {
		t.Fatal(err)
	}
	if got["hw.ramSize"] != "4096" {
		t.Errorf("hw.ramSize = %q, want 4096", got["hw.ramSize"])
	}
	for _, k := range []string{"AvdId", "skin.name", "image.sysdir.1"} {
		if got[k] != original[k] {
			t.Errorf("%s was lost or changed: %q, want %q", k, got[k], original[k])
		}
	}
}
