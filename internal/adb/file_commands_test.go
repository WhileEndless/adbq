package adb

import (
	"strings"
	"testing"
)

func TestFileCommandsForASelectedFile(t *testing.T) {
	got := FileCommandsFor("emulator-5554", FileCommandRequest{
		Dir: "/data/local/tmp", Name: "notes.txt",
	}, PlainRenderer("emulator-5554"))

	if got.Path != "/data/local/tmp/notes.txt" {
		t.Errorf("path = %q", got.Path)
	}
	if want := "adb -s emulator-5554 shell 'ls -lAp /data/local/tmp'"; got.List[0] != want {
		t.Errorf("list:\n got  %s\n want %s", got.List[0], want)
	}
	if want := "adb -s emulator-5554 pull /data/local/tmp/notes.txt notes.txt"; got.Pull[0] != want {
		t.Errorf("pull:\n got  %s\n want %s", got.Pull[0], want)
	}
	if want := "adb -s emulator-5554 shell 'rm -f /data/local/tmp/notes.txt'"; got.Delete[0] != want {
		t.Errorf("delete:\n got  %s\n want %s", got.Delete[0], want)
	}
}

// Deleting a directory is recursive, which is the whole reason the preview has
// to be in the confirm dialog.
func TestFileCommandsForADirectoryDeletesRecursively(t *testing.T) {
	got := FileCommandsFor("emulator-5554", FileCommandRequest{
		Dir: "/sdcard", Name: "Download", IsDir: true,
	}, PlainRenderer("emulator-5554"))
	if !strings.Contains(got.Delete[0], "rm -rf /sdcard/Download") {
		t.Errorf("delete: %s", got.Delete[0])
	}
	if len(got.Pull) != 0 {
		t.Errorf("a directory has no single-file pull: %v", got.Pull)
	}
}

func TestFileCommandsForRootShowsTheSuWrapper(t *testing.T) {
	got := FileCommandsFor("emulator-5554", FileCommandRequest{
		Dir: "/data/data", Name: "x", IsDir: true, AsRoot: true,
	}, PlainRenderer("emulator-5554"))
	for _, line := range [][]string{got.List, got.Delete, got.Mkdir} {
		if !strings.Contains(line[0], "su -c") {
			t.Errorf("root actions must show they run through su: %s", line[0])
		}
	}
	// A transfer runs on this computer, so it must NOT claim to use su.
	if strings.Contains(got.Push[0], "su -c") {
		t.Errorf("push is host-side: %s", got.Push[0])
	}
}

// A push can carry chmod and chown afterwards; all three steps belong in the
// preview, in the order PushFileWithOptions runs them.
func TestFileCommandsForPushListsItsFollowUpSteps(t *testing.T) {
	got := FileCommandsFor("emulator-5554", FileCommandRequest{
		Dir: "/data/local/tmp", Mode: "755", Owner: "shell:shell",
	}, PlainRenderer("emulator-5554"))
	if len(got.Push) != 3 {
		t.Fatalf("expected push+chmod+chown, got %#v", got.Push)
	}
	if !strings.Contains(got.Push[1], "chmod 755 ") || !strings.Contains(got.Push[1], pushPlaceholder) ||
		!strings.Contains(got.Push[2], "chown shell:shell ") || !strings.Contains(got.Push[2], pushPlaceholder) {
		t.Errorf("follow-up steps target the pushed file: %#v", got.Push)
	}
}

func TestFileCommandsPlaceholdersCannotBeMistakenForPaths(t *testing.T) {
	got := FileCommandsFor("emulator-5554", FileCommandRequest{Dir: "/sdcard"}, PlainRenderer("emulator-5554"))
	if !strings.Contains(got.Push[0], pushPlaceholder) {
		t.Errorf("push: %s", got.Push[0])
	}
	if !strings.Contains(got.Mkdir[0], newFolderPlaceholder) {
		t.Errorf("mkdir: %s", got.Mkdir[0])
	}
}
