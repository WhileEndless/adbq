package adb

import (
	"strings"
	"testing"
)

// The command shown in the UI must be the command adbq runs, so every option is
// pinned to its exact flag here.
func TestEmulatorArgsFlagMapping(t *testing.T) {
	cases := []struct {
		name string
		opts EmulatorOpts
		want []string
	}{
		{
			name: "defaults add nothing beyond the AVD",
			opts: EmulatorOpts{},
			want: []string{"-avd", "Pixel_8", "-port", "5554"},
		},
		{
			name: "cold boot",
			opts: EmulatorOpts{ColdBoot: true},
			want: []string{"-avd", "Pixel_8", "-port", "5554", "-no-snapshot-load"},
		},
		{
			name: "wipe data is ordered before snapshot flags",
			opts: EmulatorOpts{WipeData: true, ColdBoot: true},
			want: []string{"-avd", "Pixel_8", "-port", "5554", "-wipe-data", "-no-snapshot-load"},
		},
		{
			name: "named snapshot",
			opts: EmulatorOpts{Snapshot: "clean"},
			want: []string{"-avd", "Pixel_8", "-port", "5554", "-snapshot", "clean"},
		},
		{
			name: "headless pentest boot",
			opts: EmulatorOpts{NoWindow: true, NoBootAnim: true, WritableSystem: true},
			want: []string{"-avd", "Pixel_8", "-port", "5554", "-no-window", "-no-boot-anim", "-writable-system"},
		},
		{
			name: "hardware and network tuning",
			opts: EmulatorOpts{GPU: "swiftshader_indirect", MemoryMB: 4096, Cores: 2,
				NetSpeed: "lte", NetDelay: "none", DNS: "1.1.1.1", HTTPProxy: "127.0.0.1:8080", SELinux: "permissive"},
			want: []string{"-avd", "Pixel_8", "-port", "5554",
				"-gpu", "swiftshader_indirect", "-memory", "4096", "-cores", "2",
				"-netspeed", "lte", "-netdelay", "none", "-dns-server", "1.1.1.1",
				"-http-proxy", "127.0.0.1:8080", "-selinux", "permissive"},
		},
		{
			name: "extra passes through last",
			opts: EmulatorOpts{NoWindow: true, Extra: []string{"-verbose", "", "  "}},
			want: []string{"-avd", "Pixel_8", "-port", "5554", "-no-window", "-verbose"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EmulatorArgs("Pixel_8", 5554, tc.opts)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("EmulatorArgs()\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

// -no-snapshot disables the snapshot machinery outright, so emitting the finer
// snapshot flags alongside it would be contradictory noise in the shown command.
func TestEmulatorArgsNoSnapshotSuppressesSnapshotDetail(t *testing.T) {
	got := EmulatorArgs("X", 0, EmulatorOpts{NoSnapshot: true, ColdBoot: true, Snapshot: "s", NoSnapshotSave: true})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-no-snapshot") {
		t.Fatalf("want -no-snapshot, got %v", got)
	}
	for _, bad := range []string{"-no-snapshot-load", "-no-snapshot-save", "-snapshot s"} {
		if strings.Contains(joined, bad) {
			t.Errorf("%q must not appear alongside -no-snapshot: %v", bad, got)
		}
	}
}

// port 0 means "let the emulator choose", and must not emit -port 0.
func TestEmulatorArgsOmitsZeroPort(t *testing.T) {
	got := EmulatorArgs("X", 0, EmulatorOpts{})
	if strings.Contains(strings.Join(got, " "), "-port") {
		t.Errorf("port 0 must not produce a -port flag: %v", got)
	}
}

func TestEmulatorArgsIsDeterministic(t *testing.T) {
	o := EmulatorOpts{ColdBoot: true, NoWindow: true, GPU: "host", MemoryMB: 2048, DNS: "8.8.8.8"}
	first := strings.Join(EmulatorArgs("A", 5556, o), " ")
	for i := 0; i < 20; i++ {
		if got := strings.Join(EmulatorArgs("A", 5556, o), " "); got != first {
			t.Fatalf("argument order is not stable:\n%s\n%s", first, got)
		}
	}
}

func TestEmulatorCommandQuotesAndFallsBack(t *testing.T) {
	cmd := EmulatorCommand("/Users/me/Library/Android/sdk/emulator/emulator", "Pixel 8", 5554, EmulatorOpts{})
	if !strings.Contains(cmd, "'Pixel 8'") {
		t.Errorf("an AVD name with a space must be quoted: %s", cmd)
	}
	if bare := EmulatorCommand("", "X", 0, EmulatorOpts{}); !strings.HasPrefix(bare, "emulator -avd X") {
		t.Errorf("an unknown binary must still render a pasteable command: %s", bare)
	}
	// A displayed command must never leak anything but flags the user chose.
	if strings.Contains(cmd, "token") || strings.Contains(cmd, "password") {
		t.Error("command contains unexpected content")
	}
}

func TestSerialPortRoundTrip(t *testing.T) {
	if got := SerialForPort(5556); got != "emulator-5556" {
		t.Errorf("SerialForPort = %q", got)
	}
	if got := PortForSerial("emulator-5556"); got != 5556 {
		t.Errorf("PortForSerial = %d", got)
	}
	// Synthetic serials only: a test fixture is a bad place for a real device's
	// identity, and the shape is all this assertion needs.
	for _, s := range []string{"abcdef0123456789", "192.168.1.5:5555", "emulator-abc", ""} {
		if got := PortForSerial(s); got != 0 {
			t.Errorf("PortForSerial(%q) = %d, want 0", s, got)
		}
	}
}

// The emulator only accepts even console ports in [5554, 5584]; an allocator
// that drifts outside that range hands back a serial adb will never show.
func TestAllocConsolePortStaysInRange(t *testing.T) {
	p, err := allocConsolePort()
	if err != nil {
		t.Skipf("no free console port on this machine: %v", err)
	}
	if p < emulatorPortMin || p > emulatorPortMax || p%2 != 0 {
		t.Errorf("allocConsolePort() = %d, want an even port in [%d, %d]", p, emulatorPortMin, emulatorPortMax)
	}
}
