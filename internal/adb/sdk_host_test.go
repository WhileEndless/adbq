package adb

import (
	"context"
	"os"
	"testing"
)

// Host tests exercise the real Android SDK installed on this computer. They are
// opt-in — CI has no SDK, and probing shells out to the emulator binary:
//
//	ADBQ_PROBE_SDK=1 go test ./internal/adb/ -run TestHostSDK -v
func hostSDK(t *testing.T) *SDKManager {
	t.Helper()
	if os.Getenv("ADBQ_PROBE_SDK") == "" {
		t.Skip("set ADBQ_PROBE_SDK=1 to run host SDK tests")
	}
	m := NewSDKManager(NewHostStore())
	if !m.Available() {
		t.Skipf("no Android SDK on this host: %s", m.Info().Error)
	}
	return m
}

func TestHostSDKDetection(t *testing.T) {
	m := hostSDK(t)
	i := m.Info()
	t.Logf("root=%s (%s)", i.SDKRoot, i.Source)
	t.Logf("emulator=%s ver=%s accel=%v (%s)", i.Emulator, i.EmulatorVer, i.Accelerated, i.AccelNote)
	t.Logf("avdmanager=%s", i.AVDManager)
	t.Logf("sdkmanager=%s", i.SDKManager)
	t.Logf("avdHome=%s", i.AVDHome)
	t.Logf("studio=%s ver=%s", i.StudioPath, i.StudioVer)

	if i.SDKRoot == "" {
		t.Error("an available SDK must report a root")
	}
	if i.EmulatorVer == "" {
		t.Error("emulator -version must yield a version string")
	}
	if i.AVDHome == "" {
		t.Error("AVD home must resolve even when the SDK lives elsewhere")
	}
	if i.Error != "" {
		t.Errorf("available SDK must not carry an error: %s", i.Error)
	}
}

func TestHostInstalledSystemImages(t *testing.T) {
	m := hostSDK(t)
	imgs := NewPackageManager(m).ListInstalledImages()
	if len(imgs) == 0 {
		t.Skip("no system images installed on this host")
	}
	for _, i := range imgs {
		t.Logf("%-58s api=%-3d tag=%-24s abi=%-12s play=%-5v rev=%s",
			i.Pkg, i.API, i.Tag, i.ABI, i.PlayStore, i.Revision)
		if !i.Installed || i.Location == "" {
			t.Errorf("%s: read from disk but not marked installed", i.Pkg)
		}
		if len(i.Commands) == 0 {
			t.Errorf("%s: no command shown (CLAUDE.md §4.1)", i.Pkg)
		}
	}
}

func TestHostDeviceProfiles(t *testing.T) {
	m := hostSDK(t)
	profiles, err := NewEmulatorManager(m, NewClient()).ListDeviceProfiles(context.Background())
	if err != nil {
		t.Skipf("avdmanager unavailable: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("avdmanager listed no device profiles")
	}
	t.Logf("%d device profiles, first: %+v", len(profiles), profiles[0])
	for _, p := range profiles {
		if p.ID == "" || p.Name == "" {
			t.Errorf("incomplete profile: %+v", p)
		}
	}
}
