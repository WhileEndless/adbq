package adb

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApkSplitKind(t *testing.T) {
	cases := []struct{ name, kind, token string }{
		{"base.apk", "", ""},
		{"split_config.arm64_v8a.apk", "abi", "arm64-v8a"},
		{"split_config.armeabi_v7a.apk", "abi", "armeabi-v7a"},
		{"config.x86_64.apk", "abi", "x86_64"},
		{"splits/base-arm64_v8a.apk", "abi", "arm64-v8a"},
		{"split_config.xxhdpi.apk", "density", "xxhdpi"},
		{"splits/base-xxxhdpi.apk", "density", "xxxhdpi"},
		{"split_config.en.apk", "locale", "en"},
		{"splits/base-pt.apk", "locale", "pt"},
		{"split_config.zh_CN.apk", "locale", "zh_CN"},
		// A feature module is neither a config split nor the base; it must be
		// installed unconditionally.
		{"split_dynamicfeature.apk", "", ""},
		{"toc.pb", "", ""},
	}
	for _, c := range cases {
		k, tok := apkSplitKind(c.name)
		if k != c.kind || tok != c.token {
			t.Errorf("apkSplitKind(%q) = (%q,%q), want (%q,%q)", c.name, k, tok, c.kind, c.token)
		}
	}
}

func TestSelectApkEntriesKeepsOnlyApplicableSplits(t *testing.T) {
	names := []string{
		"base.apk",
		"split_config.arm64_v8a.apk",
		"split_config.armeabi_v7a.apk",
		"split_config.x86.apk",
		"split_config.xxhdpi.apk",
		"split_config.hdpi.apk",
		"split_config.en.apk",
		"split_config.tr.apk",
		"split_feature_maps.apk",
		"meta.sai_v2.json",
	}
	sel := selectApkEntries(names, []string{"arm64-v8a", "armeabi-v7a"}, 480)
	if sel.err != nil {
		t.Fatalf("unexpected error: %v", sel.err)
	}
	keep := strings.Join(sel.keep, " ")
	for _, want := range []string{"base.apk", "split_config.arm64_v8a.apk", "split_config.armeabi_v7a.apk",
		"split_config.xxhdpi.apk", "split_config.en.apk", "split_config.tr.apk", "split_feature_maps.apk"} {
		if !strings.Contains(keep, want) {
			t.Errorf("expected %s to be installed, got %v", want, sel.keep)
		}
	}
	for _, bad := range []string{"split_config.x86.apk", "split_config.hdpi.apk"} {
		if strings.Contains(keep, bad) {
			t.Errorf("%s must not be installed on this device: %v", bad, sel.keep)
		}
	}
	// The metadata file is expected in every adbq export, so it must not be
	// reported as something we dropped.
	for _, s := range sel.skipped {
		if strings.Contains(s, "meta.sai_v2.json") {
			t.Errorf("meta file reported as skipped: %v", sel.skipped)
		}
	}
}

func TestSelectApkEntriesFailsWhenNoAbiMatches(t *testing.T) {
	sel := selectApkEntries([]string{"base.apk", "split_config.x86.apk"}, []string{"arm64-v8a"}, 420)
	if sel.err == nil {
		t.Fatal("expected an error when no ABI split matches the device")
	}
	if !strings.Contains(sel.err.Error(), "arm64-v8a") {
		t.Errorf("the error should name the device ABIs, got %q", sel.err)
	}
}

func TestSelectApkEntriesUnknownDensityKeepsAll(t *testing.T) {
	// With no density reading we must not guess, or we would strip the only
	// resource split the app has.
	sel := selectApkEntries([]string{"base.apk", "split_config.xxhdpi.apk", "split_config.hdpi.apk"}, nil, 0)
	if len(sel.keep) != 3 {
		t.Errorf("unknown density must keep every split, got %v", sel.keep)
	}
}

func TestPreferredApkEntriesPrefersSplitsOverStandalones(t *testing.T) {
	got := preferredApkEntries([]string{
		"toc.pb", "splits/base-master.apk", "splits/base-arm64_v8a.apk",
		"standalones/standalone-arm64_v8a.apk", "universal.apk",
	})
	if len(got) != 2 || !strings.HasPrefix(got[0], "splits/") {
		t.Fatalf("bundletool splits/ must win, got %v", got)
	}
}

func TestPreferredApkEntriesFallsBackToUniversal(t *testing.T) {
	got := preferredApkEntries([]string{"toc.pb", "universal.apk"})
	if len(got) != 1 || got[0] != "universal.apk" {
		t.Fatalf("got %v", got)
	}
}

func TestApkEntryRankPutsBaseFirst(t *testing.T) {
	// pm derives the session package from the first APK, so a config split
	// must never lead the install-multiple argument list.
	if apkEntryRank("base.apk") >= apkEntryRank("split_config.arm64_v8a.apk") {
		t.Error("base.apk must sort before a config split")
	}
	if apkEntryRank("split_feature_maps.apk") >= apkEntryRank("split_config.xxhdpi.apk") {
		t.Error("a feature module must sort before a config split")
	}
}

func TestClosestDensityBucket(t *testing.T) {
	names := []string{"split_config.hdpi.apk", "split_config.xxhdpi.apk", "split_config.xxxhdpi.apk"}
	if got := closestDensityBucket(names, 640); got != "xxxhdpi" {
		t.Errorf("640dpi → %q", got)
	}
	if got := closestDensityBucket(names, 240); got != "hdpi" {
		t.Errorf("240dpi → %q", got)
	}
	if got := closestDensityBucket(names, 560); got != "xxxhdpi" {
		t.Errorf("560dpi → %q", got)
	}
	if got := closestDensityBucket([]string{"base.apk"}, 480); got != "" {
		t.Errorf("no density split → %q", got)
	}
}

func TestApkSetFromPathsFindsBaseAndBuildsCommands(t *testing.T) {
	set := apkSetFromPaths("SER", "com.x", []string{
		"/data/app/~~a/com.x-1/split_config.arm64_v8a.apk",
		"/data/app/~~a/com.x-1/base.apk",
	}, AppVersion{Name: "1.2.3", Code: "10203"})
	if !strings.HasSuffix(set.Base, "base.apk") {
		t.Errorf("base misdetected: %q", set.Base)
	}
	if !set.Split || len(set.Splits) != 1 {
		t.Errorf("split app not recognised: %+v", set)
	}
	if set.Suggested != "com.x-1.2.3-10203.apks" {
		t.Errorf("suggested name %q", set.Suggested)
	}
	joined := strings.Join(set.Commands, "\n")
	if !strings.Contains(joined, "adb -s SER shell pm path com.x") {
		t.Errorf("pm path command missing:\n%s", joined)
	}
	if strings.Count(joined, "adb -s SER pull ") != 2 {
		t.Errorf("expected a pull per APK:\n%s", joined)
	}
	// The base must be pulled first so a partial export is still installable.
	if !strings.Contains(set.Commands[1], "base.apk") {
		t.Errorf("base should be pulled first:\n%s", joined)
	}
}

func TestApkSetFromPathsSingleApk(t *testing.T) {
	set := apkSetFromPaths("SER", "com.x", []string{"/data/app/com.x-1/base.apk"}, AppVersion{})
	if set.Split || len(set.Splits) != 0 || set.Suggested != "com.x.apk" {
		t.Errorf("non-split app misreported: %+v", set)
	}
}

func TestUniqueEntryNameAvoidsCollisions(t *testing.T) {
	taken := []string{"base.apk"}
	if got := uniqueEntryName("base.apk", taken); got != "base-2.apk" {
		t.Errorf("got %q", got)
	}
	if got := uniqueEntryName("split.apk", taken); got != "split.apk" {
		t.Errorf("got %q", got)
	}
}

func TestWriteApksArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"base.apk", "split_config.arm64_v8a.apk"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("PK-"+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "out.apks")
	meta := saiMeta{Package: "com.x", Label: "X", VersionName: "1.0", VersionCode: 7, SplitApk: true}
	if err := writeApksArchive(out, dir, []string{"base.apk", "split_config.arm64_v8a.apk"}, meta); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{"base.apk", "split_config.arm64_v8a.apk", "meta.sai_v2.json"}
	if len(names) != len(want) {
		t.Fatalf("archive entries = %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("entry %d = %q, want %q", i, names[i], w)
		}
	}
	// Re-reading it as a container must select both APKs again.
	sel := selectApkEntries(names, []string{"arm64-v8a"}, 480)
	if sel.err != nil || len(sel.keep) != 2 {
		t.Errorf("round trip selection = %v (err %v)", sel.keep, sel.err)
	}
}

func TestIsApkBundle(t *testing.T) {
	for _, p := range []string{"/x/a.apks", "/x/a.APKS", "/x/a.xapk", "/x/a.zip"} {
		if !IsApkBundle(p) {
			t.Errorf("%s should be a bundle", p)
		}
	}
	if IsApkBundle("/x/a.apk") {
		t.Error("a plain .apk is not a bundle")
	}
}

// A split export written under a .apk name is the one failure the user cannot
// see: the file installs nowhere, including back into adbq.
func TestEnsureExportExtMatchesTheContent(t *testing.T) {
	cases := []struct {
		dst, want string
		split     bool
	}{
		{"/x/com.a.b.apks", "/x/com.a.b.apks", true},
		{"/x/com.a.b.apk", "/x/com.a.b.apks", true},
		{"/x/com.a.b.APK", "/x/com.a.b.apks", true},
		{"/x/com.a.b", "/x/com.a.b.apks", true},
		{"/x/backup.zip", "/x/backup.zip", true},
		{"/x/com.a.b.apk", "/x/com.a.b.apk", false},
		{"/x/com.a.b", "/x/com.a.b.apk", false},
		{"/x/com.a.b.apks", "/x/com.a.b.apk", false},
	}
	for _, c := range cases {
		if got := EnsureExportExt(c.dst, c.split); got != c.want {
			t.Errorf("EnsureExportExt(%q, split=%v) = %q, want %q", c.dst, c.split, got, c.want)
		}
	}
}

func TestInstallMultipleErrExplainsCommonFailures(t *testing.T) {
	cases := []struct{ out, want string }{
		{"Failure [INSTALL_FAILED_MISSING_SPLIT: Missing split for com.x]", "missing a split"},
		{"Failure [INSTALL_FAILED_VERSION_DOWNGRADE]", "newer version is installed"},
		{"Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: signatures do not match]", "different key"},
		{"Failure [INSTALL_FAILED_NO_MATCHING_ABIS]", "native code"},
	}
	for _, c := range cases {
		err := installMultipleErr(c.out, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("installMultipleErr(%q) = %v, want it to mention %q", c.out, err, c.want)
		}
	}
	if err := installMultipleErr("Success\n", nil); err != nil {
		t.Errorf("a successful install must not error: %v", err)
	}
}

func TestApkSetMarshalsEmptySlicesNotNull(t *testing.T) {
	// A nil Go slice marshals as JSON null while the generated TS type says
	// string[], so the UI crashed reading .length off it. Non-split apps are
	// the case that produced no splits at all.
	b, err := json.Marshal(apkSetFromPaths("SER", "com.x", []string{"/data/app/com.x-1/base.apk"}, AppVersion{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("ApkSet must not marshal any field as null: %s", b)
	}
}
