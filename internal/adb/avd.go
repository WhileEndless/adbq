package adb

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// AVDState is the lifecycle state of an Android Virtual Device.
//
// "stopped" and "running" are the steady states; "booting" covers both halves
// of a cold boot (the emulator process is up but adb has no transport yet, and
// the transport exists but sys.boot_completed is still 0). We distinguish them
// because a UI that shows "stopped" for the 40 seconds an AVD takes to boot
// invites the user to start it twice.
type AVDState string

const (
	AVDStopped AVDState = "stopped"
	AVDBooting AVDState = "booting"
	AVDRunning AVDState = "running"
	// AVDOffline means adb sees the transport but refuses to talk to it —
	// a wedged emulator that needs killing, not waiting on.
	AVDOffline AVDState = "offline"
	AVDError   AVDState = "error"
)

// AVD is one Android Virtual Device: its on-disk definition plus, when it is
// up, its live state. Everything except the live half is readable without an
// emulator running, so the list works on a machine with nothing booted.
type AVD struct {
	// Name is the .ini filename, which is the identity `emulator -avd` expects.
	// It is NOT always the directory name — see LoadAVD.
	Name    string `json:"name"`
	Display string `json:"display"`
	Path    string `json:"path"` // the .avd directory

	// Target is the raw platform string ("android-34", "android-36.1"); API is
	// its major number, which is all any compatibility decision needs.
	Target     string `json:"target"`
	API        int    `json:"api"`
	AndroidVer string `json:"androidVer"`

	Tag        string `json:"tag"`        // google_apis | google_apis_playstore | default | aosp_atd …
	TagDisplay string `json:"tagDisplay"` // "Google APIs", "Google Play"
	PlayStore  bool   `json:"playStore"`
	ABI        string `json:"abi"`

	Device     string `json:"device"`    // hw.device.name, e.g. pixel_8
	DeviceMfr  string `json:"deviceMfr"` // hw.device.manufacturer
	Skin       string `json:"skin"`      // skin.name
	RAMMB      int    `json:"ramMB"`
	Cores      int    `json:"cores"`
	Density    int    `json:"density"`
	Resolution string `json:"resolution"` // "1080x2400"
	SDCard     string `json:"sdCard"`     // sdcard.size as written ("512M")
	DataSize   string `json:"dataSize"`   // disk.dataPartition.size
	GPUMode    string `json:"gpuMode"`    // hw.gpu.mode
	DiskBytes  int64  `json:"diskBytes"`  // size of the .avd directory on disk

	// SysImgDir is the absolute system-image directory this AVD boots from, and
	// RamdiskRel the ramdisk path relative to the SDK root. Both matter for
	// rooting: the ramdisk is shared by every AVD using the same image.
	SysImgDir  string `json:"sysImgDir"`
	RamdiskRel string `json:"ramdiskRel"`
	// Patched reports a rootAVD-style ramdisk backup sitting next to the image,
	// i.e. this system image has already been modified.
	Patched bool `json:"patched"`

	Snapshots []string `json:"snapshots"`

	// Live state — zero-valued when the AVD is not running.
	State   AVDState `json:"state"`
	Serial  string   `json:"serial"`  // emulator-<consolePort>
	Port    int      `json:"port"`    // console port
	Managed bool     `json:"managed"` // true when adbq started this instance
	Root    string   `json:"root"`    // "" | "adb-root" | "su" | "no"

	// Error explains a definition adbq could read but not trust (missing
	// config.ini, unreadable directory). Such AVDs are listed, not hidden:
	// hiding them makes a broken AVD look deleted.
	Error string `json:"error"`

	// Warning is a non-fatal problem with an AVD that otherwise works — a
	// setting that could not be written, say. Unlike Error it does not mean the
	// definition is untrustworthy, so the UI shows it once rather than marking
	// the AVD broken.
	Warning string `json:"warning"`

	// Commands is the adb/emulator command line behind this entry (CLAUDE.md §4.1).
	Commands []string `json:"commands"`
}

// ─── ini parsing ───────────────────────────────────────────────────────────

// parseIni reads the key=value format used by config.ini, <name>.ini and
// hardware-qemu.ini. It is deliberately permissive: these files are written by
// several generations of Android tooling and a single unparseable line must not
// cost the user the whole AVD.
func parseIni(b []byte) map[string]string {
	out := map[string]string{}
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, ";") {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func readIni(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseIni(b), nil
}

// iniInt reads a numeric field, tolerating the unit suffixes Android writes
// ("512M", "10G", "8192"). Returns 0 when absent or unparseable.
func iniInt(m map[string]string, key string) int {
	v := strings.TrimSpace(m[key])
	if v == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(v, "M"), strings.HasSuffix(v, "m"):
		v = v[:len(v)-1]
	case strings.HasSuffix(v, "G"), strings.HasSuffix(v, "g"):
		v, mult = v[:len(v)-1], 1024
	case strings.HasSuffix(v, "K"), strings.HasSuffix(v, "k"):
		v = v[:len(v)-1]
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return n * mult
}

func iniBool(m map[string]string, key string) bool {
	switch strings.ToLower(strings.TrimSpace(m[key])) {
	case "yes", "true", "1", "on":
		return true
	}
	return false
}

// apiFromTarget extracts the major API level from a platform string. Android
// now ships minor platforms ("android-36.1"), and every compatibility rule we
// have is expressed against the major level.
func apiFromTarget(target string) int {
	t := strings.TrimSpace(target)
	t = strings.TrimPrefix(t, "android-")
	// Preview platforms use codenames ("android-Baklava"); those have no number.
	if i := strings.IndexByte(t, '.'); i >= 0 {
		t = t[:i]
	}
	n, err := strconv.Atoi(t)
	if err != nil {
		return 0
	}
	return n
}

// apiFromSysdir recovers the platform from image.sysdir.1
// ("system-images/android-34/google_apis/arm64-v8a/") for AVDs whose .ini has
// no target= line.
func apiFromSysdir(sysdir string) string {
	for _, part := range strings.Split(filepath.ToSlash(sysdir), "/") {
		if strings.HasPrefix(part, "android-") {
			return part
		}
	}
	return ""
}

// ─── inventory ─────────────────────────────────────────────────────────────

// ListAVDNames returns the AVD names defined under avdHome, sorted.
//
// The name is the .ini filename, not the directory name: `avdmanager` lets the
// two diverge (Medium_Phone_API_36.1.ini → Medium_Phone.avd), and `emulator
// -avd` only accepts the former.
func ListAVDNames(avdHome string) ([]string, error) {
	if avdHome == "" {
		return []string{}, nil
	}
	entries, err := os.ReadDir(avdHome)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ini") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".ini"))
	}
	sort.Strings(names)
	return names, nil
}

// LoadAVD reads one AVD definition from disk. It never returns a nil AVD for a
// name that exists: a broken definition comes back with Error set so the UI can
// show it as broken rather than silently dropping it.
func LoadAVD(avdHome, sdkRoot, name string) *AVD {
	a := &AVD{
		Name:      name,
		Display:   name,
		State:     AVDStopped,
		Snapshots: []string{},
		Commands:  []string{},
	}

	top, err := readIni(filepath.Join(avdHome, name+".ini"))
	if err != nil {
		a.Error = "cannot read " + name + ".ini: " + err.Error()
		a.State = AVDError
		return a
	}
	a.Path = top["path"]
	if a.Path == "" && top["path.rel"] != "" {
		// path.rel is relative to the .android dir, i.e. avdHome's parent.
		a.Path = filepath.Join(filepath.Dir(avdHome), top["path.rel"])
	}
	a.Target = top["target"]

	cfg, err := readIni(filepath.Join(a.Path, "config.ini"))
	if err != nil {
		a.Error = "missing or unreadable config.ini in " + a.Path
		a.State = AVDError
		return a
	}
	a.applyConfig(cfg, sdkRoot)
	a.Snapshots = readSnapshots(a.Path)
	a.DiskBytes = dirSize(a.Path)
	return a
}

func (a *AVD) applyConfig(cfg map[string]string, sdkRoot string) {
	if v := cfg["avd.ini.displayname"]; v != "" {
		a.Display = v
	}
	if a.Target == "" {
		a.Target = apiFromSysdir(cfg["image.sysdir.1"])
	}
	a.API = apiFromTarget(a.Target)
	a.AndroidVer = AndroidVersionForSdk(strconv.Itoa(a.API))

	a.Tag = cfg["tag.id"]
	a.TagDisplay = cfg["tag.display"]
	a.PlayStore = iniBool(cfg, "PlayStore.enabled")
	// Older images predate PlayStore.enabled; the tag is the fallback signal.
	if !a.PlayStore && strings.Contains(a.Tag, "playstore") {
		a.PlayStore = true
	}
	a.ABI = cfg["abi.type"]
	if a.ABI == "" {
		a.ABI = cfg["hw.cpu.arch"]
	}

	a.Device = cfg["hw.device.name"]
	a.DeviceMfr = cfg["hw.device.manufacturer"]
	a.Skin = cfg["skin.name"]
	a.RAMMB = iniInt(cfg, "hw.ramSize")
	a.Cores = iniInt(cfg, "hw.cpu.ncore")
	a.Density = iniInt(cfg, "hw.lcd.density")
	if w, h := iniInt(cfg, "hw.lcd.width"), iniInt(cfg, "hw.lcd.height"); w > 0 && h > 0 {
		a.Resolution = strconv.Itoa(w) + "x" + strconv.Itoa(h)
	}
	a.SDCard = cfg["sdcard.size"]
	a.DataSize = cfg["disk.dataPartition.size"]
	a.GPUMode = cfg["hw.gpu.mode"]

	if sysdir := cfg["image.sysdir.1"]; sysdir != "" && sdkRoot != "" {
		a.SysImgDir = filepath.Join(sdkRoot, filepath.FromSlash(sysdir))
		a.RamdiskRel = filepath.ToSlash(filepath.Join(filepath.Clean(filepath.FromSlash(sysdir)), "ramdisk.img"))
		a.Patched = ramdiskPatched(a.SysImgDir)
	}
}

// ramdiskPatched reports a .backup file next to the ramdisk — the marker
// rootAVD leaves behind, and the only offline evidence that a system image has
// been modified.
func ramdiskPatched(sysImgDir string) bool {
	if sysImgDir == "" {
		return false
	}
	for _, n := range []string{"ramdisk.img.backup", "ramdisk.img.gz.backup"} {
		if _, err := os.Stat(filepath.Join(sysImgDir, n)); err == nil {
			return true
		}
	}
	return false
}

// readSnapshots lists snapshot names. Each snapshot is a subdirectory of
// <avd>/snapshots; a missing directory just means none have been taken.
func readSnapshots(avdPath string) []string {
	out := []string{}
	if avdPath == "" {
		return out
	}
	entries, err := os.ReadDir(filepath.Join(avdPath, "snapshots"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// dirSize sums the AVD directory, so the UI can show what an AVD costs before
// the user decides to delete it. Best-effort: unreadable entries are skipped.
func dirSize(root string) int64 {
	if root == "" {
		return 0
	}
	var total int64
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable subtree just doesn't count
		}
		if !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total
}
