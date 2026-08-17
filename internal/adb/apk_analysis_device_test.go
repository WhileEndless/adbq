package adb

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in device tests for the analysis paths, in the same style as the export
// ones (see apks_device_test.go for the environment variables).
//
// Nothing here changes anything on the device: both paths only read.

func TestDeviceStageApksIsIdempotent(t *testing.T) {
	c, serial := probeSerial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pkg, set := firstSplitApp(t, c, ctx, serial)

	first, err := c.StageApks(ctx, serial, pkg, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatalf("StageApks: %v", err)
	}
	want := 1 + len(set.Splits)
	if len(first.Files) != want {
		t.Fatalf("staged %d APK(s), want %d", len(first.Files), want)
	}
	if !strings.HasSuffix(strings.ToLower(first.Names[0]), ".apk") {
		t.Errorf("first staged file is not an APK: %q", first.Names[0])
	}
	if !strings.EqualFold(first.Names[0], "base.apk") {
		t.Errorf("base must be staged first, got %q", first.Names[0])
	}

	// A second run must reuse what is already there — this is what makes
	// reopening an app instant.
	second, err := c.StageApks(ctx, serial, pkg, func(s string) { t.Errorf("nothing should be pulled again: %s", s) })
	if err != nil {
		t.Fatalf("second StageApks: %v", err)
	}
	if !second.Cached {
		t.Error("the second staging did not report itself as cached")
	}
}

func TestDevicePlanJadxOpenMatchesTheApkSet(t *testing.T) {
	c, serial := probeSerial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pkg, set := firstSplitApp(t, c, ctx, serial)

	plan, err := c.PlanJadxOpen(ctx, serial, pkg, JadxStatus(HostSettings{}, ""))
	if err != nil {
		t.Fatalf("PlanJadxOpen: %v", err)
	}
	if len(plan.Names) != 1+len(set.Splits) {
		t.Errorf("plan covers %d input(s), want %d", len(plan.Names), 1+len(set.Splits))
	}
	joined := strings.Join(plan.Commands, "\n")
	// Every APK must appear in the launch line, or a split's code would be
	// missing from the session the user is shown.
	last := plan.Commands[len(plan.Commands)-1]
	for _, n := range plan.Names {
		if !strings.Contains(last, n) {
			t.Errorf("%s is missing from the launch command:\n%s", n, last)
		}
	}
	if strings.Count(joined, "adb -s "+serial+" pull ") != len(plan.Names) {
		t.Errorf("expected a pull per input:\n%s", joined)
	}
	t.Logf("ready=%v reason=%q staged=%v", plan.Ready, plan.Reason, plan.Staged)
}

func TestDeviceExportAppBinaries(t *testing.T) {
	c, serial := probeSerial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	pkg, set := firstSplitApp(t, c, ctx, serial)

	plan := PlanAppBinaries(serial, set)
	if !strings.HasSuffix(plan.Suggested, "-binaries.zip") {
		t.Errorf("suggested name %q", plan.Suggested)
	}
	dst := filepath.Join(t.TempDir(), plan.Suggested)
	if _, err := c.ExportAppBinaries(ctx, serial, pkg, dst, func(s string) { t.Log(s) }); err != nil {
		t.Fatalf("ExportAppBinaries: %v", err)
	}

	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatalf("the archive is not readable: %v", err)
	}
	defer zr.Close()
	var manifest binariesManifest
	seen := 0
	for _, e := range zr.File {
		if e.Name == "manifest.json" {
			rc, err := e.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &manifest); err != nil {
				t.Fatalf("manifest is not valid JSON: %v", err)
			}
			continue
		}
		seen++
	}
	if manifest.Package != pkg {
		t.Errorf("manifest names %q, want %q", manifest.Package, pkg)
	}
	if len(manifest.Entries) != seen {
		t.Errorf("manifest lists %d entries but the archive holds %d files", len(manifest.Entries), seen)
	}
	for _, e := range manifest.Entries {
		if len(e.SHA256) != 64 {
			t.Errorf("%s/%s has no usable digest", e.Source, e.Path)
		}
	}
	t.Logf("collected %d file(s); notes=%v", seen, manifest.Notes)
}

// Every export must be distinguishable by version, which is only worth
// asserting against a real device: the version pair comes from dumpsys.
func TestDeviceExportNamesCarryTheVersion(t *testing.T) {
	c, serial := probeSerial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pkg, set := firstSplitApp(t, c, ctx, serial)

	if set.Version.Name == "" && set.Version.Code == "" {
		t.Skip("this device reports no version for the probe package")
	}
	if !strings.HasPrefix(set.Suggested, pkg+"-") {
		t.Errorf("suggested name %q does not carry a version", set.Suggested)
	}
	if got := c.ExportBaseNameFor(ctx, serial, pkg); got == pkg {
		t.Errorf("ExportBaseNameFor returned the bare package name despite a known version")
	}
}
