package adb

import "testing"

func TestProfileStoreRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate ~/.adbq

	s, err := NewProfileStore()
	if err != nil {
		t.Fatalf("NewProfileStore: %v", err)
	}
	p := Profile{Name: "Burp Pentest"}
	p.Proxy = ProxyStep{ProfileStep: ProfileStep{Enabled: true}, HostPort: "127.0.0.1:8080"}
	p.Frida = FridaStep{ProfileStep: ProfileStep{Enabled: true}, Version: "16.4.8", AutoArch: true, Start: true}
	saved, err := s.SaveProfile(p)
	if err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	if saved.ID == "" || saved.CreatedAt == 0 || saved.UpdatedAt == 0 {
		t.Fatalf("SaveProfile did not stamp id/timestamps: %+v", saved)
	}

	// Reload from disk in a fresh store.
	s2, err := NewProfileStore()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := s2.GetProfile(saved.ID)
	if !ok {
		t.Fatalf("profile %s missing after reload", saved.ID)
	}
	if got.Name != "Burp Pentest" || got.Proxy.HostPort != "127.0.0.1:8080" || !got.Frida.Start {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	// Updating an existing profile keeps CreatedAt, bumps UpdatedAt.
	got.Name = "Renamed"
	again, err := s2.SaveProfile(got)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if again.ID != saved.ID || again.CreatedAt != saved.CreatedAt {
		t.Errorf("update changed id/createdAt: %+v vs %+v", again, saved)
	}
}

func TestProfileDeleteUnbindsDevices(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := NewProfileStore()
	p, _ := s.SaveProfile(Profile{Name: "X"})

	s.UpsertDeviceSeen(DeviceRecord{Key: "HW123", AdbSerial: "emulator-5554", Model: "Pixel"})
	if err := s.BindProfile("HW123", p.ID); err != nil {
		t.Fatalf("BindProfile: %v", err)
	}
	if rec, _ := s.GetDevice("HW123"); rec.BoundProfileID != p.ID {
		t.Fatalf("binding not stored: %+v", rec)
	}

	if err := s.DeleteProfile(p.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if rec, _ := s.GetDevice("HW123"); rec.BoundProfileID != "" {
		t.Errorf("device still bound to deleted profile: %+v", rec)
	}
	// LastSeen metadata preserved across upserts (binding survives re-seen).
	s.UpsertDeviceSeen(DeviceRecord{Key: "HW123", AdbSerial: "192.168.1.5:5555"})
	if rec, _ := s.GetDevice("HW123"); rec.Model != "Pixel" {
		t.Errorf("UpsertDeviceSeen lost metadata: %+v", rec)
	}
}

func TestDeviceKeyFallback(t *testing.T) {
	if got := DeviceKey(&Device{ID: "emulator-5554", HardwareSerial: "HW999"}); got != "HW999" {
		t.Errorf("expected hardware serial, got %q", got)
	}
	if got := DeviceKey(&Device{ID: "emulator-5554"}); got != "emulator-5554" {
		t.Errorf("expected adb id fallback, got %q", got)
	}
	if got := DeviceKey(&Device{ID: "abc", HardwareSerial: "unknown"}); got != "abc" {
		t.Errorf("'unknown' should fall back to adb id, got %q", got)
	}
}
