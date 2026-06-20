package adb

import "testing"

func TestFridaScriptStoreCRUD(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	s, err := NewFridaStore()
	if err != nil {
		t.Fatalf("NewFridaStore: %v", err)
	}

	// Create.
	sc, err := s.SaveScript(FridaScript{Name: "hook", Description: "d", Source: "console.log(1)"})
	if err != nil {
		t.Fatalf("SaveScript: %v", err)
	}
	if sc.ID == "" || sc.Origin != "local" || sc.CreatedAt == 0 {
		t.Fatalf("unexpected created script: %+v", sc)
	}

	// List returns metadata only (no source body).
	list := s.ListScripts()
	if len(list) != 1 || list[0].Source != "" {
		t.Fatalf("ListScripts: %+v", list)
	}

	// Get returns the source body.
	got, err := s.GetScript(sc.ID)
	if err != nil || got.Source != "console.log(1)" {
		t.Fatalf("GetScript: %+v err=%v", got, err)
	}

	// Update preserves CreatedAt and rewrites the body.
	got.Source = "console.log(2)"
	got.Name = "hook2"
	up, err := s.SaveScript(got)
	if err != nil || up.CreatedAt != sc.CreatedAt {
		t.Fatalf("update: %+v err=%v", up, err)
	}
	g2, _ := s.GetScript(sc.ID)
	if g2.Source != "console.log(2)" || g2.Name != "hook2" {
		t.Fatalf("updated get: %+v", g2)
	}

	// Bind to an app, then delete the script — it must detach.
	if err := s.SetAppScripts("com.x", []string{sc.ID}, "spawn", ""); err != nil {
		t.Fatalf("SetAppScripts: %v", err)
	}
	if b := s.GetAppScripts("com.x"); len(b.ScriptIDs) != 1 || b.ScriptIDs[0] != sc.ID || b.Mode != "spawn" {
		t.Fatalf("binding: %+v", b)
	}
	if err := s.DeleteScript(sc.ID); err != nil {
		t.Fatalf("DeleteScript: %v", err)
	}
	if len(s.ListScripts()) != 0 {
		t.Fatal("script not deleted")
	}
	if b := s.GetAppScripts("com.x"); len(b.ScriptIDs) != 0 {
		t.Fatalf("deleted script still bound: %+v", b)
	}

	// Persistence across a reload.
	if _, err := s.SaveScript(FridaScript{Name: "persisted", Source: "x"}); err != nil {
		t.Fatalf("SaveScript persisted: %v", err)
	}
	s2, _ := NewFridaStore()
	if len(s2.ListScripts()) != 1 {
		t.Fatal("script not persisted across reload")
	}
}
