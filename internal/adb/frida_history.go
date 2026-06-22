package adb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Frida instrumentation history: a recents list so the user can re-launch a
// previously-instrumented app from the Frida tab without going back to Apps.
// Deduplicated by package (one row per app, newest config), capped, persisted to
// history.json next to the other Frida config.

const fridaHistoryMax = 100

// FridaHistoryEntry records one launched instrumentation for the recents list.
type FridaHistoryEntry struct {
	Package     string   `json:"package"`
	Mode        string   `json:"mode"`       // spawn | attach
	RuntimeVer  string   `json:"runtimeVer"` // device frida-server version used
	ScriptIDs   []string `json:"scriptIds"`
	ScriptNames []string `json:"scriptNames"` // resolved at record time, for display
	LastRun     int64    `json:"lastRun"`
	Count       int      `json:"count"`
}

func (s *FridaStore) initHistory() {
	dir, err := fridaDataDir()
	if err != nil {
		return // in-memory only
	}
	s.historyPath = filepath.Join(dir, "history.json")
	if b, err := os.ReadFile(s.historyPath); err == nil {
		var h []FridaHistoryEntry
		if json.Unmarshal(b, &h) == nil {
			s.history = h
		}
	}
}

func (s *FridaStore) saveHistory() error {
	if s.historyPath == "" {
		return nil
	}
	return atomicWriteJSON(s.historyPath, s.history)
}

// RecordHistory upserts an entry by package: an existing app is updated with the
// latest config, its run count bumped, and moved to the front. The list is
// capped at the newest fridaHistoryMax apps.
func (s *FridaStore) RecordHistory(e FridaHistoryEntry) {
	if e.Package == "" {
		return
	}
	if e.Mode != "attach" {
		e.Mode = "spawn"
	}
	if e.ScriptIDs == nil {
		e.ScriptIDs = []string{}
	}
	if e.ScriptNames == nil {
		e.ScriptNames = []string{}
	}
	e.LastRun = time.Now().Unix()

	s.mu.Lock()
	defer s.mu.Unlock()
	rest := make([]FridaHistoryEntry, 0, len(s.history))
	prevCount := 0
	for _, h := range s.history {
		if h.Package == e.Package {
			prevCount = h.Count
			continue
		}
		rest = append(rest, h)
	}
	e.Count = prevCount + 1
	s.history = append([]FridaHistoryEntry{e}, rest...)
	if len(s.history) > fridaHistoryMax {
		s.history = s.history[:fridaHistoryMax]
	}
	_ = s.saveHistory()
}

// ListHistory returns the recents, newest first.
func (s *FridaStore) ListHistory() []FridaHistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FridaHistoryEntry, len(s.history))
	copy(out, s.history)
	for i := range out {
		if out[i].ScriptIDs == nil {
			out[i].ScriptIDs = []string{}
		}
		if out[i].ScriptNames == nil {
			out[i].ScriptNames = []string{}
		}
	}
	return out
}

// RemoveHistory drops a single package's recents entry.
func (s *FridaStore) RemoveHistory(pkg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rest := make([]FridaHistoryEntry, 0, len(s.history))
	for _, h := range s.history {
		if h.Package != pkg {
			rest = append(rest, h)
		}
	}
	s.history = rest
	return s.saveHistory()
}

// ClearHistory empties the recents list.
func (s *FridaStore) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	return s.saveHistory()
}
