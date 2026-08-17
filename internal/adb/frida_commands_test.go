package adb

import (
	"strings"
	"testing"
)

func TestFridaCommandsForAKnownServer(t *testing.T) {
	got := FridaCommandsFor("emulator-5554", "/data/local/tmp/frida-server-16.5.9-android-x86_64", 0, PlainRenderer("emulator-5554"))

	if !strings.Contains(got.Start[0], "-l 0.0.0.0:27042 -D") {
		t.Errorf("start: %s", got.Start[0])
	}
	// The redirections are what let the call return; a preview without them
	// describes a command that hangs.
	if !strings.Contains(got.Start[0], "</dev/null") || !strings.Contains(got.Start[0], "2>&1") {
		t.Errorf("start must show the redirections: %s", got.Start[0])
	}
	if !strings.Contains(got.Start[0], "su -c") || !strings.Contains(got.Stop[0], "su -c") {
		t.Error("starting and stopping a server both need root")
	}
	if !strings.Contains(got.Log[0], "adbq-frida-27042.log") {
		t.Errorf("log: %s", got.Log[0])
	}
	// The default port needs no forward, and offering one would be noise.
	if len(got.Forward) != 0 {
		t.Errorf("forward: %#v", got.Forward)
	}
}

func TestFridaCommandsForANonDefaultPortForwards(t *testing.T) {
	got := FridaCommandsFor("emulator-5554", "/data/local/tmp/frida-server", 27500, PlainRenderer("emulator-5554"))
	if len(got.Forward) != 1 || !strings.Contains(got.Forward[0], "forward tcp:0 tcp:27500") {
		t.Errorf("forward: %#v", got.Forward)
	}
	if !strings.Contains(got.Start[0], ":27500") || !strings.Contains(got.Log[0], "27500") {
		t.Error("the chosen port has to appear in the start and log commands")
	}
}

// Before a version is chosen there is no path to show, and inventing one would
// produce a command that pushes to the wrong place.
func TestFridaCommandsForNamesAPlaceholderWhenNothingIsChosen(t *testing.T) {
	got := FridaCommandsFor("emulator-5554", "", 0, PlainRenderer("emulator-5554"))
	if !strings.Contains(strings.Join(got.Install, "\n"), fridaServerPlaceholder) {
		t.Errorf("install: %#v", got.Install)
	}
}

// A session runs a driver on a pinned interpreter, not the frida CLI. Both are
// shown, and the CLI one is labelled as an equivalent rather than as the truth.
func TestFridaSessionCommandsShowBothWhatRunsAndTheEquivalent(t *testing.T) {
	got := FridaSessionCommandsFor("/opt/venv/bin/python3", "/tmp/adbq/frida_driver.py", "/tmp/adbq/job.json",
		"com.example.app", "spawn", 2, "")
	if len(got.Runner) != 1 || !strings.Contains(got.Runner[0], "frida_driver.py /tmp/adbq/job.json") {
		t.Errorf("runner: %#v", got.Runner)
	}
	cli := strings.Join(got.CLI, "\n")
	if !strings.Contains(cli, "# ") {
		t.Error("the CLI line must be marked as an equivalent, not as what ran")
	}
	if !strings.Contains(cli, "frida -U -f com.example.app") || strings.Count(cli, "-l ") != 2 {
		t.Errorf("cli: %s", cli)
	}

	attach := FridaSessionCommandsFor("py", "d.py", "", "com.example.app", "attach", 0, "127.0.0.1:5000")
	acli := strings.Join(attach.CLI, "\n")
	if !strings.Contains(acli, "-H 127.0.0.1:5000") || !strings.Contains(acli, "-n com.example.app") {
		t.Errorf("attach cli: %s", acli)
	}
}
