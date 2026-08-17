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

// A lookup runs three reads and the preview lists all three, in order — the
// point of the panel is comparing what hosts says with what DNS says.
func TestDNSLookupCommandsListEveryRead(t *testing.T) {
	got := DNSLookupCommands("emulator-5554", "api.example.test", PlainRenderer("emulator-5554"))
	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
	for i, want := range []string{"/etc/hosts", "ping -c 1", "nslookup"} {
		if !strings.Contains(got[i], want) {
			t.Errorf("step %d does not mention %q: %s", i, want, got[i])
		}
	}
}

// safeHost strips anything outside the host-name grammar rather than refusing
// it, so what the preview shows is the sanitised name the lookup will really
// use. What must never happen is a shell metacharacter reaching the rendered
// command — the preview would then be advertising an injection.
func TestDNSLookupCommandsShowTheSanitisedHost(t *testing.T) {
	got := DNSLookupCommands("emulator-5554", "example.test; rm -rf /", PlainRenderer("emulator-5554"))
	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
	// The hosts read is host-name independent; the two that interpolate are not.
	for _, line := range got[1:] {
		if strings.Contains(line, "rm -rf") || strings.Contains(line, ";") {
			t.Errorf("unsafe input reached the command: %s", line)
		}
	}
	// Nothing left after sanitising means there is no lookup to describe.
	for _, bad := range []string{"", "$(){}", "///"} {
		if cmds := DNSLookupCommands("emulator-5554", bad, PlainRenderer("emulator-5554")); cmds != nil {
			t.Errorf("DNSLookupCommands(%q) = %#v, want none", bad, cmds)
		}
	}
}
