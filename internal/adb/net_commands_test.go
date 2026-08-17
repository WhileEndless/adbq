package adb

import (
	"strings"
	"testing"
)

// The escalation is the point of this preview: someone about to let adbq
// remount their system partition should be able to read every attempt first.
func TestCertInstallPlanListsTheWholeEscalation(t *testing.T) {
	plan := CertInstallPlanFor("emulator-5554", true, PlainRenderer("emulator-5554"))
	if plan.Store != "system" {
		t.Errorf("store = %q", plan.Store)
	}
	all := strings.Join(plan.Commands, "\n")
	for _, want := range []string{
		certFilePlaceholder,
		"magisk --remount-system",
		"mount -o rw,remount /system",
		"mount -t tmpfs tmpfs",
		systemCacertsDir + "/" + certNamePlaceholder,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("plan does not mention %q:\n%s", want, all)
		}
	}
	// Every strategy adbq walks has to appear — a preview that lists three of
	// four is a preview that lies about the fourth.
	for _, s := range cacertStrategies(cacertStage, systemCacertsDir+"/"+certNamePlaceholder) {
		if !strings.Contains(all, s.Cmd) {
			t.Errorf("strategy %q missing from the plan", s.Name)
		}
	}
}

// Without root there is no system store to write, and the plan must not imply
// otherwise.
func TestCertInstallPlanWithoutRootOnlyStages(t *testing.T) {
	plan := CertInstallPlanFor("emulator-5554", false, PlainRenderer("emulator-5554"))
	if plan.Store != "user" || plan.Persistent {
		t.Errorf("unexpected plan: %#v", plan)
	}
	all := strings.Join(plan.Commands, "\n")
	if strings.Contains(all, "remount") || strings.Contains(all, systemCacertsDir) {
		t.Errorf("no system-store commands belong here:\n%s", all)
	}
	if !strings.Contains(all, "/sdcard/Download/") {
		t.Errorf("the staged file should be named:\n%s", all)
	}
}

func TestHostsApplyPlanStagesTheContentItWillWrite(t *testing.T) {
	plan := HostsApplyPlanFor("emulator-5554", "/system/etc/hosts", "10.0.0.1 example.test", PlainRenderer("emulator-5554"))
	all := strings.Join(plan.Commands, "\n")
	if !strings.Contains(all, "10.0.0.1 example.test") {
		t.Errorf("the staged content is what gets written:\n%s", all)
	}
	for _, s := range hostsStrategies(hostsStage, "/system/etc/hosts") {
		if !strings.Contains(all, s.Cmd) {
			t.Errorf("strategy %q missing from the plan", s.Name)
		}
	}
	for _, want := range []string{"md5sum /system/etc/hosts", "/data/adb/modules/adbq-hosts", "ndc resolver"} {
		if !strings.Contains(all, want) {
			t.Errorf("plan does not mention %q", want)
		}
	}
}

// The hash in the Magisk module description is computed over the normalized
// content, so a preview built from raw CRLF input would name a hash the write
// never produces.
func TestHostsApplyPlanHashesNormalizedContent(t *testing.T) {
	plan := HostsApplyPlanFor("emulator-5554", "/etc/hosts", "10.0.0.1 example.test\r\n", PlainRenderer("emulator-5554"))
	want := md5sum("10.0.0.1 example.test\n")
	if !strings.Contains(strings.Join(plan.Commands, "\n"), want) {
		t.Errorf("expected md5 %s in the plan", want)
	}
}

func TestNetCommandsForCoversProxyAndFlush(t *testing.T) {
	got := NetCommandsFor("emulator-5554", "10.0.0.2:8080", PlainRenderer("emulator-5554"))
	if !strings.Contains(got.Proxy[0], "http_proxy 10.0.0.2:8080") {
		t.Errorf("proxy: %s", got.Proxy[0])
	}
	// Clearing is a write of ":0", not an unset — the preview has to say so.
	if !strings.HasSuffix(got.ClearProxy[0], "http_proxy :0") {
		t.Errorf("clear: %s", got.ClearProxy[0])
	}
	if !strings.Contains(got.FlushDNS[0], "su -c") {
		t.Errorf("flushing netd needs root: %s", got.FlushDNS[0])
	}
	if !strings.Contains(got.Connections[0], "/proc/net/tcp") {
		t.Errorf("connections: %s", got.Connections[0])
	}
}
