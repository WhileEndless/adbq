package adb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// HostSettings are the user's host-machine overrides — things adbq would
// otherwise auto-detect. Every field is optional; an empty value means "keep
// auto-detecting", so deleting host.json restores the default behaviour.
type HostSettings struct {
	// SDKRoot overrides Android SDK discovery (ANDROID_HOME et al).
	SDKRoot string `json:"sdkRoot,omitempty"`
	// ADBPath overrides the adb binary Client.Binary() would resolve.
	ADBPath string `json:"adbPath,omitempty"`
	// AVDHome overrides where .avd directories are looked for (ANDROID_AVD_HOME).
	AVDHome string `json:"avdHome,omitempty"`
}

// HostStore persists HostSettings to ~/.adbq/host.json.
//
// Like ProfileStore, it never returns nil and degrades to an in-memory store
// when the config directory can't be resolved — a missing home directory should
// cost the user their preferences, not the whole app.
type HostStore struct {
	mu       sync.RWMutex
	path     string // "" → in-memory only
	settings HostSettings
}

func NewHostStore() *HostStore {
	s := &HostStore{}
	dir, err := configDir()
	if err != nil {
		return s
	}
	s.path = filepath.Join(dir, "host.json")
	s.load()
	return s
}

func (s *HostStore) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return // absent or unreadable → defaults
	}
	var hs HostSettings
	if err := json.Unmarshal(b, &hs); err != nil {
		return // corrupt file → defaults, and the next Set() rewrites it
	}
	s.settings = hs
}

// Get returns a copy of the current settings.
func (s *HostStore) Get() HostSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// Set replaces the settings and persists them.
func (s *HostStore) Set(hs HostSettings) error {
	s.mu.Lock()
	s.settings = hs
	path := s.path
	snapshot := s.settings
	s.mu.Unlock()
	if path == "" {
		return nil
	}
	return atomicWriteJSON(path, snapshot)
}
