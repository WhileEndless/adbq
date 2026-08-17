package adb

import "testing"

func TestForwardCommandsForForwardKeepsHostFirst(t *testing.T) {
	got := ForwardCommandsFor("emulator-5554", "forward", "tcp:9222", "localabstract:devtools")
	if want := "adb -s emulator-5554 forward tcp:9222 localabstract:devtools"; got.Add[0] != want {
		t.Errorf("add:\n got  %s\n want %s", got.Add[0], want)
	}
	if want := "adb -s emulator-5554 forward --remove tcp:9222"; got.Remove[0] != want {
		t.Errorf("remove:\n got  %s\n want %s", got.Remove[0], want)
	}
	if want := "adb -s emulator-5554 forward --list"; got.List[0] != want {
		t.Errorf("list:\n got  %s\n want %s", got.List[0], want)
	}
}

// A reverse is declared device-side first and removed by its device spec —
// swapping those is precisely the mistake the preview exists to catch.
func TestForwardCommandsForReverseSwapsTheOrder(t *testing.T) {
	got := ForwardCommandsFor("emulator-5554", "reverse", "tcp:8080", "tcp:8081")
	if want := "adb -s emulator-5554 reverse tcp:8081 tcp:8080"; got.Add[0] != want {
		t.Errorf("add:\n got  %s\n want %s", got.Add[0], want)
	}
	if want := "adb -s emulator-5554 reverse --remove tcp:8081"; got.Remove[0] != want {
		t.Errorf("remove:\n got  %s\n want %s", got.Remove[0], want)
	}
}

// Half-typed input must not render a command that would fail if pasted.
func TestForwardCommandsForOmitsIncompleteMappings(t *testing.T) {
	got := ForwardCommandsFor("emulator-5554", "forward", "tcp:8080", "")
	if len(got.Add) != 0 {
		t.Errorf("no add command without both ends: %v", got.Add)
	}
	if len(got.Remove) == 0 || len(got.List) == 0 {
		t.Error("remove and list only need the local spec")
	}
}

func TestForwardCommandsForRowsStaysAligned(t *testing.T) {
	rows := []Forward{{Local: "tcp:1", Remote: "tcp:2"}, {Local: "tcp:3", Remote: "tcp:4"}}
	got := ForwardCommandsForRows("emulator-5554", "forward", rows)
	if len(got) != len(rows) {
		t.Fatalf("got %d entries for %d rows", len(got), len(rows))
	}
	if got[1].Local != "tcp:3" {
		t.Errorf("row order changed: %#v", got)
	}
}
