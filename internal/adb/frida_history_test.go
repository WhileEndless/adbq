package adb

import "testing"

func TestFridaHistoryDedupAndCap(t *testing.T) {
	s := &FridaStore{} // in-memory: empty historyPath makes save a no-op
	s.RecordHistory(FridaHistoryEntry{Package: "com.a", Mode: "spawn", ScriptIDs: []string{"s-1"}, ScriptNames: []string{"unpin"}})
	s.RecordHistory(FridaHistoryEntry{Package: "com.b", Mode: "attach"})
	s.RecordHistory(FridaHistoryEntry{Package: "com.a", Mode: "spawn", ScriptIDs: []string{"s-1", "s-2"}}) // repeat com.a

	h := s.ListHistory()
	if len(h) != 2 {
		t.Fatalf("want 2 deduped entries, got %d", len(h))
	}
	if h[0].Package != "com.a" {
		t.Errorf("most recent should be com.a, got %s", h[0].Package)
	}
	if h[0].Count != 2 {
		t.Errorf("com.a count should be 2, got %d", h[0].Count)
	}
	if len(h[0].ScriptIDs) != 2 {
		t.Errorf("com.a should carry the latest 2 scripts, got %d", len(h[0].ScriptIDs))
	}

	for i := 0; i < fridaHistoryMax+10; i++ {
		s.RecordHistory(FridaHistoryEntry{Package: "pkg" + itoa(int64(i))})
	}
	if got := len(s.ListHistory()); got > fridaHistoryMax {
		t.Errorf("history exceeded cap: %d > %d", got, fridaHistoryMax)
	}
}
