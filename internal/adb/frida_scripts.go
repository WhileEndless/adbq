package adb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sha256Hex returns the hex-encoded SHA256 of a string. Used to fingerprint
// CodeShare script sources so re-fetches can detect upstream changes.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Frida script library: user-authored (or CodeShare-imported) JS instrumentation
// scripts and the per-app bindings that say which scripts to load when launching
// an app under Frida. Metadata lives in scripts.json; each script's source is a
// .js sidecar so the in-app editor reads/writes real files. Bindings are
// package-keyed (device-independent) in app-scripts.json.

// FridaScript is one library entry. Source is populated only by GetScript.
type FridaScript struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Origin         string `json:"origin"` // "local" | "codeshare"
	CodeshareOwner string `json:"codeshareOwner,omitempty"`
	CodeshareSlug  string `json:"codeshareSlug,omitempty"`
	SourceSha      string `json:"sourceSha,omitempty"` // sha256 of source at import/sync (codeshare)
	Trusted        bool   `json:"trusted"`             // user reviewed an imported script
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
	Source         string `json:"source,omitempty"`
}

// AppScripts binds a package to the scripts loaded when launching it under Frida.
type AppScripts struct {
	Package   string   `json:"package"`
	ScriptIDs []string `json:"scriptIds"`
	Mode      string   `json:"mode"` // "spawn" | "attach"
	VenvVer   string   `json:"venvVer,omitempty"`
}

func (s *FridaStore) initScripts() {
	s.scripts = map[string]FridaScript{}
	s.appScripts = map[string]AppScripts{}
	dir, err := fridaDataDir()
	if err != nil {
		return // in-memory only
	}
	s.scriptsPath = filepath.Join(dir, "scripts.json")
	s.appScriptsPath = filepath.Join(dir, "app-scripts.json")
	if sdir, err := fridaScriptsDir(); err == nil {
		s.scriptsDir = sdir
	}
	if b, err := os.ReadFile(s.scriptsPath); err == nil {
		var idx map[string]FridaScript
		if json.Unmarshal(b, &idx) == nil && idx != nil {
			s.scripts = idx
		}
	}
	for id := range s.scripts {
		var n int
		if _, err := fmt.Sscanf(id, "s-%d", &n); err == nil && n > s.scriptSeq {
			s.scriptSeq = n
		}
	}
	if b, err := os.ReadFile(s.appScriptsPath); err == nil {
		var m map[string]AppScripts
		if json.Unmarshal(b, &m) == nil && m != nil {
			s.appScripts = m
		}
	}
	s.initHistory()
}

func (s *FridaStore) saveScriptsIndex() error {
	if s.scriptsPath == "" {
		return nil
	}
	return atomicWriteJSON(s.scriptsPath, s.scripts)
}

func (s *FridaStore) saveAppScripts() error {
	if s.appScriptsPath == "" {
		return nil
	}
	return atomicWriteJSON(s.appScriptsPath, s.appScripts)
}

func scriptSidecar(dir, id string) string { return filepath.Join(dir, id+".js") }

// ListScripts returns script metadata (no source), most-recently-updated first.
func (s *FridaStore) ListScripts() []FridaScript {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FridaScript, 0, len(s.scripts))
	for _, sc := range s.scripts {
		sc.Source = ""
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// GetScript returns one script with its source body loaded from the sidecar.
func (s *FridaStore) GetScript(id string) (FridaScript, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc, ok := s.scripts[id]
	if !ok {
		return FridaScript{}, fmt.Errorf("script %s not found", id)
	}
	if s.scriptsDir != "" {
		if b, err := os.ReadFile(scriptSidecar(s.scriptsDir, id)); err == nil {
			sc.Source = string(b)
		}
	}
	return sc, nil
}

// SaveScript creates (empty ID) or updates a script: source → sidecar, metadata
// → index. CreatedAt and CodeShare provenance are preserved across updates.
func (s *FridaStore) SaveScript(in FridaScript) (FridaScript, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if in.ID == "" {
		s.scriptSeq++
		in.ID = fmt.Sprintf("s-%d", s.scriptSeq)
		in.CreatedAt = now
		if in.Origin == "" {
			in.Origin = "local"
		}
	} else {
		prev, ok := s.scripts[in.ID]
		if !ok {
			return FridaScript{}, fmt.Errorf("script %s not found", in.ID)
		}
		in.CreatedAt = prev.CreatedAt
		if in.Origin == "" {
			in.Origin = prev.Origin
		}
		if in.CodeshareOwner == "" {
			in.CodeshareOwner = prev.CodeshareOwner
		}
		if in.CodeshareSlug == "" {
			in.CodeshareSlug = prev.CodeshareSlug
		}
	}
	in.UpdatedAt = now
	if in.Name == "" {
		in.Name = "Untitled script"
	}
	if s.scriptsDir != "" {
		if err := os.WriteFile(scriptSidecar(s.scriptsDir, in.ID), []byte(in.Source), 0o644); err != nil {
			return FridaScript{}, fmt.Errorf("write script body: %w", err)
		}
	}
	meta := in
	meta.Source = ""
	s.scripts[in.ID] = meta
	if err := s.saveScriptsIndex(); err != nil {
		return FridaScript{}, err
	}
	return in, nil
}

// DeleteScript removes a script and detaches it from every app binding.
func (s *FridaStore) DeleteScript(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.scripts[id]; !ok {
		return fmt.Errorf("script %s not found", id)
	}
	delete(s.scripts, id)
	if s.scriptsDir != "" {
		_ = os.Remove(scriptSidecar(s.scriptsDir, id))
	}
	changed := false
	for pkg, b := range s.appScripts {
		kept := b.ScriptIDs[:0]
		for _, sid := range b.ScriptIDs {
			if sid != id {
				kept = append(kept, sid)
			}
		}
		if len(kept) != len(b.ScriptIDs) {
			b.ScriptIDs = kept
			s.appScripts[pkg] = b
			changed = true
		}
	}
	if changed {
		_ = s.saveAppScripts()
	}
	return s.saveScriptsIndex()
}

// ImportCodeshare saves a fetched CodeShare project into the library as an
// untrusted script (the user reviews and trusts it explicitly afterwards). If a
// script from the same owner/slug already exists, it is updated in place so
// "check for update" re-imports rather than duplicating.
func (s *FridaStore) ImportCodeshare(cs *CodeshareScript) (FridaScript, error) {
	if cs == nil {
		return FridaScript{}, fmt.Errorf("nil codeshare script")
	}
	name := cs.ProjectName
	if name == "" {
		name = cs.Slug
	}
	desc := cs.Description
	if cs.FridaVersion != "" {
		desc = strings.TrimSpace(desc + " (author's frida " + cs.FridaVersion + ")")
	}
	s.mu.Lock()
	var existingID string
	for id, sc := range s.scripts {
		if sc.Origin == "codeshare" && sc.CodeshareOwner == cs.Owner && sc.CodeshareSlug == cs.Slug {
			existingID = id
			break
		}
	}
	s.mu.Unlock()
	return s.SaveScript(FridaScript{
		ID:             existingID,
		Name:           name,
		Description:    desc,
		Origin:         "codeshare",
		CodeshareOwner: cs.Owner,
		CodeshareSlug:  cs.Slug,
		SourceSha:      cs.SourceSha,
		Trusted:        false, // imported source is untrusted until the user confirms
		Source:         cs.Source,
	})
}

// ─── per-app bindings (package-keyed, device-independent) ───────────────────

// GetAppScripts returns the binding for a package (a spawn-mode default when none).
func (s *FridaStore) GetAppScripts(pkg string) AppScripts {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.appScripts[pkg]; ok {
		if b.ScriptIDs == nil {
			b.ScriptIDs = []string{}
		}
		return b
	}
	return AppScripts{Package: pkg, Mode: "spawn", ScriptIDs: []string{}}
}

// SetAppScripts replaces a package's binding (empty + no venv pin clears it).
func (s *FridaStore) SetAppScripts(pkg string, scriptIDs []string, mode, venvVer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode != "attach" {
		mode = "spawn"
	}
	if scriptIDs == nil {
		scriptIDs = []string{}
	}
	if len(scriptIDs) == 0 && venvVer == "" {
		delete(s.appScripts, pkg)
	} else {
		s.appScripts[pkg] = AppScripts{Package: pkg, ScriptIDs: scriptIDs, Mode: mode, VenvVer: venvVer}
	}
	return s.saveAppScripts()
}

// ListAppScripts returns every package binding (for the device-independent view).
func (s *FridaStore) ListAppScripts() []AppScripts {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AppScripts, 0, len(s.appScripts))
	for _, b := range s.appScripts {
		if b.ScriptIDs == nil {
			b.ScriptIDs = []string{}
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out
}
