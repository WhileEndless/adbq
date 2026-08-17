package adb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIniTolerance(t *testing.T) {
	in := []byte("# a comment\n" +
		"; another\n" +
		"AvdId=Pixel_8\r\n" +
		"  spaced.key  =  spaced value  \n" +
		"garbage-line-without-equals\n" +
		"\n" +
		"skin.path=/Users/x/Library/Android/sdk/skins/pixel_8\n" +
		"empty.value=\n")
	got := parseIni(in)
	if got["AvdId"] != "Pixel_8" {
		t.Errorf("CRLF line not handled: %q", got["AvdId"])
	}
	if got["spaced.key"] != "spaced value" {
		t.Errorf("whitespace not trimmed: %q", got["spaced.key"])
	}
	if _, ok := got["garbage-line-without-equals"]; ok {
		t.Error("a line with no '=' must be skipped, not stored as a key")
	}
	if v, ok := got["empty.value"]; !ok || v != "" {
		t.Errorf("an empty value must still register the key, got %q ok=%v", v, ok)
	}
	if len(got) != 4 {
		t.Errorf("unexpected key count: %v", got)
	}
}

func TestIniInt(t *testing.T) {
	m := map[string]string{
		"hw.ramSize": "8192", "sdcard.size": "512M", "disk.dataPartition.size": "10G",
		"lower": "2g", "junk": "abc", "blank": "",
	}
	cases := []struct {
		key  string
		want int
	}{
		{"hw.ramSize", 8192},
		{"sdcard.size", 512},
		{"disk.dataPartition.size", 10240},
		{"lower", 2048},
		{"junk", 0},
		{"blank", 0},
		{"absent", 0},
	}
	for _, tc := range cases {
		if got := iniInt(m, tc.key); got != tc.want {
			t.Errorf("iniInt(%q) = %d, want %d", tc.key, got, tc.want)
		}
	}
}

// Android now ships minor platform levels ("android-36.1"). Every compatibility
// rule adbq has is keyed on the major level, so the parse must not give up.
func TestAPIFromTarget(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"android-34", 34},
		{"android-36.1", 36},
		{"android-21", 21},
		{"36", 36},
		{"android-Baklava", 0}, // preview codename: no numeric level
		{"", 0},
	}
	for _, tc := range cases {
		if got := apiFromTarget(tc.in); got != tc.want {
			t.Errorf("apiFromTarget(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestAPIFromSysdir(t *testing.T) {
	if got := apiFromSysdir("system-images/android-34/google_apis/arm64-v8a/"); got != "android-34" {
		t.Errorf("got %q", got)
	}
	if got := apiFromSysdir("nothing/here"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// writeAVD lays out a minimal but realistic AVD on disk. dirName may differ
// from name, which is the case avdmanager actually produces.
func writeAVD(t *testing.T, avdHome, name, dirName, target string, cfg map[string]string) string {
	t.Helper()
	avdDir := filepath.Join(avdHome, dirName+".avd")
	if err := os.MkdirAll(avdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	top := "avd.ini.encoding=UTF-8\npath=" + avdDir + "\npath.rel=avd/" + dirName + ".avd\n"
	if target != "" {
		top += "target=" + target + "\n"
	}
	if err := os.WriteFile(filepath.Join(avdHome, name+".ini"), []byte(top), 0o644); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for k, v := range cfg {
		sb.WriteString(k + "=" + v + "\n")
	}
	if err := os.WriteFile(filepath.Join(avdDir, "config.ini"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return avdDir
}

// The AVD's identity is its .ini filename, not its directory name — passing the
// directory name to `emulator -avd` fails outright.
func TestLoadAVDUsesIniNameNotDirectoryName(t *testing.T) {
	home := t.TempDir()
	sdk := t.TempDir()
	writeAVD(t, home, "Medium_Phone_API_36.1", "Medium_Phone", "android-36.1", map[string]string{
		"AvdId":               "Medium_Phone_API_36.1",
		"avd.ini.displayname": "Medium Phone API 36.1",
		"abi.type":            "arm64-v8a",
		"PlayStore.enabled":   "true",
		"tag.id":              "google_apis_playstore",
		"tag.display":         "Google Play",
		"image.sysdir.1":      "system-images/android-36.1/google_apis_playstore/arm64-v8a/",
		"hw.ramSize":          "2048",
		"hw.lcd.width":        "1080",
		"hw.lcd.height":       "2400",
		"hw.lcd.density":      "420",
		"hw.device.name":      "medium_phone",
	})

	names, err := ListAVDNames(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "Medium_Phone_API_36.1" {
		t.Fatalf("ListAVDNames = %v, want the .ini name", names)
	}

	a := LoadAVD(home, sdk, names[0])
	if a.Error != "" {
		t.Fatalf("unexpected error: %s", a.Error)
	}
	if a.Display != "Medium Phone API 36.1" {
		t.Errorf("Display = %q", a.Display)
	}
	if a.API != 36 || a.Target != "android-36.1" {
		t.Errorf("API/Target = %d/%q, want 36/android-36.1", a.API, a.Target)
	}
	if !a.PlayStore {
		t.Error("PlayStore.enabled=true must set PlayStore")
	}
	if a.Resolution != "1080x2400" || a.RAMMB != 2048 || a.Density != 420 {
		t.Errorf("hardware fields wrong: %+v", a)
	}
	wantSys := filepath.Join(sdk, "system-images", "android-36.1", "google_apis_playstore", "arm64-v8a")
	if a.SysImgDir != wantSys {
		t.Errorf("SysImgDir = %q, want %q", a.SysImgDir, wantSys)
	}
	if a.RamdiskRel != "system-images/android-36.1/google_apis_playstore/arm64-v8a/ramdisk.img" {
		t.Errorf("RamdiskRel = %q", a.RamdiskRel)
	}
}

// Images predating PlayStore.enabled must still be recognised, otherwise adbq
// would offer `adb root` on an image that refuses it.
func TestLoadAVDInfersPlayStoreFromTag(t *testing.T) {
	home, sdk := t.TempDir(), t.TempDir()
	writeAVD(t, home, "Old_Play", "Old_Play", "android-30", map[string]string{
		"tag.id":         "google_apis_playstore",
		"image.sysdir.1": "system-images/android-30/google_apis_playstore/x86/",
	})
	a := LoadAVD(home, sdk, "Old_Play")
	if !a.PlayStore {
		t.Error("a *_playstore tag must imply PlayStore even without PlayStore.enabled")
	}
}

// A missing target= must not leave the AVD level-less: the system image path
// carries the same information.
func TestLoadAVDFallsBackToSysdirForTarget(t *testing.T) {
	home, sdk := t.TempDir(), t.TempDir()
	writeAVD(t, home, "NoTarget", "NoTarget", "", map[string]string{
		"image.sysdir.1": "system-images/android-27/google_apis/arm64-v8a/",
	})
	a := LoadAVD(home, sdk, "NoTarget")
	if a.API != 27 {
		t.Errorf("API = %d, want 27 recovered from image.sysdir.1", a.API)
	}
}

// A broken AVD must be listed as broken, not silently dropped — a vanished row
// reads as "deleted" to the user.
func TestLoadAVDReportsBrokenDefinitions(t *testing.T) {
	home, sdk := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "Broken.ini"),
		[]byte("path="+filepath.Join(home, "Broken.avd")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := LoadAVD(home, sdk, "Broken")
	if a.State != AVDError || a.Error == "" {
		t.Errorf("a missing config.ini must yield an error state, got %+v", a)
	}
	if a.Name != "Broken" {
		t.Errorf("a broken AVD must keep its name, got %q", a.Name)
	}
}

func TestReadSnapshotsAndPatchedMarker(t *testing.T) {
	home, sdk := t.TempDir(), t.TempDir()
	dir := writeAVD(t, home, "Snap", "Snap", "android-34", map[string]string{
		"image.sysdir.1": "system-images/android-34/google_apis/arm64-v8a/",
	})
	for _, s := range []string{"default_boot", "snap_2026-05-06"} {
		if err := os.MkdirAll(filepath.Join(dir, "snapshots", s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sysDir := filepath.Join(sdk, "system-images", "android-34", "google_apis", "arm64-v8a")
	if err := os.MkdirAll(sysDir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := LoadAVD(home, sdk, "Snap")
	if len(a.Snapshots) != 2 || a.Snapshots[0] != "default_boot" {
		t.Errorf("Snapshots = %v", a.Snapshots)
	}
	if a.Patched {
		t.Error("no .backup next to the ramdisk must mean not patched")
	}

	if err := os.WriteFile(filepath.Join(sysDir, "ramdisk.img.backup"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LoadAVD(home, sdk, "Snap").Patched {
		t.Error("a ramdisk.img.backup must mark the image as patched")
	}
}

// Nil slices marshal to JSON null while the generated bindings type them as
// arrays; the UI then throws on .length. See docs/development-guidelines.md.
func TestAVDMarshalsEmptySlicesNotNull(t *testing.T) {
	home, sdk := t.TempDir(), t.TempDir()
	writeAVD(t, home, "Plain", "Plain", "android-34", map[string]string{})
	b, err := json.Marshal(LoadAVD(home, sdk, "Plain"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("AVD must not marshal any field as null: %s", b)
	}
}

func TestListAVDNamesOnMissingHome(t *testing.T) {
	names, err := ListAVDNames(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("a missing AVD home is not an error, got %v", err)
	}
	if names == nil || len(names) != 0 {
		t.Errorf("want an empty non-nil slice, got %#v", names)
	}
}
