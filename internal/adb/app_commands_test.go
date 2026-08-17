package adb

import (
	"strings"
	"testing"
)

func TestAppCommandsForCoversEveryAction(t *testing.T) {
	got := AppCommandsFor("emulator-5554", "com.example.app", "com.example.app-1.2", PlainRenderer("emulator-5554"))

	for name, want := range map[string]string{
		"launch":    "adb -s emulator-5554 shell 'monkey -p com.example.app -c android.intent.category.LAUNCHER 1'",
		"forceStop": "adb -s emulator-5554 shell 'am force-stop com.example.app'",
		"clear":     "adb -s emulator-5554 shell 'pm clear com.example.app'",
		"uninstall": "adb -s emulator-5554 uninstall com.example.app",
	} {
		var line string
		switch name {
		case "launch":
			line = got.Launch[0]
		case "forceStop":
			line = got.ForceStop[0]
		case "clear":
			line = got.Clear[0]
		case "uninstall":
			line = got.Uninstall[0]
		}
		if line != want {
			t.Errorf("%s:\n got  %s\n want %s", name, line, want)
		}
	}
}

// The data export is three steps — archive as root, pull, clean up — and all
// three have to be listed (K4), with the pull naming a file the user can paste.
func TestAppCommandsForExportListsEveryStep(t *testing.T) {
	got := AppCommandsFor("emulator-5554", "com.example.app", "com.example.app-1.2", PlainRenderer("emulator-5554"))
	if len(got.ExportData) != 3 {
		t.Fatalf("expected archive+pull+cleanup, got %#v", got.ExportData)
	}
	if !strings.Contains(got.ExportData[0], "su -c") || !strings.Contains(got.ExportData[0], "tar -czf") {
		t.Errorf("the archive step must show it needs root: %s", got.ExportData[0])
	}
	if !strings.HasSuffix(got.ExportData[1], "com.example.app-1.2.tar.gz") {
		t.Errorf("the pull should name the suggested export file: %s", got.ExportData[1])
	}
	if !strings.Contains(got.ExportData[2], "rm /sdcard/adbq-appdata-com.example.app.tar.gz") {
		t.Errorf("the staged archive should be removed again: %s", got.ExportData[2])
	}
}

// Version is best-effort; without it the preview still has to be runnable.
func TestAppCommandsForFallsBackToThePackageName(t *testing.T) {
	got := AppCommandsFor("emulator-5554", "com.example.app", "", PlainRenderer("emulator-5554"))
	if !strings.HasSuffix(got.ExportData[1], "com.example.app.tar.gz") {
		t.Errorf("got %s", got.ExportData[1])
	}
}

// What the preview shows for the archive step has to be what ExportAppData
// runs, so both read the same builder.
func TestAppDataTarRemoteMatchesTheStagedArchive(t *testing.T) {
	if !strings.Contains(appDataTarRemote("com.example.app"), appDataArchive("com.example.app")) {
		t.Error("the tar command must write to the path the pull reads")
	}
}
