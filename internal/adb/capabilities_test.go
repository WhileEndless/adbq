package adb

import "testing"

func TestParseCapabilities(t *testing.T) {
	out := "33@@@13@@@arm64-v8a@@@arm64-v8a,armeabi-v7a,armeabi@@@Enforcing@@@iptables\nip\ntcpdump\nsu"
	caps := parseCapabilities(out)
	if caps.SDK != 33 {
		t.Errorf("SDK = %d, want 33", caps.SDK)
	}
	if caps.Release != "13" {
		t.Errorf("Release = %q, want 13", caps.Release)
	}
	if caps.ABI != "arm64-v8a" {
		t.Errorf("ABI = %q", caps.ABI)
	}
	if len(caps.ABIList) != 3 || caps.ABIList[2] != "armeabi" {
		t.Errorf("ABIList = %v", caps.ABIList)
	}
	if !caps.Bits64 {
		t.Error("Bits64 = false, want true")
	}
	if caps.SELinux != "enforcing" {
		t.Errorf("SELinux = %q, want enforcing", caps.SELinux)
	}
	if !caps.Supports("iptables") || !caps.Supports("tcpdump") || caps.Supports("nft") {
		t.Errorf("Has = %v", caps.Has)
	}
	if !caps.AndroidAtLeast(30) || caps.AndroidAtLeast(34) {
		t.Error("AndroidAtLeast wrong")
	}
	if !caps.SELinuxEnforcing() {
		t.Error("SELinuxEnforcing = false")
	}
}

func TestParseCapabilities32bitOldDevice(t *testing.T) {
	// API 21 toolbox: no getenforce, no abilist, no nft/tcpdump.
	out := "21@@@5.0.2@@@armeabi-v7a@@@@@@@@@su\niptables\nip"
	caps := parseCapabilities(out)
	if caps.SDK != 21 || caps.Bits64 {
		t.Errorf("SDK=%d bits64=%v", caps.SDK, caps.Bits64)
	}
	if caps.SELinux != "" {
		t.Errorf("SELinux = %q, want empty", caps.SELinux)
	}
	if caps.Supports("tcpdump") || caps.Supports("nft") {
		t.Error("should not report tcpdump/nft on this device")
	}
	if !caps.Supports("su") {
		t.Error("su should be present")
	}
}

func TestCapabilitiesNilSafe(t *testing.T) {
	var caps *Capabilities
	if caps.Supports("su") || caps.AndroidAtLeast(1) || caps.SELinuxEnforcing() {
		t.Error("nil Capabilities methods must be false")
	}
}
