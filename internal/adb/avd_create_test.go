package adb

import (
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
