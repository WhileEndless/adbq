package adb

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests talk to a real device and are opt-in, like the other probe
// tests in this package:
//
//	ADBQ_PROBE_SERIAL         source device to export from (required)
//	ADBQ_PROBE_INSTALL_SERIAL target device to install into (optional)
//
// The install test deliberately takes a *separate* serial: reinstalling an app
// onto the machine it came from is a live change to someone's phone, so it
// only runs when a throwaway target (an emulator) is named explicitly.

func probeSerial(t *testing.T) (*Client, string) {
	t.Helper()
	if os.Getenv("ADBQ_SKIP_DEVICE") == "1" {
		t.Skip("ADBQ_SKIP_DEVICE=1")
	}
	serial := os.Getenv("ADBQ_PROBE_SERIAL")
	if serial == "" {
		t.Skip("set ADBQ_PROBE_SERIAL to run device tests")
	}
	return NewClient(), serial
}

// firstSplitApp returns a user-installed package that is an App Bundle
// install, so the export path is exercised with real splits.
func firstSplitApp(t *testing.T, c *Client, ctx context.Context, serial string) (string, *ApkSet) {
	t.Helper()
	// ADBQ_PROBE_APKS_PKG pins the package, so a specific app can be retested
	// after a change instead of whichever one happens to sort first.
	if pkg := os.Getenv("ADBQ_PROBE_APKS_PKG"); pkg != "" {
		set, err := c.ApkSetOf(ctx, serial, pkg)
		if err != nil {
			t.Fatalf("ApkSetOf(%s): %v", pkg, err)
		}
		if !set.Split {
			t.Fatalf("%s is not a split install", pkg)
		}
		return pkg, set
	}
	apps, err := c.ListApps(ctx, serial, true)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	for _, a := range apps {
		set, err := c.ApkSetOf(ctx, serial, a.Pkg)
		if err != nil || set == nil {
			continue
		}
		if set.Split {
			return a.Pkg, set
		}
	}
	t.Skip("no split-APK app installed on this device")
	return "", nil
}

func TestDeviceExportApks(t *testing.T) {
	c, serial := probeSerial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	pkg, set := firstSplitApp(t, c, ctx, serial)
	want := len(set.Splits) + 1
	dst := filepath.Join(t.TempDir(), set.Suggested)

	if _, err := c.ExportApks(ctx, serial, pkg, dst, func(string) {}); err != nil {
		t.Fatalf("ExportApks: %v", err)
	}
	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatalf("the export is not a readable archive: %v", err)
	}
	defer zr.Close()

	apks, meta := 0, false
	for _, f := range zr.File {
		switch {
		case strings.HasSuffix(strings.ToLower(f.Name), ".apk"):
			apks++
			if f.UncompressedSize64 == 0 {
				t.Errorf("%s is empty in the archive", f.Name)
			}
		case f.Name == "meta.sai_v2.json":
			meta = true
		}
	}
	if apks != want {
		t.Errorf("archive holds %d APKs, the device reports %d", apks, want)
	}
	if !meta {
		t.Error("meta.sai_v2.json missing — third-party installers need it")
	}

	// The export must be installable again: the planner has to accept it and
	// keep more than one APK, otherwise a reinstall loses a split.
	plan, err := c.PlanApkInstall(ctx, serial, dst)
	if err != nil {
		t.Fatalf("PlanApkInstall on our own export: %v", err)
	}
	if len(plan.Install) < 2 || !plan.Split {
		t.Errorf("our own export does not plan as a split install: %+v", plan)
	}
	if !strings.Contains(strings.Join(plan.Commands, "\n"), "install-multiple") {
		t.Errorf("split install must use install-multiple, got %v", plan.Commands)
	}
	// The base has to lead the session or pm rejects it.
	if !strings.EqualFold(filepath.Base(plan.Install[0]), "base.apk") {
		t.Logf("first entry is %q; ranking reorders it at install time", plan.Install[0])
	}
}

func TestDeviceInstallApksRoundTrip(t *testing.T) {
	c, serial := probeSerial(t)
	target := os.Getenv("ADBQ_PROBE_INSTALL_SERIAL")
	if target == "" {
		t.Skip("set ADBQ_PROBE_INSTALL_SERIAL (a throwaway device) to run the install round trip")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	pkg, set := firstSplitApp(t, c, ctx, serial)
	dst := filepath.Join(t.TempDir(), set.Suggested)
	if _, err := c.ExportApks(ctx, serial, pkg, dst, func(string) {}); err != nil {
		t.Fatalf("ExportApks: %v", err)
	}

	// Leave the target as we found it.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		_, _ = c.UninstallApp(cctx, target, pkg)
	})
	_, _ = c.UninstallApp(ctx, target, pkg)

	out, err := c.InstallApkBundle(ctx, target, dst, func(string) {})
	if err != nil {
		t.Fatalf("InstallApkBundle: %v (%s)", err, strings.TrimSpace(out))
	}
	paths, err := c.PathsOfApp(ctx, target, pkg)
	if err != nil {
		t.Fatalf("PathsOfApp after install: %v", err)
	}
	if len(paths) < 2 {
		t.Errorf("the reinstalled app has %d APK(s); the splits did not survive the round trip: %v", len(paths), paths)
	}
}
