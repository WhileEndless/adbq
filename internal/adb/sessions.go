package adb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session is one persistent long-running operation that adbq launched on a
// device. We persist these to disk so they survive an app crash/restart and
// can be reconciled on next launch.
type Session struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // "capture" | "screen-record" | "frida"
	Serial     string `json:"serial"`
	StartedAt  int64  `json:"startedAt"`
	RemoteFile string `json:"remoteFile,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Marker     string `json:"marker,omitempty"` // additional fingerprint (e.g. bpf filter)
}

// SessionStore is thread-safe JSON-on-disk persistence at ~/.adbq/sessions.json.
type SessionStore struct {
	mu   sync.Mutex
	path string
	data map[string]Session
}

func NewSessionStore() (*SessionStore, error) {
	d, err := configDir()
	if err != nil {
		return nil, err
	}
	p := filepath.Join(d, "sessions.json")
	s := &SessionStore{path: p, data: map[string]Session{}}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	return s, nil
}

func (s *SessionStore) save() error {
	b, _ := json.MarshalIndent(s.data, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}

// Put adds or replaces a session by ID and persists.
func (s *SessionStore) Put(sess Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.StartedAt == 0 {
		sess.StartedAt = time.Now().Unix()
	}
	s.data[sess.ID] = sess
	_ = s.save()
}

// Remove drops a session by ID.
func (s *SessionStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	_ = s.save()
}

// List returns all known sessions.
func (s *SessionStore) List() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.data))
	for _, v := range s.data {
		out = append(out, v)
	}
	return out
}

// FindByKindSerial returns the session matching kind+serial, if any.
func (s *SessionStore) FindByKindSerial(kind, serial string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.data {
		if v.Kind == kind && v.Serial == serial {
			return v, true
		}
	}
	return Session{}, false
}
