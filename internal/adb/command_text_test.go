package adb

import "testing"

func TestQuoteArgOnlyQuotesWhatAShellWouldMangle(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "''",
		"pm":                      "pm",
		"com.example.app":         "com.example.app",
		"/data/local/tmp/x":       "/data/local/tmp/x",
		"emulator-5554":           "emulator-5554",
		"a b":                     "'a b'",
		"/tmp/My Files/a.apk":     "'/tmp/My Files/a.apk'",
		"rm -rf /*":               "'rm -rf /*'",
		"it's":                    `'it'\''s'`,
		"$HOME":                   "'$HOME'",
		"a;b":                     "'a;b'",
		"/sdcard/Download/a.pcap": "/sdcard/Download/a.pcap",
	} {
		if got := quoteArg(in); got != want {
			t.Errorf("quoteArg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeviceCommandTextSpellsOutTheSerial(t *testing.T) {
	got := DeviceCommandText("emulator-5554", "pull", "/data/app/base.apk", "/tmp/My Files/base.apk")
	want := "adb -s emulator-5554 pull /data/app/base.apk '/tmp/My Files/base.apk'"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// Without a serial the command still has to be runnable — just untargeted.
	if got := DeviceCommandText("", "devices"); got != "adb devices" {
		t.Errorf("unexpected untargeted command: %s", got)
	}
}

// A remote command must stay one argument: `adb shell pm clear x` and
// `adb shell 'pm clear x'` differ the moment the command contains a pipe or a
// quote, and what we show has to be what Client.Shell runs.
func TestShellCommandTextKeepsTheRemoteCommandWhole(t *testing.T) {
	got := ShellCommandText("emulator-5554", "pm clear com.example.app")
	want := "adb -s emulator-5554 shell 'pm clear com.example.app'"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestHostCommandTextQuotesPaths(t *testing.T) {
	got := HostCommandText("/opt/Android Sdk/emulator/emulator", "-avd", "test")
	want := "'/opt/Android Sdk/emulator/emulator' -avd test"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
