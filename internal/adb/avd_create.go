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
	// FormFactor groups the ~90 definitions the SDK ships. Most of them are
	// wear, TV, automotive or headset profiles that are irrelevant to app
	// testing, and an unfiltered list buries the handful of phones.
	FormFactor string `json:"formFactor"` // phone|tablet|foldable|wear|tv|automotive|desktop|xr
	// Recommended marks the profile adbq preselects for a new AVD.
	Recommended bool `json:"recommended"`
}

// deviceFormFactor classifies a profile. The Tag line carries the answer for
// everything except phones and tablets, which have no tag at all.
func deviceFormFactor(p DeviceProfile) string {
	tag := strings.ToLower(p.Tag)
	switch {
	case strings.Contains(tag, "wear"):
		return "wear"
	case strings.Contains(tag, "tv"):
		return "tv"
	case strings.Contains(tag, "automotive"):
		return "automotive"
	case strings.Contains(tag, "desktop"):
		return "desktop"
	case strings.Contains(tag, "xr"), strings.Contains(tag, "glasses"):
		return "xr"
	}
	id, name := strings.ToLower(p.ID), strings.ToLower(p.Name)
	switch {
	case strings.Contains(id, "fold") || strings.Contains(name, "fold"):
		return "foldable"
	case strings.Contains(id, "tablet") || strings.Contains(name, "tablet") ||
		strings.Contains(id, "nexus_9") || strings.Contains(id, "nexus_10"):
		return "tablet"
	}
	return "phone"
}

// preferredDeviceIDs are the profiles adbq preselects, best first: a current
// Pixel if the SDK has one, otherwise the generic medium phone, which every SDK
// version ships.
var preferredDeviceIDs = []string{
	"pixel_8", "pixel_7", "pixel_6", "pixel_5", "medium_phone", "pixel_4",
}

// classifyDeviceProfiles fills in FormFactor and marks the recommended default.
func classifyDeviceProfiles(profiles []DeviceProfile) []DeviceProfile {
	byID := map[string]int{}
	for i := range profiles {
		profiles[i].FormFactor = deviceFormFactor(profiles[i])
		byID[profiles[i].ID] = i
	}
	for _, want := range preferredDeviceIDs {
		if i, ok := byID[want]; ok {
			profiles[i].Recommended = true
			break
		}
	}
	return profiles
}

// DefaultAVDSpec proposes sensible settings for a new AVD, so the create form
// opens filled in rather than blank. The emulator's own defaults are frugal
// enough (1.5 GB RAM, 2 cores, 800 MB of data) to make a modern Android image
// feel broken, and a user who does not know that has no way to guess.
func DefaultAVDSpec(img SystemImage, profiles []DeviceProfile) AVDSpec {
	spec := AVDSpec{
		Pkg:      img.Pkg,
		SDCard:   "512M",
		RAMMB:    4096,
		Cores:    4,
		DataSize: "8G",
		Keyboard: true,
		GPUMode:  "auto",
	}
	for _, p := range profiles {
		if p.Recommended {
			spec.Device = p.ID
			break
		}
	}
	if img.Pkg != "" {
		spec.Name = suggestAVDName(img)
	}
	return spec
}

// suggestAVDName proposes a name that says what the AVD is, and is already
// valid: "Android_14_Play" rather than "Pixel 8 API 34 (copy)".
func suggestAVDName(img SystemImage) string {
	base := "Android"
	if img.API > 0 {
		base += "_" + strconv.Itoa(img.API)
	} else if img.Level != "" {
		base += "_" + strings.TrimPrefix(img.Level, "android-")
	}
	switch {
	case img.PlayStore:
		base += "_Play"
	case strings.Contains(img.Tag, "google_apis"):
		base += "_GApis"
	}
	// The suggestion feeds a field validated by avdNameOK, so sanitise here
	// rather than handing the user a name that will be rejected.
	var sb strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	return sb.String()
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
	parts := []string{quoteArg(bin)}
	for _, a := range CreateAVDArgs(s) {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// DeleteAVDCommand renders the deletion command for the confirm dialog.
func DeleteAVDCommand(bin, name string) string {
	if strings.TrimSpace(bin) == "" {
		bin = "avdmanager"
	}
	return quoteArg(bin) + " delete avd -n " + quoteArg(name)
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
	return classifyDeviceProfiles(parseDeviceProfiles(string(out))), nil
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

	tweakErr := applyAVDConfigTweaks(info.AVDHome, s)
	avd, err := m.AVDByName(ctx, s.Name)
	if err != nil {
		return nil, err
	}
	// The AVD exists and boots; only the extra settings were lost. That must not
	// read as "creation failed", but it must not vanish either — otherwise the
	// AVD comes up with the emulator's frugal defaults and nothing says why.
	if tweakErr != nil {
		avd.Warning = "created, but the RAM/cores/disk/GPU settings could not be written to config.ini: " +
			tweakErr.Error() + " — adjust them from the AVD's hardware settings"
	}
	return avd, nil
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
	// Written either way. "Off" is a choice the create form offers, and omitting
	// the key would silently hand the decision back to the device profile — the
	// switch would look like it did nothing.
	changes["hw.keyboard"] = "no"
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

// AVDHardware is the editable half of an AVD's config.ini — the settings a user
// actually changes after creating one. Every field is optional: a zero value
// means "leave whatever is there", so the UI can send a partial edit without
// having to round-trip settings it doesn't show.
type AVDHardware struct {
	RAMMB    int    `json:"ramMB"`
	Cores    int    `json:"cores"`
	DataSize string `json:"dataSize"` // disk.dataPartition.size, e.g. "8G"
	SDCard   string `json:"sdCard"`   // sdcard.size
	GPUMode  string `json:"gpuMode"`  // auto|host|swiftshader_indirect|angle_indirect|off
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Density  int    `json:"density"`
	Keyboard *bool  `json:"keyboard"` // pointer: false is a real choice, not "unset"
}

// gpuModes are the values `hw.gpu.mode` accepts. A typo here produces an
// emulator that refuses to start, so the value is checked before it is written.
var gpuModes = map[string]bool{
	"auto": true, "host": true, "swiftshader_indirect": true,
	"angle_indirect": true, "guest": true, "off": true,
}

// avdSizeOK validates a partition size the way the emulator expects it: a
// number with an optional K/M/G suffix.
func avdSizeOK(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true // "leave alone"
	}
	last := s[len(s)-1]
	if last == 'K' || last == 'k' || last == 'M' || last == 'm' || last == 'G' || last == 'g' {
		s = s[:len(s)-1]
	}
	if s == "" {
		return false
	}
	n, err := strconv.Atoi(s)
	return err == nil && n > 0
}

// AVDHardwareChanges turns an edit into the exact config.ini keys it writes.
// Pure and tested, so the UI can show the user precisely what will change
// before anything is written (CLAUDE.md §4.1).
func AVDHardwareChanges(h AVDHardware) (map[string]string, error) {
	out := map[string]string{}
	if h.RAMMB != 0 {
		// Below ~512 MB Android will not boot at all; above 64 GB is a typo.
		if h.RAMMB < 512 || h.RAMMB > 65536 {
			return nil, fmt.Errorf("RAM must be between 512 and 65536 MB, got %d", h.RAMMB)
		}
		out["hw.ramSize"] = strconv.Itoa(h.RAMMB)
	}
	if h.Cores != 0 {
		if h.Cores < 1 || h.Cores > 16 {
			return nil, fmt.Errorf("CPU cores must be between 1 and 16, got %d", h.Cores)
		}
		out["hw.cpu.ncore"] = strconv.Itoa(h.Cores)
	}
	if v := strings.TrimSpace(h.DataSize); v != "" {
		if !avdSizeOK(v) {
			return nil, fmt.Errorf("%q is not a valid data partition size — use a number with an optional K, M or G suffix", v)
		}
		out["disk.dataPartition.size"] = v
	}
	if v := strings.TrimSpace(h.SDCard); v != "" {
		if !avdSizeOK(v) {
			return nil, fmt.Errorf("%q is not a valid SD card size — use a number with an optional K, M or G suffix", v)
		}
		out["sdcard.size"] = v
		out["hw.sdCard"] = "yes"
	}
	if v := strings.TrimSpace(h.GPUMode); v != "" {
		if !gpuModes[v] {
			return nil, fmt.Errorf("%q is not a GPU mode the emulator accepts", v)
		}
		out["hw.gpu.enabled"] = "yes"
		out["hw.gpu.mode"] = v
		if v == "off" {
			out["hw.gpu.enabled"] = "no"
		}
	}
	// Width and height are meaningless apart, so they move together.
	if (h.Width == 0) != (h.Height == 0) {
		return nil, fmt.Errorf("set both width and height, or neither")
	}
	if h.Width != 0 {
		if h.Width < 240 || h.Width > 8192 || h.Height < 240 || h.Height > 8192 {
			return nil, fmt.Errorf("resolution %dx%d is out of range", h.Width, h.Height)
		}
		out["hw.lcd.width"] = strconv.Itoa(h.Width)
		out["hw.lcd.height"] = strconv.Itoa(h.Height)
	}
	if h.Density != 0 {
		if h.Density < 80 || h.Density > 960 {
			return nil, fmt.Errorf("screen density must be between 80 and 960 dpi, got %d", h.Density)
		}
		out["hw.lcd.density"] = strconv.Itoa(h.Density)
	}
	if h.Keyboard != nil {
		out["hw.keyboard"] = "no"
		if *h.Keyboard {
			out["hw.keyboard"] = "yes"
		}
	}
	return out, nil
}

// UpdateAVDHardware rewrites an AVD's config.ini with the requested changes and
// returns the reloaded AVD.
//
// The emulator reads config.ini once at boot, so an edit made while the AVD is
// running takes effect on its next start. That is worth saying in the UI, but
// not worth refusing: pre-configuring a running AVD is a reasonable thing to do.
func (m *EmulatorManager) UpdateAVDHardware(ctx context.Context, name string, h AVDHardware) (*AVD, error) {
	if !avdNameOK(name) {
		return nil, fmt.Errorf("%q is not a valid AVD name", name)
	}
	changes, err := AVDHardwareChanges(h)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return m.AVDByName(ctx, name)
	}
	info := m.sdk.Info()
	top, err := readIni(avdIniPath(info.AVDHome, name))
	if err != nil {
		return nil, fmt.Errorf("cannot read the definition of %s: %w", name, err)
	}
	cfgPath := filepath.Join(top["path"], "config.ini")
	cfg, err := readIni(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the config of %s: %w", name, err)
	}
	for k, v := range changes {
		cfg[k] = v
	}
	if err := writeIni(cfgPath, cfg); err != nil {
		return nil, fmt.Errorf("cannot save the config of %s: %w", name, err)
	}
	return m.AVDByName(ctx, name)
}

// DeleteAVD removes an AVD definition and its data. Irreversible: callers must
// confirm with the user first (CLAUDE.md §5).
func (m *EmulatorManager) DeleteAVD(ctx context.Context, name string) error {
	if !avdNameOK(name) {
		return fmt.Errorf("%q is not a valid AVD name", name)
	}
	if m.avdBusy(ctx, name) {
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
	// Snapshots are read and rewritten by a live emulator, so this has to catch
	// an AVD started from anywhere, not just adbq's own children.
	bctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if m.avdBusy(bctx, name) {
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
