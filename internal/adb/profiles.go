package adb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── Profile step sub-objects ────────────────────────────────────────────────
// Each step carries an Enabled flag so a profile can mix on/off features.

// ProfileStep is embedded by every step to carry the enable toggle.
type ProfileStep struct {
	Enabled bool `json:"enabled"`
}

// FridaStep installs (if missing) and optionally starts a frida-server. Only the
// version is stored; the arch is resolved from the connected device at apply
// time when AutoArch is set (so one profile works across devices).
type FridaStep struct {
	ProfileStep
	Version  string `json:"version"`         // e.g. "16.4.8"; "" = latest at apply
	AutoArch bool   `json:"autoArch"`        // resolve arch from device ABI
	Arch     string `json:"arch,omitempty"`  // honored only when AutoArch=false
	Start    bool   `json:"start"`           // run StartFrida after install (root)
	Iface    string `json:"iface,omitempty"` // default 0.0.0.0
	Port     int    `json:"port,omitempty"`  // default 27042
}

// ForwardSpec / ReverseSpec mirror `adb forward`/`adb reverse` arguments.
type ForwardSpec struct {
	Local  string `json:"local"`  // "tcp:8080"
	Remote string `json:"remote"` // "tcp:8080"
}
type ReverseSpec struct {
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

type ForwardsStep struct {
	ProfileStep
	Forwards []ForwardSpec `json:"forwards"`
	Reverses []ReverseSpec `json:"reverses"`
}

// ProxyStep sets the global http proxy. HostPort "" clears it; "auto" resolves a
// host:port from the device's network at apply time.
type ProxyStep struct {
	ProfileStep
	HostPort string `json:"hostPort"`
	Port     int    `json:"port,omitempty"` // used when HostPort=="auto"
}

type HostsStep struct {
	ProfileStep
	Content  string `json:"content"`
	FlushDNS bool   `json:"flushDns"`
}

// CertStep stores the CA certificate PEM text (public material). At apply time
// the PEM is written to a temp file and installed via InstallSystemCert.
type CertStep struct {
	ProfileStep
	PEM     string `json:"pem"`
	Subject string `json:"subject"` // cached for display only
}

type IptablesStep struct {
	ProfileStep
	V4Blob string `json:"v4Blob,omitempty"` // iptables-save text (family v4)
	V6Blob string `json:"v6Blob,omitempty"` // ip6tables-save text
}

// Profile is a reusable named bundle of per-device settings. Every step is an
// action applied on connect (after confirmation); there are no stored-but-unused
// "preset" steps.
type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`

	Frida    FridaStep    `json:"frida"`
	Forwards ForwardsStep `json:"forwards"`
	Proxy    ProxyStep    `json:"proxy"`
	Hosts    HostsStep    `json:"hosts"`
	Cert     CertStep     `json:"cert"`
	Iptables IptablesStep `json:"iptables"`
}

// Domains reports the cache domains applying this profile would dirty, derived
// from which steps are actually enabled rather than hand-listed at the call
// site. It lives here, beside the step definitions, so adding a step forces the
// question "what does this invalidate?" in the same edit — a list kept anywhere
// else would go stale the first time someone added a step and forgot.
//
// Enabled-only: applying a profile with the cert step off must not drop the
// cert cache, or a profile apply would cost a re-read of everything a profile
// could conceivably touch.
func (p *Profile) Domains() []Domain {
	if p == nil {
		return nil
	}
	var out []Domain
	if p.Iptables.Enabled {
		out = append(out, DomIptables)
	}
	if p.Hosts.Enabled {
		// Rewriting hosts changes name resolution, so network reads go too.
		out = append(out, DomHosts, DomNet)
	}
	if p.Cert.Enabled {
		out = append(out, DomCerts)
	}
	if p.Frida.Enabled {
		// The step can push a server binary as well as start it.
		out = append(out, DomFrida, DomFiles, DomStorage)
	}
	if p.Forwards.Enabled {
		out = append(out, DomForwards)
	}
	if p.Proxy.Enabled {
		out = append(out, DomProxy, DomNet)
	}
	return out
}

// DeviceRecord remembers a device adbq has seen and which profile it last used
// (its default on reconnect). Keyed by the stable DeviceKey.
type DeviceRecord struct {
	Key            string `json:"key"`
	AdbSerial      string `json:"adbSerial"`
	HardwareSerial string `json:"hardwareSerial"`
	Label          string `json:"label"`
	Model          string `json:"model"`
	Manufacturer   string `json:"manufacturer"`
	FirstSeen      int64  `json:"firstSeen"`
	LastSeen       int64  `json:"lastSeen"`
	BoundProfileID string `json:"boundProfileId"` // "" = Base / no profile
}

// ── Apply result / preview types (surfaced to the UI) ───────────────────────

// StepResult is the outcome of applying one profile step.
type StepResult struct {
	Name        string `json:"name"`        // "frida", "forwards", …
	Status      string `json:"status"`      // "ok" | "skip" | "err"
	Message     string `json:"message"`     // detail / error / skip reason
	NeedsReboot bool   `json:"needsReboot"` // hosts magisk-module / non-persistent cert
}

// ApplyReport aggregates the per-step results of applying a profile.
type ApplyReport struct {
	ProfileID   string       `json:"profileId"`
	ProfileName string       `json:"profileName"`
	Serial      string       `json:"serial"`
	Rooted      bool         `json:"rooted"`
	Steps       []StepResult `json:"steps"`
	NeedsReboot bool         `json:"needsReboot"`
}

// StepPreview describes what a step will do, for the pre-apply confirm dialog.
type StepPreview struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	NeedsRoot bool   `json:"needsRoot"`
	// Commands is what this step will run. A profile applies several unrelated
	// changes in one click, which is exactly when a user most needs to see them
	// listed before agreeing (CLAUDE.md §4.1 K2/K4).
	Commands   []string `json:"commands"`
	WillSkip   bool     `json:"willSkip"`
	SkipReason string   `json:"skipReason,omitempty"`
}

// ── ProfileStore: JSON-on-disk persistence under ~/.adbq ─────────────────────

// ProfileStore owns profiles.json (id → Profile) and devices.json
// (key → DeviceRecord). Thread-safe; writes are atomic (tmp + rename) since this
// is user-authored data where a torn write is worse than for sessions.
type ProfileStore struct {
	mu           sync.Mutex
	profilesPath string
	devicesPath  string
	seq          int
	profiles     map[string]Profile
	devices      map[string]DeviceRecord
}

func NewProfileStore() (*ProfileStore, error) {
	// Always return a usable (non-nil) store so callers never have to nil-check:
	// if the config dir can't be resolved, the store works in-memory and its
	// saves become no-ops (empty paths).
	s := &ProfileStore{profiles: map[string]Profile{}, devices: map[string]DeviceRecord{}}
	d, err := configDir()
	if err != nil {
		return s, err
	}
	s.profilesPath = filepath.Join(d, "profiles.json")
	s.devicesPath = filepath.Join(d, "devices.json")
	if b, err := os.ReadFile(s.profilesPath); err == nil {
		_ = json.Unmarshal(b, &s.profiles)
	}
	if b, err := os.ReadFile(s.devicesPath); err == nil {
		_ = json.Unmarshal(b, &s.devices)
	}
	// Seed the id sequence above any existing "p-<n>" ids so we never collide.
	for id := range s.profiles {
		var n int
		if _, err := fmt.Sscanf(id, "p-%d", &n); err == nil && n > s.seq {
			s.seq = n
		}
	}
	return s, nil
}

func atomicWriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *ProfileStore) saveProfiles() error {
	if s.profilesPath == "" {
		return nil // in-memory store (config dir unavailable)
	}
	return atomicWriteJSON(s.profilesPath, s.profiles)
}
func (s *ProfileStore) saveDevices() error {
	if s.devicesPath == "" {
		return nil
	}
	return atomicWriteJSON(s.devicesPath, s.devices)
}

// ListProfiles returns all profiles.
func (s *ProfileStore) ListProfiles() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p)
	}
	return out
}

// GetProfile returns the profile by id.
func (s *ProfileStore) GetProfile(id string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[id]
	return p, ok
}

// SaveProfile creates (assigning an id) or updates a profile, stamping times.
func (s *ProfileStore) SaveProfile(p Profile) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if p.ID == "" {
		s.seq++
		p.ID = fmt.Sprintf("p-%d", s.seq)
		p.CreatedAt = now
	} else if existing, ok := s.profiles[p.ID]; ok {
		p.CreatedAt = existing.CreatedAt
	} else {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	s.profiles[p.ID] = p
	return p, s.saveProfiles()
}

// DeleteProfile removes a profile and unbinds any device pointing at it.
func (s *ProfileStore) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.profiles, id)
	for k, rec := range s.devices {
		if rec.BoundProfileID == id {
			rec.BoundProfileID = ""
			s.devices[k] = rec
		}
	}
	if err := s.saveProfiles(); err != nil {
		return err
	}
	return s.saveDevices()
}

// ListDeviceRecords returns all known device records.
func (s *ProfileStore) ListDeviceRecords() []DeviceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DeviceRecord, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, d)
	}
	return out
}

// GetDevice returns the record for a device key.
func (s *ProfileStore) GetDevice(key string) (DeviceRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[key]
	return d, ok
}

// UpsertDeviceSeen records that a device was seen, refreshing its metadata and
// LastSeen while preserving its bound profile and FirstSeen.
func (s *ProfileStore) UpsertDeviceSeen(rec DeviceRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if existing, ok := s.devices[rec.Key]; ok {
		existing.AdbSerial = rec.AdbSerial
		existing.HardwareSerial = rec.HardwareSerial
		if rec.Label != "" {
			existing.Label = rec.Label
		}
		if rec.Model != "" {
			existing.Model = rec.Model
		}
		if rec.Manufacturer != "" {
			existing.Manufacturer = rec.Manufacturer
		}
		existing.LastSeen = now
		s.devices[rec.Key] = existing
	} else {
		rec.FirstSeen = now
		rec.LastSeen = now
		s.devices[rec.Key] = rec
	}
	_ = s.saveDevices()
}

// BindProfile sets (or clears, with "") a device's default profile.
func (s *ProfileStore) BindProfile(key, profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.devices[key]
	if !ok {
		now := time.Now().Unix()
		rec = DeviceRecord{Key: key, FirstSeen: now, LastSeen: now}
	}
	rec.BoundProfileID = profileID
	s.devices[key] = rec
	return s.saveDevices()
}
