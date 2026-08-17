package adb

import (
	"strings"
	"testing"
)

func TestDeviceCommandsForCoversTheOverviewActions(t *testing.T) {
	got := DeviceCommandsFor("emulator-5554", 5555, 30, "scrcpy -s emulator-5554", PlainRenderer("emulator-5554"))
	for name, want := range map[string]string{
		"reboot":     "adb -s emulator-5554 reboot",
		"recovery":   "adb -s emulator-5554 reboot recovery",
		"bootloader": "adb -s emulator-5554 reboot bootloader",
		"tcpip":      "adb -s emulator-5554 tcpip 5555",
	} {
		var line string
		switch name {
		case "reboot":
			line = got.Reboot[0]
		case "recovery":
			line = got.RebootRecovery[0]
		case "bootloader":
			line = got.RebootBootloader[0]
		case "tcpip":
			line = got.Tcpip[0]
		}
		if line != want {
			t.Errorf("%s:\n got  %s\n want %s", name, line, want)
		}
	}
	// A screenshot is binary, so it must not be shown going through `shell`.
	if !strings.Contains(got.Screenshot[0], "exec-out") {
		t.Errorf("screenshot: %s", got.Screenshot[0])
	}
	if got.Scrcpy[0] != "scrcpy -s emulator-5554" {
		t.Errorf("scrcpy: %s", got.Scrcpy[0])
	}
}

// Recording is start, stop, pull, clean up — a single screenrecord line would
// imply the file appears on its own.
func TestDeviceCommandsForRecordingListsEveryStep(t *testing.T) {
	got := DeviceCommandsFor("emulator-5554", 0, 45, "", PlainRenderer("emulator-5554"))
	all := strings.Join(got.ScreenRecord, "\n")
	for _, want := range []string{"screenrecord --time-limit 45", "pkill -INT screenrecord", "pull /sdcard/adbq-screenrecord.mp4", "rm /sdcard/adbq-screenrecord.mp4"} {
		if !strings.Contains(all, want) {
			t.Errorf("recording plan is missing %q:\n%s", want, all)
		}
	}
	// No scrcpy installed means no scrcpy command, not an empty-looking one.
	if len(got.Scrcpy) != 0 {
		t.Errorf("scrcpy: %#v", got.Scrcpy)
	}
}

func TestLogcatCommandsForCarriesTheFilters(t *testing.T) {
	stream, clear := LogcatCommandsFor("emulator-5554", 4242, 500, PlainRenderer("emulator-5554"))
	want := "adb -s emulator-5554 logcat -v threadtime --pid=4242 -T 500 '*:V'"
	if stream[0] != want {
		t.Errorf("stream:\n got  %s\n want %s", stream[0], want)
	}
	if clear[0] != "adb -s emulator-5554 logcat -c" {
		t.Errorf("clear: %s", clear[0])
	}
	// Unfiltered, the pid and tail flags must be absent rather than zeroed.
	bare, _ := LogcatCommandsFor("emulator-5554", 0, 0, PlainRenderer("emulator-5554"))
	if strings.Contains(bare[0], "--pid") || strings.Contains(bare[0], "-T") {
		t.Errorf("bare stream: %s", bare[0])
	}
}

// The process table falls back to the shell user when su is denied, and the
// preview has to follow that instead of always claiming root.
func TestProcessCommandsFollowsWhoIsReading(t *testing.T) {
	rooted := ProcessCommands("emulator-5554", true, PlainRenderer("emulator-5554"))
	if !strings.Contains(rooted[0], "su -c") {
		t.Errorf("rooted: %s", rooted[0])
	}
	plain := ProcessCommands("emulator-5554", false, PlainRenderer("emulator-5554"))
	if strings.Contains(plain[0], "su -c") {
		t.Errorf("rootless: %s", plain[0])
	}
	if !strings.Contains(plain[0], "/proc/stat") {
		t.Errorf("the procfs sweep should be visible: %s", plain[0])
	}
}

func TestCaptureCommandForShowsTheRealInvocation(t *testing.T) {
	wrap := func(inner string) string { return "su -c " + shQuote(inner) }
	got := CaptureCommandFor("emulator-5554", "/data/local/tmp/tcpdump", "wlan0", "tcp port 443", wrap)
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	for _, want := range []string{"exec-out", "wlan0", "-U -s 0 -w -", "tcp port 443", "su -c"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("capture command is missing %q:\n%s", want, got[0])
		}
	}
	// Without tcpdump there is nothing truthful to show.
	if len(CaptureCommandFor("emulator-5554", "", "any", "", wrap)) != 0 {
		t.Error("no tcpdump, no command")
	}
}

// The file-capture panel used to print a command it made up in the frontend,
// naming a file the capture never writes. These are the real ones.
func TestCaptureCommandsForNameTheRealFileAndSignal(t *testing.T) {
	got := CaptureCommandsFor("emulator-5554", "/data/local/tmp/tcpdump", "wlan0", "'tcp port 443'", PlainRenderer("emulator-5554"))
	if !strings.Contains(got.Start[0], "nohup") || !strings.Contains(got.Start[0], capturePath) {
		t.Errorf("start: %s", got.Start[0])
	}
	// A filter the user wrapped in quotes themselves must not be quoted twice —
	// tcpdump would then read the quotes as part of the expression. Checked on
	// the remote command, before the su/shell layers add their own escaping.
	inner := captureStartRemote("/data/local/tmp/tcpdump", "wlan0", strings.Trim("'tcp port 443'", "'\""))
	if !strings.Contains(inner, `'tcp port 443'`) || strings.Contains(inner, `''tcp port 443''`) {
		t.Errorf("filter quoting: %s", inner)
	}
	// SIGINT, not SIGKILL: the pcap header is only written on a clean stop.
	if !strings.Contains(got.Stop[0], "kill -INT") {
		t.Errorf("stop: %s", got.Stop[0])
	}
	if !strings.Contains(got.Pull[0], "pull "+capturePath) {
		t.Errorf("pull: %s", got.Pull[0])
	}
	// No tcpdump on the device means there is no start command to promise.
	none := CaptureCommandsFor("emulator-5554", "", "any", "", PlainRenderer("emulator-5554"))
	if len(none.Start) != 0 {
		t.Errorf("start without tcpdump: %#v", none.Start)
	}
}

func TestTcpdumpInstallCommandsCoverPushChmodAndProbe(t *testing.T) {
	got := TcpdumpInstallCommands("emulator-5554", PlainRenderer("emulator-5554"))
	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
	for i, want := range []string{tcpdumpInstallPath, "chmod 755", "--version"} {
		if !strings.Contains(got[i], want) {
			t.Errorf("step %d (%s) does not mention %q", i, got[i], want)
		}
	}
}
