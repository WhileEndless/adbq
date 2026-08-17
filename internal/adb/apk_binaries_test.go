package adb

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyBinaryEntry(t *testing.T) {
	elf := append([]byte{0x7f, 'E', 'L', 'F'}, 1, 2, 3, 4)
	pe := append([]byte{'M', 'Z'}, 0, 0, 0, 0)
	png := []byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0}
	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"lib/arm64-v8a/libapp.so", elf, "so"},
		// Extraction-time compression can leave the entry unreadable as ELF; the
		// path already says what it is.
		{"lib/arm64-v8a/libnative.so", png, "so"},
		{"assets/tool", elf, "elf"},
		{"assets/bin/Data/Managed/Assembly-CSharp.dll", pe, "pe"},
		{"assets/flutter_assets/kernel_blob.bin", png, "blob"},
		{"assets/bin/Data/Managed/Metadata/global-metadata.dat", png, "blob"},
		{"assets/images/logo.png", png, ""},
		{"assets/config.json", []byte("{\"a\":1}"), ""},
		{`lib\arm64-v8a\libapp.so`, png, "so"},
	}
	for _, c := range cases {
		if got := classifyBinaryEntry(c.name, c.head); got != c.want {
			t.Errorf("classifyBinaryEntry(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSkipBinaryScanLeavesOutWhatTheApkExportAlreadyCovers(t *testing.T) {
	for _, n := range []string{"classes.dex", "classes2.dex", "AndroidManifest.xml", "resources.arsc", "res/drawable/x.xml", "META-INF/CERT.RSA"} {
		if !skipBinaryScan(n) {
			t.Errorf("%q should be skipped", n)
		}
	}
	for _, n := range []string{"lib/arm64-v8a/libapp.so", "assets/flutter_assets/kernel_blob.bin", "assets/tool"} {
		if skipBinaryScan(n) {
			t.Errorf("%q must not be skipped", n)
		}
	}
}

func TestScanApkBinariesPicksOnlyTheBinaries(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "base.apk")
	writeTestZip(t, apk, map[string]string{
		"lib/arm64-v8a/libapp.so":               "\x7fELFlibrary",
		"lib/arm64-v8a/libflutter.so":           "\x7fELFengine",
		"assets/flutter_assets/kernel_blob.bin": "dart kernel",
		"assets/helper":                         "\x7fELFexecutable",
		"assets/managed/Assembly-CSharp.dll":    "MZ\x90\x00pe",
		"assets/config.json":                    "{}",
		"res/drawable/logo.png":                 "\x89PNG",
		"classes.dex":                           "dex\n035",
		"AndroidManifest.xml":                   "binary xml",
		"resources.arsc":                        "arsc",
	})
	got, err := scanApkBinaries(apk, "base.apk")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"lib/arm64-v8a/libapp.so":               "so",
		"lib/arm64-v8a/libflutter.so":           "so",
		"assets/flutter_assets/kernel_blob.bin": "blob",
		"assets/helper":                         "elf",
		"assets/managed/Assembly-CSharp.dll":    "pe",
	}
	if len(got) != len(want) {
		t.Fatalf("collected %d entries, want %d: %+v", len(got), len(want), got)
	}
	for _, e := range got {
		if want[e.Path] != e.Kind {
			t.Errorf("%s classified as %q, want %q", e.Path, e.Kind, want[e.Path])
		}
		if e.Source != "base.apk" {
			t.Errorf("entry %s lost its source: %q", e.Path, e.Source)
		}
	}
	// Sorted output keeps the archive and the manifest stable between runs.
	for i := 1; i < len(got); i++ {
		if got[i-1].Path > got[i].Path {
			t.Errorf("entries are not sorted: %q before %q", got[i-1].Path, got[i].Path)
		}
	}
}

func TestScanApkBinariesOnAnAppWithNone(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "base.apk")
	writeTestZip(t, apk, map[string]string{
		"classes.dex":         "dex\n035",
		"AndroidManifest.xml": "binary xml",
		"res/layout/a.xml":    "xml",
	})
	got, err := scanApkBinaries(apk, "base.apk")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("an app with no binaries must yield none, got %+v", got)
	}
}

func TestWriteBinariesArchiveGroupsBySourceAndHashesEverything(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.apk")
	split := filepath.Join(dir, "split_config.arm64_v8a.apk")
	writeTestZip(t, base, map[string]string{
		"lib/arm64-v8a/libapp.so": "\x7fELFbase",
		"classes.dex":             "dex",
	})
	writeTestZip(t, split, map[string]string{
		"lib/arm64-v8a/libflutter.so": "\x7fELFsplit",
	})

	var entries []BinaryEntry
	for _, s := range []apkSource{{base, "base.apk"}, {split, "split_config.arm64_v8a.apk"}} {
		found, err := scanApkBinaries(s.path, s.source)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, found...)
	}
	manifest := binariesManifest{Package: "com.example.app", VersionCode: "10203", Entries: []BinaryEntry{}, Notes: []string{}}
	dst := filepath.Join(dir, "out.zip")
	n, err := writeBinariesArchive(dst, []apkSource{{base, "base.apk"}, {split, "split_config.arm64_v8a.apk"}}, "", entries, &manifest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("wrote %d files, want 2", n)
	}

	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	var manifestBody []byte
	for _, e := range zr.File {
		names[e.Name] = true
		if e.Name == "manifest.json" {
			rc, err := e.Open()
			if err != nil {
				t.Fatal(err)
			}
			manifestBody, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	// Grouping by source is what makes two identically named libraries from
	// different splits distinguishable.
	for _, want := range []string{
		"base.apk/lib/arm64-v8a/libapp.so",
		"split_config.arm64_v8a.apk/lib/arm64-v8a/libflutter.so",
		"manifest.json",
	} {
		if !names[want] {
			t.Errorf("archive is missing %q (has %v)", want, names)
		}
	}
	if names["base.apk/classes.dex"] {
		t.Error("dex files belong to the APK export, not this one")
	}

	var got binariesManifest
	if err := json.Unmarshal(manifestBody, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("manifest lists %d entries, want 2", len(got.Entries))
	}
	for _, e := range got.Entries {
		if len(e.SHA256) != 64 {
			t.Errorf("entry %s has no usable digest: %q", e.Path, e.SHA256)
		}
		if e.Size == 0 {
			t.Errorf("entry %s reports no size", e.Path)
		}
	}
}

// An app with nothing to collect must still produce a readable archive that says
// so, rather than failing or leaving an empty file behind.
func TestWriteBinariesArchiveSaysWhenThereIsNothing(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "empty.zip")
	manifest := binariesManifest{Package: "com.example.app", Entries: []BinaryEntry{}, Notes: []string{}}
	n, err := writeBinariesArchive(dst, nil, "", nil, &manifest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("wrote %d files, want 0", n)
	}
	if len(manifest.Notes) == 0 {
		t.Error("an empty collection must be explained in the manifest")
	}
	if _, err := zip.OpenReader(dst); err != nil {
		t.Errorf("the archive must still be readable: %v", err)
	}
}

func TestPlanAppBinariesDescribesTheWork(t *testing.T) {
	set := apkSetFromPaths("SER", "com.example.app", []string{
		"/data/app/~~a/com.example.app-1/base.apk",
		"/data/app/~~a/com.example.app-1/split_config.arm64_v8a.apk",
	}, AppVersion{Name: "1.2.3", Code: "10203"})
	plan := PlanAppBinaries("SER", set)
	if plan.Suggested != "com.example.app-1.2.3-10203-binaries.zip" {
		t.Errorf("suggested name %q", plan.Suggested)
	}
	if plan.Sources != 2 {
		t.Errorf("sources = %d, want 2", plan.Sources)
	}
	joined := strings.Join(plan.Commands, "\n")
	if strings.Count(joined, "adb -s SER pull ") != 3 {
		t.Errorf("want a pull per APK plus the extracted library directory:\n%s", joined)
	}
	if !strings.Contains(joined, "/data/app/~~a/com.example.app-1/lib") {
		t.Errorf("the extracted library directory must appear in the preview:\n%s", joined)
	}
	if !strings.Contains(joined, plan.Suggested) {
		t.Errorf("the preview must name the file it produces:\n%s", joined)
	}
}
