package adb

import (
	"strconv"
	"strings"
)

// EmulatorOpts are the boot-time choices adbq exposes for `emulator -avd`.
//
// Only fields the user actually set are turned into flags: the emulator's own
// defaults come from the AVD's config.ini, and passing a flag with a "default"
// value would silently override a per-AVD setting the user configured in
// Android Studio.
type EmulatorOpts struct {
	// ColdBoot skips the saved snapshot for this boot only (-no-snapshot-load).
	ColdBoot bool `json:"coldBoot"`
	// NoSnapshotSave discards state on exit instead of writing it back.
	NoSnapshotSave bool `json:"noSnapshotSave"`
	// NoSnapshot disables snapshots entirely for this run.
	NoSnapshot bool `json:"noSnapshot"`
	// Snapshot boots from a named snapshot instead of default-boot.
	Snapshot string `json:"snapshot"`
	// WipeData factory-resets the userdata partition. Destructive.
	WipeData bool `json:"wipeData"`

	NoWindow   bool `json:"noWindow"`   // headless
	NoBootAnim bool `json:"noBootAnim"` // faster boot
	// WritableSystem allows `adb remount` to modify /system — the prerequisite
	// for anything that edits the system partition at runtime.
	WritableSystem bool `json:"writableSystem"`
	// ReadOnly lets several instances share one AVD; snapshots are unavailable.
	ReadOnly bool `json:"readOnly"`

	GPU      string `json:"gpu"`      // auto|host|swiftshader_indirect|angle_indirect|off
	MemoryMB int    `json:"memoryMB"` // -memory
	Cores    int    `json:"cores"`    // -cores

	NetSpeed  string `json:"netSpeed"`  // gsm|edge|lte|full|<up>:<down>
	NetDelay  string `json:"netDelay"`  // none|gsm|edge|umts|<ms>
	DNS       string `json:"dns"`       // comma-separated servers
	HTTPProxy string `json:"httpProxy"` // host:port

	// SELinux is "disabled" or "permissive"; empty leaves the image default.
	SELinux string `json:"selinux"`

	// Extra is passed through verbatim, last, for flags adbq doesn't model.
	Extra []string `json:"extra"`
}

// EmulatorArgs builds the argument list for `emulator`. It is pure so the exact
// command the UI shows is the command adbq runs (CLAUDE.md §4.1).
//
// port must be an even console port in [5554, 5584]; pass 0 to let the emulator
// pick one, at the cost of not knowing the resulting serial up front.
func EmulatorArgs(name string, port int, o EmulatorOpts) []string {
	args := []string{"-avd", name}
	if port > 0 {
		args = append(args, "-port", strconv.Itoa(port))
	}

	// Snapshot behaviour first: these interact, and a stable order keeps the
	// displayed command diffable as the user toggles checkboxes.
	if o.WipeData {
		args = append(args, "-wipe-data")
	}
	if o.NoSnapshot {
		args = append(args, "-no-snapshot")
	} else {
		if o.ColdBoot {
			args = append(args, "-no-snapshot-load")
		}
		if o.NoSnapshotSave {
			args = append(args, "-no-snapshot-save")
		}
		if s := strings.TrimSpace(o.Snapshot); s != "" {
			args = append(args, "-snapshot", s)
		}
	}

	if o.NoWindow {
		args = append(args, "-no-window")
	}
	if o.NoBootAnim {
		args = append(args, "-no-boot-anim")
	}
	if o.WritableSystem {
		args = append(args, "-writable-system")
	}
	if o.ReadOnly {
		args = append(args, "-read-only")
	}

	if g := strings.TrimSpace(o.GPU); g != "" {
		args = append(args, "-gpu", g)
	}
	if o.MemoryMB > 0 {
		args = append(args, "-memory", strconv.Itoa(o.MemoryMB))
	}
	if o.Cores > 0 {
		args = append(args, "-cores", strconv.Itoa(o.Cores))
	}

	if v := strings.TrimSpace(o.NetSpeed); v != "" {
		args = append(args, "-netspeed", v)
	}
	if v := strings.TrimSpace(o.NetDelay); v != "" {
		args = append(args, "-netdelay", v)
	}
	if v := strings.TrimSpace(o.DNS); v != "" {
		args = append(args, "-dns-server", v)
	}
	if v := strings.TrimSpace(o.HTTPProxy); v != "" {
		args = append(args, "-http-proxy", v)
	}
	if v := strings.TrimSpace(o.SELinux); v != "" {
		args = append(args, "-selinux", v)
	}

	for _, e := range o.Extra {
		if e = strings.TrimSpace(e); e != "" {
			args = append(args, e)
		}
	}
	return args
}

// EmulatorCommand renders the launch command as a single copy-pasteable line.
// bin may be empty, in which case the bare `emulator` name is shown — a user
// with the SDK on PATH can still paste it.
func EmulatorCommand(bin, name string, port int, o EmulatorOpts) string {
	if strings.TrimSpace(bin) == "" {
		bin = "emulator"
	}
	parts := make([]string, 0, 24)
	parts = append(parts, quoteArg(bin))
	for _, a := range EmulatorArgs(name, port, o) {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// emulatorConsolePorts is the range the emulator binds its console to: even
// ports from 5554 to 5584, with port+1 reserved for adb. The serial adb reports
// is always "emulator-<consolePort>".
const (
	emulatorPortMin = 5554
	emulatorPortMax = 5584
)

// SerialForPort maps a console port to the adb serial it will appear as.
func SerialForPort(port int) string { return "emulator-" + strconv.Itoa(port) }

// PortForSerial is the inverse of SerialForPort; 0 when serial isn't an emulator.
func PortForSerial(serial string) int {
	s, ok := strings.CutPrefix(serial, "emulator-")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
