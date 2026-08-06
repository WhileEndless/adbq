package adb

import "testing"

func TestParseProcTableSkipsHeaderAndKeepsPackages(t *testing.T) {
	out := "  PID   UID NAME                       \n" +
		"    1     0 init\n" +
		"    2     0 [kthreadd]\n" +
		"  915 10076 com.samsung.android.messaging\n" +
		" 4711  1000 system_server\n" +
		"\n" +
		"garbage line without numbers\n"

	tbl := parseProcTable(out)
	if len(tbl) != 4 {
		t.Fatalf("got %d rows, want 4: %v", len(tbl), tbl)
	}
	if got := tbl[915]; got.Name != "com.samsung.android.messaging" || !got.IsApp() {
		t.Errorf("app row = %+v, want the package name and IsApp()", got)
	}
	if got := tbl[4711]; got.Name != "system_server" || got.IsApp() {
		t.Errorf("system_server = %+v, want IsApp() false", got)
	}
	if got := tbl[2]; got.Name != "[kthreadd]" || got.IsApp() {
		t.Errorf("kernel thread = %+v, want it kept and not an app", got)
	}
}

func TestParseProcTableFallbackFormat(t *testing.T) {
	// The procfs fallback emits "<pid> <uid> <Name:>" with no header. A process
	// name may contain spaces, which must survive the rejoin.
	tbl := parseProcTable("1 0 init\n1270 10098 kworker u16 3\n")
	if got := tbl[1270]; got.Name != "kworker u16 3" {
		t.Errorf("name with spaces = %q, want it preserved whole", got.Name)
	}
	if !tbl[1270].IsApp() {
		t.Errorf("uid 10098 should classify as an app")
	}
	if tbl[1].IsApp() {
		t.Errorf("uid 0 should not classify as an app")
	}
}

func TestIsAppCoversSecondaryUsers(t *testing.T) {
	// uid = userId*100000 + appId. Work-profile (user 10) app 76.
	if !(ProcOwner{UID: 1010076}).IsApp() {
		t.Errorf("secondary-user app uid should classify as an app")
	}
	// user 10's AID_SYSTEM. It is well above 10000 in raw value, so a naive
	// `uid >= 10000` test would call this OS process an app and leak system
	// noise into a work-profile device's feed.
	if (ProcOwner{UID: 1001000}).IsApp() {
		t.Errorf("a secondary user's system uid must not classify as an app")
	}
	if (ProcOwner{UID: 2000}).IsApp() {
		t.Errorf("shell uid should not classify as an app")
	}
	if (ProcOwner{UID: -1}).IsApp() {
		t.Errorf("an unknown uid must not classify as an app")
	}
}

func TestTruncMsgMarksClampedLines(t *testing.T) {
	long := make([]byte, maxMsgLen+50)
	for i := range long {
		long[i] = 'x'
	}
	got := truncMsg(string(long))
	if len(got) <= maxMsgLen || len(got) > maxMsgLen+32 {
		t.Fatalf("truncated length = %d, want just over the %d cap", len(got), maxMsgLen)
	}
	if got[len(got)-1] != ']' {
		t.Errorf("truncated message should be marked, got tail %q", got[len(got)-16:])
	}
	if truncMsg("short") != "short" {
		t.Errorf("short messages must pass through untouched")
	}
}
