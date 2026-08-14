package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DeviceProfile is one hardware definition `avdmanager create avd -d` accepts.
type DeviceProfile struct {
	ID   string `json:"id"`   // pixel_8
	Name string `json:"name"` // Pixel 8
	OEM  string `json:"oem"`
	Tag  string `json:"tag"`
}

// AVDSpec describes an AVD to create.
type AVDSpec struct {
	Name   string `json:"name"`
	Pkg    string `json:"pkg"`    // system-images;android-34;google_apis;arm64-v8a
	Device string `json:"device"` // device profile id; empty = avdmanager default
	SDCard string `json:"sdCard"` // "512M" or a path to an existing image
	Force  bool   `json:"force"`  // overwrite an existing AVD of the same name

	// Post-creation config.ini tweaks. avdmanager has no flags for these, but
	// they are the settings users change first, and editing the ini afterwards
	// is exactly what Android Studio does.
	RAMMB    int    `json:"ramMB"`
	Cores    int    `json:"cores"`
	DataSize string `json:"dataSize"` // disk.dataPartition.size, e.g. "8G"
	Keyboard bool   `json:"keyboard"` // hw.keyboard — hardware keyboard input
	GPUMode  string `json:"gpuMode"`
}

// avdNameOK mirrors avdmanager's own restriction. Enforcing it here means a bad
// name is rejected before it becomes a confusing tool error, and a name can
// never smuggle extra arguments into the command line.
func avdNameOK(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// CreateAVDArgs builds the avdmanager argument list. Pure, so the command the
// confirm dialog shows is the command that runs (CLAUDE.md §4.1).
func CreateAVDArgs(s AVDSpec) []string {
	args := []string{"--silent", "create", "avd", "-n", s.Name, "-k", s.Pkg}
	if d := strings.TrimSpace(s.Device); d != "" {
		args = append(args, "-d", d)
	}
	if c := strings.TrimSpace(s.SDCard); c != "" {
		args = append(args, "-c", c)
	}
	if s.Force {
		args = append(args, "--force")
	}
	return args
}

// CreateAVDCommand renders the creation command for display.
func CreateAVDCommand(bin string, s AVDSpec) string {
	if strings.TrimSpace(bin) == "" {
		bin = "avdmanager"
	}
	parts := []string{shellQuoteLocal(bin)}
	for _, a := range CreateAVDArgs(s) {
		parts = append(parts, shellQuoteLocal(a))
	}
	return strings.Join(parts, " ")
}

// DeleteAVDCommand renders the deletion command for the confirm dialog.
func DeleteAVDCommand(bin, name string) string {
	if strings.TrimSpace(bin) == "" {
		bin = "avdmanager"
	}
	return shellQuoteLocal(bin) + " delete avd -n " + shellQuoteLocal(name)
}

// ListDeviceProfiles returns the hardware definitions available for creation.
func (m *EmulatorManager) ListDeviceProfiles(ctx context.Context) ([]DeviceProfile, error) {
	bin, err := m.sdk.AVDManagerBin()
	if err != nil {
		return nil, err
	}
	lctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(lctx, bin, "list", "device").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("avdmanager list device failed: %w", err)
	}
	return parseDeviceProfiles(string(out)), nil
}

// parseDeviceProfiles reads avdmanager's indented block format:
//
//	id: 0 or "pixel_8"
//	    Name: Pixel 8
//	    OEM : Google
//	    Tag : google_apis
func parseDeviceProfiles(out string) []DeviceProfile {
	profiles := []DeviceProfile{}
	var cur *DeviceProfile
	flush := func() {
		if cur != nil && cur.ID != "" {
			profiles = append(profiles, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "id:"):
			flush()
			cur = &DeviceProfile{ID: deviceIDFromLine(line)}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "Name:"):
			cur.Name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "OEM"):
			// avdmanager pads the label ("OEM : Google"), so the colon is not
			// flush against the key the way it is for Name.
			cur.OEM = trimLabelled(line, "OEM")
		case strings.HasPrefix(line, "Tag"):
			cur.Tag = trimLabelled(line, "Tag")
		case strings.HasPrefix(line, "---"):
			flush()
		}
	}
	flush()
	return profiles
}

// trimLabelled strips a `<key> : value` prefix, tolerating the alignment
// padding avdmanager inserts between the key and the colon.
func trimLabelled(line, key string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, key))
	return strings.TrimSpace(strings.TrimPrefix(rest, ":"))
}

// deviceIDFromLine pulls the quoted id out of `id: 12 or "pixel_8"`. The
// numeric index is deliberately ignored: it shifts whenever Google adds a
// device, so only the string id is a stable reference.
func deviceIDFromLine(line string) string {
	if i := strings.Index(line, `"`); i >= 0 {
		rest := line[i+1:]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j]
		}
	}
	return ""
}

// CreateAVD creates a new AVD and applies the config.ini tweaks avdmanager has
// no flags for.
func (m *EmulatorManager) CreateAVD(ctx context.Context, s AVDSpec) (*AVD, error) {
	s.Name = strings.TrimSpace(s.Name)
	if !avdNameOK(s.Name) {
		return nil, fmt.Errorf("%q is not a valid AVD name — use letters, digits, dot, dash and underscore only", s.Name)
	}
	if err := validateSDKPackage(s.Pkg); err != nil {
		return nil, err
	}
	bin, err := m.sdk.AVDManagerBin()
	if err != nil {
		return nil, err
	}
	info := m.sdk.Info()
	if !s.Force {
		if _, err := os.Stat(avdIniPath(info.AVDHome, s.Name)); err == nil {
			return nil, fmt.Errorf("an AVD named %q already exists", s.Name)
		}
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, CreateAVDArgs(s)...)
	cmd.Env = emulatorEnv(info)
	// avdmanager prompts "Do you wish to create a custom hardware profile?"
	// when -d is omitted. "no" keeps the device's own defaults, which is what
	// the UI offers; without this the command blocks forever.
	cmd.Stdin = strings.NewReader("no\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s", avdManagerError(string(out), err))
	}

	if err := applyAVDConfigTweaks(info.AVDHome, s); err != nil {
		// The AVD exists and is usable; a failed tweak is worth reporting but
		// must not read as "creation failed".
		return m.AVDByName(ctx, s.Name)
	}
	return m.AVDByName(ctx, s.Name)
}

// applyAVDConfigTweaks rewrites config.ini keys avdmanager cannot set. The file
// is read, merged and written back whole so unrelated keys survive.
func applyAVDConfigTweaks(avdHome string, s AVDSpec) error {
	changes := map[string]string{}
	if s.RAMMB > 0 {
		changes["hw.ramSize"] = strconv.Itoa(s.RAMMB)
	}
	if s.Cores > 0 {
		changes["hw.cpu.ncore"] = strconv.Itoa(s.Cores)
	}
	if v := strings.TrimSpace(s.DataSize); v != "" {
		changes["disk.dataPartition.size"] = v
	}
	if s.Keyboard {
		changes["hw.keyboard"] = "yes"
	}
	if v := strings.TrimSpace(s.GPUMode); v != "" {
		changes["hw.gpu.enabled"] = "yes"
		changes["hw.gpu.mode"] = v
	}
	if len(changes) == 0 {
		return nil
	}

	top, err := readIni(avdIniPath(avdHome, s.Name))
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(top["path"], "config.ini")
	cfg, err := readIni(cfgPath)
	if err != nil {
		return err
	}
	for k, v := range changes {
		cfg[k] = v
	}
	return writeIni(cfgPath, cfg)
}

// writeIni rewrites a config.ini with keys sorted, matching how the Android
// tools themselves write the file so a diff stays readable.
func writeIni(path string, kv map[string]string) error {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k + "=" + kv[k] + "\n")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeleteAVD removes an AVD definition and its data. Irreversible: callers must
// confirm with the user first (CLAUDE.md §5).
func (m *EmulatorManager) DeleteAVD(ctx context.Context, name string) error {
	if !avdNameOK(name) {
		return fmt.Errorf("%q is not a valid AVD name", name)
	}
	if m.IsManaged(name) {
		return fmt.Errorf("%s is running — stop it before deleting", name)
	}
	bin, err := m.sdk.AVDManagerBin()
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "delete", "avd", "-n", name)
	cmd.Env = emulatorEnv(m.sdk.Info())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", avdManagerError(string(out), err))
	}
	return nil
}

// DeleteSnapshot removes one saved snapshot directory. Snapshots are plain
// directories, and avdmanager has no command for this.
func (m *EmulatorManager) DeleteSnapshot(name, snapshot string) error {
	if !avdNameOK(name) {
		return fmt.Errorf("%q is not a valid AVD name", name)
	}
	// A snapshot name reaches us from the UI and becomes a path; anything
	// containing a separator could escape the snapshots directory.
	if snapshot == "" || strings.ContainsAny(snapshot, `/\`) || snapshot == "." || snapshot == ".." {
		return fmt.Errorf("%q is not a valid snapshot name", snapshot)
	}
	if m.IsManaged(name) {
		return fmt.Errorf("%s is running — stop it before deleting snapshots", name)
	}
	info := m.sdk.Info()
	a := LoadAVD(info.AVDHome, info.SDKRoot, name)
	if a.Path == "" {
		return fmt.Errorf("cannot locate %s on disk", name)
	}
	return os.RemoveAll(filepath.Join(a.Path, "snapshots", snapshot))
}

// avdManagerError condenses avdmanager's Java-flavoured output into one line.
func avdManagerError(out string, err error) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "package path is not valid"), strings.Contains(low, "not a valid package"):
		return "that system image is not installed — install it from the System images tab first"
	case strings.Contains(low, "already exists"):
		return "an AVD with that name already exists"
	case strings.Contains(low, "no device found"), strings.Contains(low, "invalid device"):
		return "unknown device profile — pick one from the list"
	case strings.Contains(low, "error: unable to locate"), strings.Contains(low, "java_home"):
		return "avdmanager could not start — it needs a Java runtime (Android Studio ships one; set JAVA_HOME if you use a standalone SDK)"
	}
	for _, ln := range strings.Split(out, "\n") {
		t := strings.TrimSpace(ln)
		if t != "" && !strings.HasPrefix(t, "at ") && !strings.HasPrefix(t, "Warning:") {
			return t
		}
	}
	return err.Error()
}
