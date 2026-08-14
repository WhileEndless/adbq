package adb

import (
	"errors"
	"strings"
	"testing"
)

// errTest stands in for the exec error a failed sdkmanager run returns.
var errTest = errors.New("exit status 1")

const sdkManagerListSample = `Loading package information...
Installed packages:
  Path                                                       | Version | Description                         | Location
  -------                                                    | ------- | -------                             | -------
  build-tools;36.1.0                                         | 36.1.0  | Android SDK Build-Tools 36.1        | build-tools/36.1.0
  emulator                                                   | 36.3.10 | Android Emulator                    | emulator
  system-images;android-34;google_apis;arm64-v8a             | 14      | Google APIs ARM 64 v8a System Image | system-images/android-34/google_apis/arm64-v8a
  system-images;android-36.1;google_apis_playstore;arm64-v8a | 3       | Google Play ARM 64 v8a System Image | system-images/android-36.1/google_apis_playstore/arm64-v8a

Available Packages:
  Path                                            | Version | Description
  -------                                         | ------- | -------
  add-ons;addon-google_apis-google-15             | 3       | Google APIs
  system-images;android-33;google_apis;x86_64     | 12      | Google APIs Intel x86_64 Atom System Image
  system-images;android-33;google_apis_playstore;x86_64 | 9 | Google Play Intel x86_64 Atom System Image
  system-images;android-21;default;armeabi-v7a    | 5       | ARM EABI v7a System Image
`

func TestParseSDKManagerList(t *testing.T) {
	imgs := parseSDKManagerList(sdkManagerListSample)
	// 2 installed + 3 available; build-tools, emulator and add-ons rows drop out.
	if len(imgs) != 5 {
		t.Fatalf("parsed %d images, want 5 (non-system-image rows must be dropped): %+v", len(imgs), imgs)
	}

	byPkg := map[string]SystemImage{}
	for _, i := range imgs {
		byPkg[i.Pkg] = i
	}

	got, ok := byPkg["system-images;android-34;google_apis;arm64-v8a"]
	if !ok {
		t.Fatal("installed image missing")
	}
	if !got.Installed {
		t.Error("rows under 'Installed packages' must be marked installed")
	}
	if got.API != 34 || got.Tag != "google_apis" || got.ABI != "arm64-v8a" || got.Revision != "14" {
		t.Errorf("fields wrong: %+v", got)
	}
	if got.PlayStore || !got.Rootable {
		t.Error("a google_apis image is not a Play Store image and is rootable with adb root")
	}

	play := byPkg["system-images;android-36.1;google_apis_playstore;arm64-v8a"]
	if !play.PlayStore || play.Rootable {
		t.Error("a Play Store image must be flagged and marked not adb-root-able")
	}
	if play.API != 36 || play.Level != "android-36.1" {
		t.Errorf("minor platform level mishandled: api=%d level=%q", play.API, play.Level)
	}

	avail := byPkg["system-images;android-33;google_apis;x86_64"]
	if avail.Installed {
		t.Error("rows under 'Available Packages' must not be marked installed")
	}
}

// Newest API first is what a user scanning the list expects — but only within a
// compatibility group, because an image this computer cannot run is never the
// answer however new it is.
func TestParseSDKManagerListSortsNewestFirstWithinCompatibility(t *testing.T) {
	imgs := parseSDKManagerList(sdkManagerListSample)

	seenIncompatible := false
	prev := 1 << 30
	for _, i := range imgs {
		if !i.Compatible {
			if !seenIncompatible {
				seenIncompatible = true
				prev = 1 << 30 // a new group restarts the ordering
			}
		} else if seenIncompatible {
			t.Fatalf("a runnable image sorted after an unrunnable one: %s", i.Pkg)
		}
		if i.API > prev {
			t.Fatalf("API levels are not descending within a group: %s", i.Pkg)
		}
		prev = i.API
	}
}

func TestSystemImageFromPkg(t *testing.T) {
	if _, ok := systemImageFromPkg("platforms;android-34"); ok {
		t.Error("a non-system-image package must be rejected")
	}
	if _, ok := systemImageFromPkg("system-images;android-34;google_apis"); ok {
		t.Error("a truncated package path must be rejected")
	}
	img, ok := systemImageFromPkg("system-images;android-CinnamonBun;google_apis_ps16k;arm64-v8a")
	if !ok {
		t.Fatal("a preview codename package must still parse")
	}
	if img.API != 0 || img.AndroidVer != "CinnamonBun" {
		t.Errorf("preview level mishandled: %+v", img)
	}
	if !img.PlayStore {
		t.Error("a _ps16k tag is a Play Store image")
	}
}

// A package name arriving from the UI must never be able to grow into extra
// sdkmanager arguments.
func TestValidateSDKPackageRejectsInjection(t *testing.T) {
	bad := []string{
		"", "  ",
		"system-images;android-34;google_apis;arm64-v8a --uninstall platform-tools",
		"system-images;android-34;google_apis;arm64-v8a`whoami`",
		"platforms;android-34",
		"system-images;android-34;google_apis;arm64-v8a;extra",
	}
	for _, p := range bad {
		if err := validateSDKPackage(p); err == nil {
			t.Errorf("validateSDKPackage(%q) must fail", p)
		}
	}
	if err := validateSDKPackage("system-images;android-34;google_apis;arm64-v8a"); err != nil {
		t.Errorf("a valid package was rejected: %v", err)
	}
}

func TestParseSDKManagerProgress(t *testing.T) {
	cases := []struct {
		line      string
		wantStage string
		wantPct   int
	}{
		{"[=========                              ] 25% Loading local repository...", "Loading local repository...", 25},
		{"[=======================================] 100% Unzipping... arm64-v8a/system.img", "Unzipping... arm64-v8a/system.img", 100},
		{"Downloading https://dl.google.com/android/repository/x.zip", "Downloading https://dl.google.com/android/repository/x.zip", 0},
		{"", "", 0},
	}
	for _, tc := range cases {
		stage, pct := parseSDKManagerProgress(tc.line)
		if stage != tc.wantStage || pct != tc.wantPct {
			t.Errorf("parseSDKManagerProgress(%q) = (%q, %d), want (%q, %d)", tc.line, stage, pct, tc.wantStage, tc.wantPct)
		}
	}
}

// The filesystem is the truth for what is installed; the manifest can be stale
// or unreachable, so a merge must never downgrade an installed image.
func TestMergeSystemImagesKeepsDiskTruth(t *testing.T) {
	installed := []SystemImage{{
		Pkg: "system-images;android-34;google_apis;arm64-v8a", API: 34,
		Installed: true, Location: "/sdk/system-images/android-34/google_apis/arm64-v8a", Revision: "14",
	}}
	remote := []SystemImage{
		{Pkg: "system-images;android-34;google_apis;arm64-v8a", API: 34, Desc: "Google APIs"},
		{Pkg: "system-images;android-33;google_apis;x86_64", API: 33, Desc: "Google APIs"},
	}
	merged := mergeSystemImages(installed, remote)
	if len(merged) != 2 {
		t.Fatalf("merged %d, want 2: %+v", len(merged), merged)
	}
	for _, m := range merged {
		if m.API == 34 {
			if !m.Installed || m.Location == "" || m.Revision != "14" {
				t.Errorf("the installed image lost its disk facts: %+v", m)
			}
			if m.Desc != "Google APIs" {
				t.Errorf("the remote description should still be picked up: %+v", m)
			}
		}
		if m.API == 33 && m.Installed {
			t.Error("a remote-only image must not be marked installed")
		}
	}
}

func TestSDKManagerErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		out  []string
		want string
	}{
		{"licence", []string{"Warning: License android-sdk-license not accepted."}, "licence"},
		{"missing package", []string{"Warning: Failed to find package system-images;android-99;x;y"}, "no such package"},
		{"network", []string{"java.net.UnknownHostException: dl.google.com"}, "could not reach"},
		{"disk", []string{"java.io.IOException: No space left on device"}, "disk space"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sdkManagerError(tc.out, errTest)
			if !strings.Contains(strings.ToLower(got), tc.want) {
				t.Errorf("sdkManagerError() = %q, want it to mention %q", got, tc.want)
			}
			// A Java stack frame is never a useful message for a user.
			if strings.HasPrefix(got, "at ") {
				t.Errorf("stack frame leaked to the user: %q", got)
			}
		})
	}
}

// scanLinesOrCR must split on the carriage returns sdkmanager uses to redraw
// its progress bar, or every percentage arrives as one huge final line.
func TestScanLinesOrCRSplitsProgressRedraws(t *testing.T) {
	data := []byte("[==   ] 20% A\r[=====] 60% B\nDone\n")
	var got []string
	for len(data) > 0 {
		adv, tok, _ := scanLinesOrCR(data, true)
		if adv == 0 {
			break
		}
		got = append(got, string(tok))
		data = data[adv:]
	}
	if len(got) != 3 || got[0] != "[==   ] 20% A" || got[2] != "Done" {
		t.Errorf("split = %q", got)
	}
}

// An image whose ABI the host cannot run installs happily and then never boots.
// The verdict belongs next to the Install button, not in the emulator's output.
func TestHostCompatibilityMatchesThisMachine(t *testing.T) {
	preferred := HostABIs()
	if len(preferred) == 0 {
		t.Skip("unknown host architecture")
	}
	native := newSystemImage("android-34", "google_apis", preferred[0])
	if !native.Compatible || native.Note != "" {
		t.Errorf("the host's own ABI must be compatible and unremarked: %+v", native)
	}

	foreign := "x86_64"
	if preferred[0] == "x86_64" {
		foreign = "arm64-v8a"
	}
	bad := newSystemImage("android-34", "google_apis", foreign)
	if bad.Compatible {
		t.Errorf("%s must not be reported as runnable on a %s host", foreign, preferred[0])
	}
	if !strings.Contains(bad.Note, preferred[0]) {
		t.Errorf("the note must point at the ABI that does work: %q", bad.Note)
	}

	// A secondary ABI runs, but slowly — that is a warning, not a rejection.
	if len(preferred) > 1 {
		slow := newSystemImage("android-34", "google_apis", preferred[1])
		if !slow.Compatible || slow.Note == "" {
			t.Errorf("a secondary ABI must be usable but flagged as slow: %+v", slow)
		}
	}
}

// Sorting must put runnable images first: an incompatible one is never the
// answer, however new it is.
func TestSortSystemImagesPutsRunnableFirst(t *testing.T) {
	preferred := HostABIs()
	if len(preferred) == 0 {
		t.Skip("unknown host architecture")
	}
	foreign := "x86_64"
	if preferred[0] == "x86_64" {
		foreign = "arm64-v8a"
	}
	imgs := []SystemImage{
		newSystemImage("android-36", "google_apis", foreign),      // newer, unusable
		newSystemImage("android-30", "google_apis", preferred[0]), // older, usable
	}
	sortSystemImages(imgs)
	if !imgs[0].Compatible {
		t.Errorf("a runnable image must sort ahead of a newer unrunnable one: %+v", imgs)
	}
}
