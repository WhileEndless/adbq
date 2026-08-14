package adb

import (
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
