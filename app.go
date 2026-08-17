package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"adbq/internal/adb"
	"adbq/internal/version"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// srEntry ties a running screen-record session to its task ID so StopScreenRecord
// can mark the right task as complete even when multiple devices record at once.
type srEntry struct {
	sess   *adb.ScreenRecordSession
	taskID string
}

// App is the Wails bindings entrypoint. All exported (capitalized) methods are
// invocable from JS via the generated bindings.
type App struct {
	ctx    context.Context
	client *adb.Client
	tasks  *adb.TaskManager

	mu sync.Mutex
	// lcMu serializes whole start/stop sequences for logcat feeds. Wails runs
	// every binding call on its own goroutine, so two concurrent StartLogcat
	// calls (React StrictMode does exactly that on mount) could both clear the
	// old feed before either registered its own, leaving an orphaned adb stream
	// emitting into the same event — every line delivered twice. `mu` cannot do
	// this job: starting a feed talks to the device, and holding the general
	// lock across that would stall every other binding.
	lcMu        sync.Mutex
	logcats     map[string]*logcatFeed
	shellMu     sync.Mutex
	shells      map[string]*adb.ShellSession
	shellSerial int

	srMu   sync.Mutex
	srSess map[string]*srEntry // serial → active recording

	scrcpy   *adb.ScrcpyManager
	sessions *adb.SessionStore
	icons    *adb.IconCache
	profiles *adb.ProfileStore
	frida    *adb.FridaStore
	host     *adb.HostStore
	sdk      *adb.SDKManager
	emu      *adb.EmulatorManager
	pkgs     *adb.PackageManager

	fridaMu   sync.Mutex
	fridaSess map[string]*adb.FridaSession
	fridaSeq  int

	dnsMu   sync.Mutex
	dnsSnif map[string]*adb.DNSSnifferStream

	procMu      sync.Mutex
	procStreams map[string]*adb.TopStream
}

func NewApp() *App {
	store, _ := adb.NewSessionStore()
	profiles, _ := adb.NewProfileStore()
	frida, _ := adb.NewFridaStore()
	host := adb.NewHostStore()
	sdk := adb.NewSDKManager(host)
	client := adb.NewClient()
	return &App{
		client:      client,
		host:        host,
		sdk:         sdk,
		emu:         adb.NewEmulatorManager(sdk, client),
		pkgs:        adb.NewPackageManager(sdk),
		tasks:       adb.NewTaskManager(),
		logcats:     map[string]*logcatFeed{},
		shells:      map[string]*adb.ShellSession{},
		srSess:      map[string]*srEntry{},
		scrcpy:      adb.NewScrcpyManager(),
		sessions:    store,
		icons:       adb.NewIconCache(),
		profiles:    profiles,
		frida:       frida,
		fridaSess:   map[string]*adb.FridaSession{},
		dnsSnif:     map[string]*adb.DNSSnifferStream{},
		procStreams: map[string]*adb.TopStream{},
	}
}

// AppIcon returns a data URI for the app's launcher icon (or empty string).
func (a *App) AppIcon(serial, pkg string) (string, error) {
	if a.icons == nil {
		return "", nil
	}
	return a.client.IconFor(a.ctx, a.icons, serial, pkg)
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.tasks.OnUpdate(func(t *adb.TaskState) {
		runtime.EventsEmit(a.ctx, "task:update", t)
	})
	// An explicit adb path from Settings must win before the first adb call, or
	// the client caches whichever binary it found on PATH for the whole session.
	if p := strings.TrimSpace(a.host.Get().ADBPath); p != "" {
		a.client.SetBinary(p)
	}
	_ = a.client.StartServer(ctx)
	// Reconcile persisted sessions: anything we left running on a device when
	// adbq crashed/closed comes back as a task entry the user can see.
	go a.reconcileSessions()
}

// reconcileSessions walks ~/.adbq/sessions.json and either re-attaches each
// entry as a live task (if still running on the device) or surfaces it as a
// "Recovered — output still on device" entry.
func (a *App) reconcileSessions() {
	if a.sessions == nil {
		return
	}
	// Give adb server a beat to be ready.
	time.Sleep(500 * time.Millisecond)
	for _, sess := range a.sessions.List() {
		switch sess.Kind {
		case "capture":
			st, err := a.client.CaptureStatus(a.ctx, sess.Serial, "", "")
			if err != nil || st == nil {
				// device offline — keep manifest entry; we'll retry next launch.
				continue
			}
			if st.Active {
				id, _ := a.tasks.Create("capture", "tcpdump (recovered)", sess.Serial+" · "+sess.RemoteFile)
				a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = "PID " + itoa(int64(st.PID)) + " · still running" })
			} else {
				// Process gone — see if pcap is still on the device.
				st2, _ := a.client.CaptureStatus(a.ctx, sess.Serial, "", "")
				if st2 != nil && st2.SizeBytes > 0 {
					id, _ := a.tasks.Create("capture", "tcpdump (recovered)", sess.Serial)
					a.tasks.Finish(id, "ok", st2.RemoteFile, "process exited; pcap left at "+st2.RemoteFile+" ("+itoa(st2.SizeBytes)+" bytes)")
				}
				a.sessions.Remove(sess.ID)
			}
		case "screen-record":
			// We can't probe screenrecord PID portably; just surface the file
			// existence and let the user pull it.
			out, _ := a.client.Shell(a.ctx, sess.Serial, "ls -l "+sess.RemoteFile+" 2>/dev/null")
			if strings.TrimSpace(out) != "" {
				id, _ := a.tasks.Create("screen-record", "Recording (recovered)", sess.Serial)
				a.tasks.Finish(id, "ok", sess.RemoteFile, "screenrecord output left on device — pull it from Files")
			}
			a.sessions.Remove(sess.ID)
		case "frida":
			servers, _ := a.client.ListFridaServers(a.ctx, sess.Serial)
			active := false
			for _, fs := range servers {
				if fs.Active {
					active = true
					break
				}
			}
			id, _ := a.tasks.Create("frida", "frida-server (recovered)", sess.Serial)
			if active {
				a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = "still running on " + sess.Serial })
			} else {
				a.tasks.Finish(id, "ok", "", "no frida-server detected — likely stopped")
				a.sessions.Remove(sess.ID)
			}
		}
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Version returns the build's semver tag.
func (a *App) Version() string { return version.Version }

// ListTasks returns all tracked long-running operations.
func (a *App) ListTasks() []adb.TaskState { return a.tasks.List() }
func (a *App) CancelTask(id string)       { a.tasks.Cancel(id) }
func (a *App) RemoveTask(id string)       { a.tasks.Remove(id) }

// ─── Android SDK / Android Studio (host toolchain) ──────────────────────

// AndroidSDK reports the Android SDK / Studio toolchain found on this computer.
func (a *App) AndroidSDK() adb.AndroidSDKInfo { return a.sdk.Info() }

// RecheckAndroidSDK re-probes after the user installs something or changes the
// SDK path, so they don't have to restart adbq.
func (a *App) RecheckAndroidSDK() adb.AndroidSDKInfo { return a.sdk.Recheck() }

// HostSettings returns the user's host-machine overrides.
func (a *App) HostSettings() adb.HostSettings { return a.host.Get() }

// SetSDKRoot pins the Android SDK location. An empty string clears the override
// and returns adbq to auto-detection.
func (a *App) SetSDKRoot(path string) (adb.AndroidSDKInfo, error) {
	hs := a.host.Get()
	hs.SDKRoot = strings.TrimSpace(path)
	if err := a.host.Set(hs); err != nil {
		return adb.AndroidSDKInfo{}, err
	}
	return a.sdk.Recheck(), nil
}

// PickSDKRoot opens a folder chooser for the Android SDK root.
func (a *App) PickSDKRoot() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select the Android SDK folder",
	})
}

// OpenAndroidStudio launches the IDE, for the few things adbq deliberately
// leaves to Studio (project work, deep AVD hardware editing).
func (a *App) OpenAndroidStudio() error {
	info := a.sdk.Info()
	if info.StudioPath == "" {
		return fmt.Errorf("Android Studio not found on this computer")
	}
	return a.OpenPath(info.StudioPath)
}

// ─── Emulators / AVDs ───────────────────────────────────────────────────

// ListAVDs returns every AVD defined on this machine with its live state.
func (a *App) ListAVDs() ([]adb.AVD, error) { return a.emu.ListAVDs(a.ctx) }

// AVDDetail returns one AVD, for the detail panel.
func (a *App) AVDDetail(name string) (*adb.AVD, error) { return a.emu.AVDByName(a.ctx, name) }

// EmulatorLaunchCommand renders the exact command StartAVD would run, so the UI
// can show it live as the user toggles boot options (CLAUDE.md §4.1).
func (a *App) EmulatorLaunchCommand(name string, opts adb.EmulatorOpts) string {
	return adb.EmulatorCommand(a.sdk.Info().Emulator, name, 0, opts)
}

// StartAVD boots an AVD and waits for it to come up, reporting progress as a task.
func (a *App) StartAVD(name string, opts adb.EmulatorOpts) (string, error) {
	id, ctx := a.tasks.Create("emulator-start", "Start "+name, "launching")
	serial, err := a.emu.Start(ctx, name, opts)
	if err != nil {
		a.tasks.Finish(id, "err", "", err.Error())
		return "", err
	}
	// Booting takes minutes; let the binding return so the UI stays responsive
	// and report the rest through the task.
	go func() {
		werr := a.emu.WaitForBoot(ctx, serial, func(stage string) {
			a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = stage })
		})
		if werr != nil {
			a.tasks.Finish(id, "err", "", werr.Error())
			return
		}
		a.tasks.Finish(id, "ok", "", serial+" ready")
	}()
	return serial, nil
}

// StopAVD shuts an emulator down gracefully via the console.
func (a *App) StopAVD(name string) error { return a.emu.Stop(a.ctx, name) }

// EmulatorLog returns emulator output newer than sinceSeq. The UI polls this
// only while the log panel is open, so a closed panel costs nothing.
func (a *App) EmulatorLog(name string, sinceSeq int) []adb.HostLogLine {
	return a.emu.LogSince(name, sinceSeq)
}

// ClearEmulatorLog empties one AVD's log buffer.
func (a *App) ClearEmulatorLog(name string) { a.emu.ClearLog(name) }

// ─── System images and AVD creation ─────────────────────────────────────

// ListInstalledSystemImages reads the SDK tree directly — instant, offline, and
// always correct about what is already on disk.
func (a *App) ListInstalledSystemImages() []adb.SystemImage { return a.pkgs.ListInstalledImages() }

// ListSystemImages merges the installed images with everything installable.
// The remote half is cached; pass refresh to force a re-fetch.
func (a *App) ListSystemImages(refresh bool) ([]adb.SystemImage, error) {
	return a.pkgs.ListSystemImages(a.ctx, refresh)
}

// InstallSystemImage downloads a system image, reporting progress as a task.
func (a *App) InstallSystemImage(pkg string) error {
	id, ctx := a.tasks.Create("sdk-install", "Install "+pkg, "starting")
	go func() {
		err := a.pkgs.InstallSystemImage(ctx, pkg, func(stage string, pct int) {
			a.tasks.Update(id, func(t *adb.TaskState) {
				t.Detail = stage
				t.Progress = pct
			})
		})
		if err != nil {
			a.tasks.Finish(id, "err", "", err.Error())
			return
		}
		a.tasks.Finish(id, "ok", "", "installed "+pkg)
	}()
	return nil
}

// UninstallSystemImage removes an installed image. Destructive — the UI must
// confirm first, showing the command.
func (a *App) UninstallSystemImage(pkg string) error {
	return a.pkgs.UninstallSystemImage(a.ctx, pkg)
}

// ListDeviceProfiles returns the hardware definitions available when creating.
func (a *App) ListDeviceProfiles() ([]adb.DeviceProfile, error) {
	return a.emu.ListDeviceProfiles(a.ctx)
}

// HostABIs lists the Android ABIs this computer can run, best first, so the UI
// can explain why an x86 image is not an option on an arm64 machine.
func (a *App) HostABIs() []string { return adb.HostABIs() }

// DefaultAVDSpec proposes sensible settings for a new AVD from a chosen system
// image, so the create form opens filled in rather than blank.
func (a *App) DefaultAVDSpec(pkg string) (adb.AVDSpec, error) {
	var img adb.SystemImage
	for _, i := range a.pkgs.ListInstalledImages() {
		if i.Pkg == pkg {
			img = i
			break
		}
	}
	if img.Pkg == "" {
		return adb.AVDSpec{}, fmt.Errorf("%q is not an installed system image", pkg)
	}
	profiles, _ := a.emu.ListDeviceProfiles(a.ctx)
	return adb.DefaultAVDSpec(img, profiles), nil
}

// CreateAVDCommand renders the creation command for the confirm dialog.
func (a *App) CreateAVDCommand(spec adb.AVDSpec) string {
	return adb.CreateAVDCommand(a.sdk.Info().AVDManager, spec)
}

// DeleteAVDCommand renders the deletion command for the confirm dialog.
func (a *App) DeleteAVDCommand(name string) string {
	return adb.DeleteAVDCommand(a.sdk.Info().AVDManager, name)
}

// CreateAVD creates a new AVD and returns it.
func (a *App) CreateAVD(spec adb.AVDSpec) (*adb.AVD, error) { return a.emu.CreateAVD(a.ctx, spec) }

// DeleteAVD removes an AVD and its data. Irreversible.
func (a *App) DeleteAVD(name string) error { return a.emu.DeleteAVD(a.ctx, name) }

// DeleteAVDSnapshot removes one saved snapshot.
func (a *App) DeleteAVDSnapshot(name, snapshot string) error {
	return a.emu.DeleteSnapshot(name, snapshot)
}

// AVDHardwareChanges previews exactly which config.ini keys an edit would
// write, so the UI can show them before anything is saved (CLAUDE.md §4.1).
func (a *App) AVDHardwareChanges(hw adb.AVDHardware) (map[string]string, error) {
	return adb.AVDHardwareChanges(hw)
}

// UpdateAVDHardware applies CPU/RAM/disk/display changes to an AVD. Changes
// take effect the next time it boots.
func (a *App) UpdateAVDHardware(name string, hw adb.AVDHardware) (*adb.AVD, error) {
	return a.emu.UpdateAVDHardware(a.ctx, name, hw)
}

// ─── rootAVD (third-party, user-consented) ──────────────────────────────

// RootAVDInfo describes the tool, its provenance and the risks, for the consent
// dialog shown before anything is downloaded.
func (a *App) RootAVDInfo() adb.RootAVDInfo { return adb.RootAVDStatus() }

// RootAVDAdvice says whether rooting is needed, possible, or pointless for one
// AVD, and why. The UI shows the reason verbatim.
func (a *App) RootAVDAdvice(name string) (map[string]string, error) {
	avd, err := a.emu.AVDByName(a.ctx, name)
	if err != nil {
		return nil, err
	}
	action, reason := adb.RootAVDAdvice(avd.API, avd.PlayStore, avd.Root, avd.Patched)
	return map[string]string{
		"action":  action,
		"reason":  reason,
		"offered": boolString(adb.RootAVDOffered(action)),
	}, nil
}

// DownloadRootAVD fetches and verifies the pinned rootAVD tree. The caller must
// have shown RootAVDInfo.Disclosures and obtained consent first.
func (a *App) DownloadRootAVD() (adb.RootAVDInfo, error) {
	id, ctx := a.tasks.Create("rootavd-download", "Download rootAVD", "starting")
	info, err := adb.InstallRootAVD(ctx, func(stage string) {
		a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = stage })
	})
	if err != nil {
		a.tasks.Finish(id, "err", "", err.Error())
		return adb.RootAVDInfo{}, err
	}
	a.tasks.Finish(id, "ok", "", "verified "+info.Commit[:12])
	return info, nil
}

// RemoveRootAVD deletes the downloaded copy of the tool.
func (a *App) RemoveRootAVD() error { return adb.RemoveRootAVD() }

// RootAVDCommand renders the command that would run, for the confirm dialog.
func (a *App) RootAVDCommand(name string, restore bool) (string, error) {
	avd, err := a.emu.AVDByName(a.ctx, name)
	if err != nil {
		return "", err
	}
	return adb.RootAVDCommand(adb.RootAVDStatus().Dir, avd.RamdiskRel, restore), nil
}

// RootAVD patches an AVD's system image with Magisk, cold-boots it and verifies
// root. Reported as a task; the transcript lands in the AVD's own log.
func (a *App) RootAVD(name string) error {
	a.runRootAVDTask("avd-root", "Root "+name, name, false)
	return nil
}

// RestoreAVDRamdisk reverts a rootAVD patch from the backup it left behind.
func (a *App) RestoreAVDRamdisk(name string) error {
	a.runRootAVDTask("avd-restore", "Restore "+name, name, true)
	return nil
}

func (a *App) runRootAVDTask(kind, title, name string, restore bool) {
	id, ctx := a.tasks.Create(kind, title, "starting")
	go func() {
		fn := a.emu.RootAVD
		if restore {
			fn = a.emu.RestoreAVDRamdisk
		}
		err := fn(ctx, name, func(stage string) {
			a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = stage })
		})
		if err != nil {
			a.tasks.Finish(id, "err", "", err.Error())
			return
		}
		a.tasks.Finish(id, "ok", "", name+" done")
	}()
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ─── scrcpy ─────────────────────────────────────────────────────────────

func (a *App) ScrcpyAvailable() bool           { return a.scrcpy.Available() }
func (a *App) ScrcpyActive(serial string) bool { return a.scrcpy.IsActive(serial) }
func (a *App) StartScrcpy(serial string) error {
	// Reasonable defaults: cap framerate, prefer h264 to avoid codec issues, USB if multiple.
	return a.scrcpy.Start(a.ctx, serial, []string{
		"--max-fps", "30",
		"--video-codec", "h264",
		"--window-title", "adbq · " + serial,
	})
}
func (a *App) StopScrcpy(serial string) error { return a.scrcpy.Stop(serial) }

// ─── Hosts persistence ─────────────────────────────────────────────────

func (a *App) SaveHostsConfig(serial, content string) error {
	return adb.SaveHostsConfig(serial, content)
}
func (a *App) LoadHostsConfig(serial string) (string, error) {
	return adb.LoadHostsConfig(serial)
}

// ApplyHostsConfig pushes the persisted hosts content to the device, trying
// every reasonable strategy (direct write, magisk remount, system-as-root
// remount, /system remount, bind-mount, then Magisk module scaffolding) and
// verifying via md5 readback. Returns the full result so the UI can show
// which strategy worked and whether a reboot is required.
func (a *App) ApplyHostsConfig(serial string) (*adb.HostsApplyResult, error) {
	content, err := adb.LoadHostsConfig(serial)
	if err != nil {
		return nil, err
	}
	if content == "" {
		return nil, fmt.Errorf("no saved hosts config for %s", serial)
	}
	return a.client.ApplyHostsRobust(a.ctx, serial, content)
}

// FlushDeviceDNS clears the netd resolver cache so a fresh /etc/hosts entry
// takes effect on running connections.
func (a *App) FlushDeviceDNS(serial string) (string, error) {
	return a.client.FlushDNS(a.ctx, serial)
}

// HostsDrifted compares the persisted config with the device's current hosts.
// Returns true when they differ (i.e. device was rebooted and reverted).
func (a *App) HostsDrifted(serial string) (bool, error) {
	saved, err := adb.LoadHostsConfig(serial)
	if err != nil || saved == "" {
		return false, err
	}
	// Use root read so /system is readable even if mode bits are weird.
	out, _, err := a.client.ShellSU(a.ctx, serial, "cat /system/etc/hosts")
	if err != nil {
		// not rooted or cat failed; treat as undrifted to avoid noisy prompts
		return false, nil
	}
	return strings.TrimSpace(out) != strings.TrimSpace(saved), nil
}

// DNSLookup resolves a hostname on the device. Honors /etc/hosts first via
// getent so users can verify their host overrides.
func (a *App) DNSLookup(serial, host string) (string, error) {
	return a.client.DNSLookup(a.ctx, serial, host)
}

// HostLANIPs lists the host machine's non-loopback IPv4 addresses, preferring
// ones on the same /24 as the device.
func (a *App) HostLANIPs(deviceIP string) []string { return adb.HostLANIPs(deviceIP) }

// SuggestProxyHost returns the most sensible proxy host string for the device.
// USB transports return 127.0.0.1 (a reverse forward should be added on top);
// Wi-Fi transports return the host's LAN IP closest to the device subnet.
type ProxySuggestion struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	NeedsReverse bool   `json:"needsReverse"`
	Reason       string `json:"reason"`
}

func (a *App) SuggestProxyHost(serial string, port int) (*ProxySuggestion, error) {
	if port <= 0 {
		port = 8080
	}
	devs, err := a.client.ListDevices(a.ctx)
	if err != nil {
		return nil, err
	}
	var d *adb.Device
	for i := range devs {
		if devs[i].ID == serial {
			d = &devs[i]
			break
		}
	}
	if d == nil {
		return nil, fmt.Errorf("device %s not connected", serial)
	}
	if d.Via != "Wi-Fi" {
		// USB / emulator → suggest 127.0.0.1 with reverse forward
		fwds, _ := a.client.ListReverses(a.ctx, serial)
		hasRev := false
		want := fmt.Sprintf("tcp:%d", port)
		for _, f := range fwds {
			if f.Remote == want || f.Local == want {
				hasRev = true
				break
			}
		}
		return &ProxySuggestion{
			Host: "127.0.0.1", Port: port,
			NeedsReverse: !hasRev,
			Reason:       "USB transport — device sees host via adb reverse tcp:" + fmt.Sprint(port),
		}, nil
	}
	// Wi-Fi: pick host LAN IP closest to device subnet
	a.client.Enrich(a.ctx, d)
	ips := adb.HostLANIPs(d.IP)
	if len(ips) == 0 {
		return &ProxySuggestion{Host: "", Port: port, Reason: "no host LAN IP detected"}, nil
	}
	return &ProxySuggestion{Host: ips[0], Port: port, Reason: "Wi-Fi transport — using host LAN IP " + ips[0]}, nil
}

// ─── Capture lifecycle ─────────────────────────────────────────────────

func (a *App) StartCapture(serial, iface, bpf string) (*adb.CaptureState, error) {
	st, err := a.client.StartCapture(a.ctx, serial, iface, bpf)
	if err != nil {
		return st, err
	}
	if st != nil && st.Active && a.sessions != nil {
		a.sessions.Put(adb.Session{
			ID: "cap:" + serial, Kind: "capture", Serial: serial,
			RemoteFile: st.RemoteFile, Marker: iface + " | " + bpf,
		})
	}
	return st, nil
}
func (a *App) StopCapture(serial string) (*adb.CaptureState, error) {
	st, err := a.client.StopCapture(a.ctx, serial)
	if a.sessions != nil {
		a.sessions.Remove("cap:" + serial)
	}
	return st, err
}
func (a *App) CaptureStatus(serial string) (*adb.CaptureState, error) {
	st, err := a.client.CaptureStatus(a.ctx, serial, "", "")
	if err != nil || st == nil {
		return st, err
	}
	if a.sessions != nil {
		if _, ok := a.sessions.FindByKindSerial("capture", serial); ok {
			st.OurSession = true
		}
	}
	if st.Active && !st.OurSession {
		st.Warning = "External tcpdump detected — not started by adbq."
	}
	return st, nil
}

// ProbeTcpdump reports which tcpdump binary (if any) is installed on the
// device. The Capture UI uses this to decide whether to show the "install
// tcpdump" affordance.
func (a *App) ProbeTcpdump(serial string) (*adb.TcpdumpInfo, error) {
	return a.client.ProbeTcpdump(a.ctx, serial)
}

// PlanTcpdumpAutoInstall returns the manifest entry that auto-install will
// fetch for this device. The UI shows the URL + hash to the user before any
// network request is made, per CLAUDE.md §1.2.
func (a *App) PlanTcpdumpAutoInstall(serial string) (*adb.TcpdumpAutoPlan, error) {
	return a.client.PlanTcpdumpAutoInstall(a.ctx, serial)
}

// InstallTcpdumpAuto downloads (with SHA256 verification), pushes and chmods
// the manifest-pinned tcpdump matching the device ABI. confirmed must be
// true — pass it only after the user accepted the dialog backed by
// PlanTcpdumpAutoInstall.
func (a *App) InstallTcpdumpAuto(serial string, confirmed bool) (*adb.TcpdumpInfo, error) {
	return a.client.InstallTcpdumpAuto(a.ctx, serial, confirmed)
}

// InstallTcpdumpWithPicker prompts for a local tcpdump binary and installs it
// to /data/local/tmp/tcpdump (chmod 755 + arch sanity check). We intentionally
// do NOT auto-download from the internet: project rules forbid pulling binary
// blobs from unverified sources, and the user picking the file keeps them in
// the loop about provenance.
func (a *App) InstallTcpdumpWithPicker(serial string) (*adb.TcpdumpInfo, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select tcpdump binary (arm64/arm/x86_64 matching the device)",
	})
	if err != nil || path == "" {
		return nil, err
	}
	if _, err := a.client.InstallTcpdump(a.ctx, serial, path); err != nil {
		return nil, err
	}
	return a.client.ProbeTcpdump(a.ctx, serial)
}

// StartLiveCapture begins an in-process gopacket-decoded capture for serial.
// Packets are emitted in batched events on "pcap:" + serial; the binary pcap
// stream is mirrored to a host-side file (state.PcapPath) so SaveLivePcap
// is a no-op copy.
func (a *App) StartLiveCapture(serial, iface, bpf string, opts adb.LiveCaptureOptions) (*adb.LiveCaptureState, error) {
	return a.client.StartLiveCapture(a.ctx, serial, iface, bpf, opts, func(batch []*adb.LivePacket) {
		runtime.EventsEmit(a.ctx, "pcap:"+serial, batch)
	})
}

// StopLiveCapture ends the in-process capture for serial. Safe to call when
// no capture is running.
func (a *App) StopLiveCapture(serial string) error {
	if a.client.Live == nil {
		return nil
	}
	return a.client.Live.Stop(serial)
}

// LiveCaptureStatus returns the current per-serial state (Active=false when
// nothing is running).
func (a *App) LiveCaptureStatus(serial string) *adb.LiveCaptureState {
	if a.client.Live == nil {
		return &adb.LiveCaptureState{}
	}
	return a.client.Live.Status(serial)
}

// DescribeLivePacket re-decodes packet number `no` from the live capture's
// recent ring buffer (per-session). Returns nil when the packet has aged out.
func (a *App) DescribeLivePacket(serial string, no uint64) *adb.LivePacketDetail {
	if a.client.Live == nil {
		return nil
	}
	return a.client.Live.DescribeLivePacket(serial, no)
}

// SaveLivePcap copies the host-side pcap mirror to a user-chosen location.
func (a *App) SaveLivePcap(serial string) (string, error) {
	if a.client.Live == nil {
		return "", fmt.Errorf("no live capture has been started for %s", serial)
	}
	st := a.client.Live.Status(serial)
	if st == nil || st.PcapPath == "" {
		return "", fmt.Errorf("no pcap mirror available for %s", serial)
	}
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save live capture (.pcap)",
		DefaultFilename: "adbq-live-" + serial + ".pcap",
		Filters:         []runtime.FileFilter{{DisplayName: "PCAP", Pattern: "*.pcap"}},
	})
	if err != nil || dst == "" {
		return "", err
	}
	in, err := os.Open(st.PcapPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return dst, nil
}

// ─── iptables bindings ─────────────────────────────────────────────────
// All writes go through `su -c` server-side, snapshot the full save-blob
// onto a per-(serial, family) undo ring before applying, and never glue
// untrusted strings into the shell command (specs are validated).

func (a *App) ProbeIptables(serial, family string) (*adb.IPTBackendInfo, error) {
	return a.client.ProbeIptables(a.ctx, serial, adb.IPFamily(family))
}
func (a *App) ListIptables(serial, family, table string) (*adb.IPTSnapshot, error) {
	return a.client.ListIptables(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table))
}
func (a *App) AppendIptablesRule(serial, family, table, chain string, spec []string) (*adb.IPTSnapshot, error) {
	return a.client.AppendIptablesRule(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table), chain, adb.IptablesSpec(spec))
}
func (a *App) InsertIptablesRule(serial, family, table, chain string, pos int, spec []string) (*adb.IPTSnapshot, error) {
	return a.client.InsertIptablesRule(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table), chain, pos, adb.IptablesSpec(spec))
}
func (a *App) DeleteIptablesRule(serial, family, table, chain string, num int) (*adb.IPTSnapshot, error) {
	return a.client.DeleteIptablesRule(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table), chain, num)
}
func (a *App) FlushIptables(serial, family, table, chain string) (*adb.IPTSnapshot, error) {
	return a.client.FlushIptables(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table), chain)
}
func (a *App) SetIptablesPolicy(serial, family, table, chain, policy string) error {
	return a.client.SetIptablesPolicy(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table), chain, policy)
}
func (a *App) CreateIptablesChain(serial, family, table, chain string) error {
	return a.client.CreateIptablesChain(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table), chain)
}
func (a *App) DeleteIptablesChain(serial, family, table, chain string) error {
	return a.client.DeleteIptablesChain(a.ctx, serial, adb.IPFamily(family), adb.IPTable(table), chain)
}
func (a *App) ExportIptables(serial, family string) (string, error) {
	return a.client.ExportIptables(a.ctx, serial, adb.IPFamily(family))
}
func (a *App) ImportIptables(serial, family, blob string) error {
	return a.client.ImportIptables(a.ctx, serial, adb.IPFamily(family), blob)
}
func (a *App) UndoIptables(serial, family string) (*adb.IPTSnapshot, error) {
	return a.client.UndoIptables(a.ctx, serial, adb.IPFamily(family))
}

// KillExternalCapture stops any tcpdump matching our capture path even when we
// didn't start it. Used to clean up stale processes from prior crashes.
func (a *App) KillExternalCapture(serial string) (*adb.CaptureState, error) {
	st, err := a.client.StopCapture(a.ctx, serial)
	if a.sessions != nil {
		a.sessions.Remove("cap:" + serial)
	}
	return st, err
}

// AdoptExternalCapture marks the currently-running tcpdump as ours so the UI
// shows it as part of our session lifecycle.
func (a *App) AdoptExternalCapture(serial string) error {
	st, err := a.client.CaptureStatus(a.ctx, serial, "", "")
	if err != nil {
		return err
	}
	if st == nil || !st.Active {
		return fmt.Errorf("no tcpdump running on %s", serial)
	}
	if a.sessions != nil {
		a.sessions.Put(adb.Session{
			ID: "cap:" + serial, Kind: "capture", Serial: serial,
			RemoteFile: st.RemoteFile, Marker: "adopted",
		})
	}
	return nil
}

// PullCapture pulls /sdcard/adbq-capture.pcap to a user-chosen host file.
func (a *App) PullCapture(serial string) (string, error) {
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save pcap as…",
		DefaultFilename: "adbq-capture-" + serial + ".pcap",
	})
	if err != nil || dst == "" {
		return "", err
	}
	return a.client.PullFile(a.ctx, serial, "/sdcard/adbq-capture.pcap", dst)
}

func (a *App) shutdown(ctx context.Context) {
	a.scrcpy.StopAll()
	// Only emulators adbq launched are killed; one the user started from
	// Android Studio is theirs and must survive adbq closing.
	a.emu.StopAll()
	// lcMu keeps a StartLogcat that is mid-flight (blocked on a slow device)
	// from re-registering its feed into the map we are about to clear, which
	// would leave an adb child running past exit.
	a.lcMu.Lock()
	defer a.lcMu.Unlock()
	a.mu.Lock()
	feeds := make([]*logcatFeed, 0, len(a.logcats))
	for _, f := range a.logcats {
		feeds = append(feeds, f)
	}
	a.logcats = map[string]*logcatFeed{}
	a.mu.Unlock()
	// Stop outside the lock: each Stop waits for its batcher goroutine.
	for _, f := range feeds {
		f.Stop()
	}
	a.shellMu.Lock()
	for _, sh := range a.shells {
		sh.Stop()
	}
	a.shellMu.Unlock()
	// Stop frida driver processes so we don't orphan a python child (and leave a
	// gum agent resident in the target) when adbq closes.
	a.fridaMu.Lock()
	for _, fs := range a.fridaSess {
		fs.Stop()
	}
	a.fridaMu.Unlock()
}

// ─── Devices ─────────────────────────────────────────────────────────────

func (a *App) ListDevices() ([]adb.Device, error) {
	devs, err := a.client.ListDevices(a.ctx)
	if err != nil {
		return nil, err
	}
	for i := range devs {
		a.client.Enrich(a.ctx, &devs[i])
	}
	return devs, nil
}

func (a *App) DeviceDetails(serial string) (*adb.Device, error) {
	devs, err := a.client.ListDevices(a.ctx)
	if err != nil {
		return nil, err
	}
	for i := range devs {
		if devs[i].ID == serial {
			a.client.Enrich(a.ctx, &devs[i])
			return &devs[i], nil
		}
	}
	return nil, fmt.Errorf("device %s not connected", serial)
}

func (a *App) ConnectTCP(addr string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
	defer cancel()
	return a.client.Connect(ctx, addr)
}
func (a *App) DisconnectDevice(addr string) (string, error) { return a.client.Disconnect(a.ctx, addr) }
func (a *App) GetStats(serial string) (*adb.Stats, error)   { return a.client.GetStats(a.ctx, serial) }
func (a *App) ADBVersion() (string, error)                  { return a.client.ServerVersion(a.ctx) }

// ─── Logcat streaming via events ────────────────────────────────────────

// restartLogcatLocked replaces whatever feed a device has with a fresh one.
// Callers must hold lcMu.
func (a *App) restartLogcatLocked(serial, pkgFilter string, showSystem bool) error {
	a.stopLogcatFeed(serial)
	// A short tail gives the screen something to show immediately instead of an
	// empty pane while waiting for the device to say something.
	f, err := a.startLogcatFeed(serial, pkgFilter, showSystem, 100)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.logcats[serial] = f
	a.mu.Unlock()
	return nil
}

// EnsureLogcat guarantees a live feed for this device and filter. It is the
// UI's only entry point for starting logs — on mount, on a filter change, on
// anything — and it is deliberately idempotent, because the alternative (a
// separate stop call followed by a start call) is two independent binding
// invocations that Wails dispatches on separate goroutines with no ordering
// guarantee: the stop can land after the start and silently kill the feed the
// UI just asked for.
//
// Re-subscribing on the JS side alone is not enough to trust either: the event
// listener can be perfectly valid while the feed behind it is gone (backend
// restarted during development, adb server bounced, device replugged), leaving
// the screen empty forever. Only the backend knows whether a feed is really
// alive, so it decides here. A healthy feed with the same filter is left
// running untouched — no restart, no re-delivered tail, no duplicates.
//
// showSystem controls whether OS-owned lines are delivered at all: with it off
// the feed drops them host-side, which is what keeps a chatty device (kernel
// audit spam and friends) from drowning the UI. Lines arrive batched as
// `logcat:<serial>` events carrying an array of entries.
// It reports whether it had to (re)start the feed, which is the frontend's cue
// to drop its buffer — a restarted feed re-delivers a tail, and keeping the old
// lines around would show them twice.
func (a *App) EnsureLogcat(serial, pkgFilter string, showSystem bool) (bool, error) {
	a.lcMu.Lock()
	defer a.lcMu.Unlock()
	a.mu.Lock()
	f := a.logcats[serial]
	a.mu.Unlock()
	if f != nil && f.pkg == pkgFilter && f.Alive() {
		f.SetShowSystem(showSystem)
		return false, nil
	}
	return true, a.restartLogcatLocked(serial, pkgFilter, showSystem)
}

// SetLogcatSystem toggles OS-line visibility on the running feed without
// restarting adb, so flipping the switch is instant and loses nothing that is
// already on screen.
func (a *App) SetLogcatSystem(serial string, showSystem bool) {
	// lcMu, not just mu: between a StartLogcat's teardown and its
	// re-registration the map is empty for as long as the device takes to
	// answer, and a toggle landing in that window would be silently dropped
	// while the UI happily showed the new state.
	a.lcMu.Lock()
	defer a.lcMu.Unlock()
	a.mu.Lock()
	f := a.logcats[serial]
	a.mu.Unlock()
	if f != nil {
		f.SetShowSystem(showSystem)
	}
}

func (a *App) StopLogcat(serial string) {
	a.lcMu.Lock()
	defer a.lcMu.Unlock()
	a.stopLogcatFeed(serial)
}

// stopLogcatFeed tears down the feed. Callers must hold lcMu; it drops a.mu
// before Stop() because Stop waits for the batcher goroutine and holding the
// general lock across that would block every other binding that touches it.
func (a *App) stopLogcatFeed(serial string) {
	a.mu.Lock()
	f := a.logcats[serial]
	delete(a.logcats, serial)
	a.mu.Unlock()
	if f != nil {
		f.Stop()
	}
}

func (a *App) ClearLogcat(serial string) (string, error) {
	// Drop what is already in flight as well as what is on the device.
	// Otherwise the lines buffered between logd and the UI arrive a moment
	// later and the "cleared" pane refills with history.
	a.mu.Lock()
	f := a.logcats[serial]
	a.mu.Unlock()
	if f != nil {
		f.Clear()
	}
	return a.client.Shell(a.ctx, serial, "logcat -c")
}

// ─── DNS Live sniffer ───────────────────────────────────────────────────

// DNSSnifferStatus tells the UI what kind of source is currently feeding
// events for this device (or whether nothing is running).
type DNSSnifferStatus struct {
	Running bool   `json:"running"`
	Source  string `json:"source"` // "tcpdump" or "logcat"
}

// StartDNSSniffer begins streaming DNS queries from the device. Events are
// emitted on the "dns:<serial>" Wails event channel; UI subscribes via
// EventsOn. Calling start a second time replaces any prior stream.
func (a *App) StartDNSSniffer(serial string) (*DNSSnifferStatus, error) {
	a.dnsMu.Lock()
	if prev, ok := a.dnsSnif[serial]; ok {
		prev.Stop()
		delete(a.dnsSnif, serial)
	}
	s, err := a.client.StartDNSSniffer(a.ctx, serial)
	if err != nil {
		a.dnsMu.Unlock()
		return nil, err
	}
	a.dnsSnif[serial] = s
	a.dnsMu.Unlock()
	eventName := "dns:" + serial
	go func() {
		for ev := range s.Events() {
			runtime.EventsEmit(a.ctx, eventName, ev)
		}
		runtime.EventsEmit(a.ctx, eventName+":done", nil)
	}()
	return &DNSSnifferStatus{Running: true, Source: s.Source}, nil
}

func (a *App) StopDNSSniffer(serial string) {
	a.dnsMu.Lock()
	defer a.dnsMu.Unlock()
	if s, ok := a.dnsSnif[serial]; ok {
		s.Stop()
		delete(a.dnsSnif, serial)
	}
}

// ─── Processes (live top) ────────────────────────────────────────────────

type ProcStreamStatus struct {
	Running     bool `json:"running"`
	IntervalSec int  `json:"intervalSec"`
}

// StartProcStream begins streaming `top` snapshots from the device. Each
// snapshot is emitted on "procs:<serial>" as a ProcSnapshot.
func (a *App) StartProcStream(serial string, intervalSec int) (*ProcStreamStatus, error) {
	if intervalSec <= 0 {
		intervalSec = 2
	}
	a.procMu.Lock()
	if prev, ok := a.procStreams[serial]; ok {
		prev.Stop()
		delete(a.procStreams, serial)
	}
	s, err := a.client.StartTopStream(a.ctx, serial, intervalSec)
	if err != nil {
		a.procMu.Unlock()
		return nil, err
	}
	a.procStreams[serial] = s
	a.procMu.Unlock()
	eventName := "procs:" + serial
	go func() {
		for snap := range s.Snapshots() {
			runtime.EventsEmit(a.ctx, eventName, snap)
		}
		runtime.EventsEmit(a.ctx, eventName+":done", nil)
	}()
	return &ProcStreamStatus{Running: true, IntervalSec: intervalSec}, nil
}

func (a *App) StopProcStream(serial string) {
	a.procMu.Lock()
	defer a.procMu.Unlock()
	if s, ok := a.procStreams[serial]; ok {
		s.Stop()
		delete(a.procStreams, serial)
	}
}

// ─── Shell sessions ─────────────────────────────────────────────────────

func (a *App) OpenShell(serial string, root bool) (string, error) {
	a.shellMu.Lock()
	a.shellSerial++
	id := fmt.Sprintf("sh-%d", a.shellSerial)
	a.shellMu.Unlock()
	s, err := a.client.StartShell(a.ctx, serial, id, root)
	if err != nil {
		return "", err
	}
	// Persist scrollback so it survives an app restart.
	label := "shell"
	if root {
		label = "root"
	}
	scrollbackLabel := serial + "_" + label + "_" + id
	if w, err := adb.OpenScrollbackWriter(serial, scrollbackLabel); err == nil {
		// Banner so reload knows the session boundary.
		_, _ = fmt.Fprintf(w, "\r\n\x1b[90m# >>> %s shell on %s opened at %s\x1b[0m\r\n",
			label, serial, time.Now().Format("2006-01-02 15:04:05"))
		s.SetTee(w)
	}
	a.shellMu.Lock()
	a.shells[id] = s
	a.shellMu.Unlock()
	eventName := "shell:" + id
	go func() {
		for chunk := range s.Output() {
			runtime.EventsEmit(a.ctx, eventName, string(chunk))
		}
		runtime.EventsEmit(a.ctx, eventName+":done", nil)
	}()
	return id, nil
}

// ListShellHistory returns persisted scrollback entries from previous adbq
// runs, newest first. The frontend can replay these into a fresh terminal.
func (a *App) ListShellHistory() ([]adb.ScrollbackEntry, error) {
	return adb.ListScrollbacks()
}

// ReadShellHistory returns the saved log content for a given session label.
func (a *App) ReadShellHistory(serial, label string) (string, error) {
	return adb.ReadScrollback(serial, label)
}

// ClearShellHistory deletes the on-disk log for a session label.
func (a *App) ClearShellHistory(serial, label string) error {
	return adb.ClearScrollback(serial, label)
}

// ResizeShell forwards a window-size change from the xterm.js frontend to the
// host PTY so the device-side shell redraws correctly (vi, top, etc.).
func (a *App) ResizeShell(id string, cols, rows int) error {
	a.shellMu.Lock()
	s := a.shells[id]
	a.shellMu.Unlock()
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	if cols <= 0 || cols > 1000 {
		cols = 100
	}
	if rows <= 0 || rows > 500 {
		rows = 30
	}
	return s.Resize(uint16(cols), uint16(rows))
}

func (a *App) WriteShell(id, data string) error {
	a.shellMu.Lock()
	s := a.shells[id]
	a.shellMu.Unlock()
	if s == nil {
		return fmt.Errorf("session %s not found", id)
	}
	return s.Write(data)
}

func (a *App) CloseShell(id string) {
	a.shellMu.Lock()
	s := a.shells[id]
	delete(a.shells, id)
	a.shellMu.Unlock()
	if s != nil {
		s.Stop()
	}
}

func (a *App) RunCommand(serial, cmd string) (string, error) {
	return a.client.Shell(a.ctx, serial, cmd)
}

func (a *App) RunCommandRoot(serial, cmd string) (string, error) {
	out, _, err := a.client.ShellSU(a.ctx, serial, cmd)
	return out, err
}

// ─── Apps ────────────────────────────────────────────────────────────────

func (a *App) ListApps(serial string, onlyUser bool) ([]adb.App, error) {
	return a.client.ListApps(a.ctx, serial, onlyUser)
}
func (a *App) DescribeApp(serial, pkg string) (*adb.AppDetail, error) {
	return a.client.DescribeApp(a.ctx, serial, pkg)
}

// AndroidVersionMap exposes the SDK-level → "Android X (Codename)" table so
// the frontend can label minSdk/targetSdk/compileSdk without a round-trip
// for every package.
func (a *App) AndroidVersionMap() map[int]string {
	return adb.AndroidVersionMap()
}

// IsAppRunning is the Apps detail panel's "is this thing alive" probe used
// to swap Launch ↔ Kill. Cheap enough to poll once every few seconds.
func (a *App) IsAppRunning(serial, pkg string) (*adb.AppRunning, error) {
	return a.client.IsAppRunning(a.ctx, serial, pkg)
}
func (a *App) UninstallApp(serial, pkg string) (string, error) {
	id, _ := a.tasks.Create("uninstall", "Uninstalling "+pkg, pkg)
	go func() {
		out, err := a.client.UninstallApp(a.ctx, serial, pkg)
		if err != nil {
			a.tasks.Finish(id, "err", out, err.Error())
			return
		}
		a.tasks.Finish(id, "ok", out, "")
	}()
	return id, nil
}

// PushFileWithOptions pushes a local file and optionally chmod/chown's the
// remote target. owner is "user[:group]" or empty; mode is chmod-compat or empty.
func (a *App) PushFileWithOptions(serial, remoteDir, mode, owner string, asRoot bool) (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select file to push"})
	if err != nil || path == "" {
		return "", err
	}
	id, _ := a.tasks.Create("push", "Pushing "+filepath.Base(path), "→ "+remoteDir)
	go func() {
		out, err := a.client.PushFile(a.ctx, serial, path, remoteDir)
		if err != nil {
			a.tasks.Finish(id, "err", out, err.Error())
			return
		}
		remoteName := remoteDir
		if remoteDir[len(remoteDir)-1] != '/' {
			remoteName += "/"
		}
		remoteName += filepath.Base(path)
		if mode != "" {
			if _, e := a.client.Chmod(a.ctx, serial, remoteName, mode, asRoot); e != nil {
				a.tasks.Finish(id, "err", out, "chmod: "+e.Error())
				return
			}
		}
		if owner != "" {
			if _, e := a.client.Chown(a.ctx, serial, remoteName, owner, asRoot); e != nil {
				a.tasks.Finish(id, "err", out, "chown: "+e.Error())
				return
			}
		}
		a.tasks.Finish(id, "ok", remoteName, "")
	}()
	return id, nil
}
func (a *App) ForceStopApp(serial, pkg string) (string, error) {
	return a.client.ForceStopApp(a.ctx, serial, pkg)
}
func (a *App) ClearApp(serial, pkg string) (string, error) {
	return a.client.ClearApp(a.ctx, serial, pkg)
}
func (a *App) LaunchApp(serial, pkg string) (string, error) {
	return a.client.LaunchApp(a.ctx, serial, pkg)
}
func (a *App) InstallAPKFromPath(serial, localPath string) (string, error) {
	return a.client.InstallAPK(a.ctx, serial, localPath)
}

func (a *App) PickAndInstallAPK(serial string) (string, error) {
	path, err := a.PickApkFile()
	if err != nil || path == "" {
		return "", err
	}
	return a.InstallApkBundleFromPath(serial, path)
}

// PickApkFile opens the host file picker and returns the chosen path without
// installing anything, so the UI can show the install plan and its adb command
// before the user commits.
func (a *App) PickApkFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select APK or APKS to install",
		Filters: []runtime.FileFilter{
			{DisplayName: "Android packages", Pattern: "*.apk;*.apks;*.xapk;*.zip"},
		},
	})
}

// PlanApkInstall reports which APKs inside a file apply to this device and the
// exact adb command the install will run. Read-only.
func (a *App) PlanApkInstall(serial, localPath string) (*adb.ApkInstallPlan, error) {
	return a.client.PlanApkInstall(a.ctx, serial, localPath)
}

// InstallApkBundleFromPath installs a single APK or a multi-APK container
// (.apks/.xapk/.zip) as one pm session.
func (a *App) InstallApkBundleFromPath(serial, localPath string) (string, error) {
	if localPath == "" {
		return "", fmt.Errorf("no file selected")
	}
	id, _ := a.tasks.Create("install", "Installing "+filepath.Base(localPath), localPath)
	go func() {
		out, err := a.client.InstallApkBundle(a.ctx, serial, localPath, func(s string) {
			a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = s })
		})
		if err != nil {
			a.tasks.Finish(id, "err", out, err.Error())
			return
		}
		a.tasks.Finish(id, "ok", out, "")
	}()
	return id, nil
}

// ApkSetOf reports the package's APK layout (base + splits) and the commands
// an export would run.
func (a *App) ApkSetOf(serial, pkg string) (*adb.ApkSet, error) {
	return a.client.ApkSetOf(a.ctx, serial, pkg)
}

// ExportApks packs every APK of a split install into one .apks archive that
// adbq (and SAI/bundletool-style installers) can install again later.
func (a *App) ExportApks(serial, pkg string) (string, error) {
	set, err := a.client.ApkSetOf(a.ctx, serial, pkg)
	if err != nil {
		return "", err
	}
	title, filter := "Save APK as…", runtime.FileFilter{DisplayName: "Android package (*.apk)", Pattern: "*.apk"}
	if set.Split {
		title = "Save APKS as…"
		filter = runtime.FileFilter{DisplayName: "Android app bundle (*.apks)", Pattern: "*.apks"}
	}
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: set.Suggested,
		Filters:         []runtime.FileFilter{filter},
	})
	if err != nil || dst == "" {
		return "", err
	}
	// The dialog returns the typed name verbatim on every platform, so a split
	// export can come back as `.apk` — a name no installer would read as an
	// archive. Correct it rather than write a mislabelled file.
	dst = adb.EnsureExportExt(dst, set.Split)
	id, _ := a.tasks.Create("export-apk", "Exporting "+pkg, fmt.Sprintf("%d APK(s) → %s", len(set.Splits)+1, dst))
	go func() {
		out, err := a.client.ExportApks(a.ctx, serial, pkg, dst, func(s string) {
			a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = s })
		})
		if err != nil {
			a.tasks.Finish(id, "err", "", err.Error())
			return
		}
		a.tasks.Finish(id, "ok", out, "")
	}()
	return id, nil
}

func (a *App) ExportAPK(serial, pkg string) (string, error) {
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save APK as…",
		DefaultFilename: pkg + ".apk",
	})
	if err != nil || dst == "" {
		return "", err
	}
	id, _ := a.tasks.Create("export-apk", "Exporting "+pkg, "→ "+dst)
	go func() {
		out, err := a.client.PullAPK(a.ctx, serial, pkg, dst)
		if err != nil {
			a.tasks.Finish(id, "err", out, err.Error())
			return
		}
		a.tasks.Finish(id, "ok", dst, "")
	}()
	return id, nil
}

// ─── Files ───────────────────────────────────────────────────────────────

func (a *App) ListDir(serial, path string, asRoot bool) ([]adb.FileEntry, error) {
	return a.client.ListDir(a.ctx, serial, path, asRoot)
}
func (a *App) DeleteFile(serial, path string, recursive, asRoot bool) (string, error) {
	return a.client.RemoveFile(a.ctx, serial, path, recursive, asRoot)
}
func (a *App) Mkdir(serial, path string, asRoot bool) (string, error) {
	return a.client.Mkdir(a.ctx, serial, path, asRoot)
}

func (a *App) PushFileWithPicker(serial, remoteDir string) (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select file to push"})
	if err != nil || path == "" {
		return "", err
	}
	return a.client.PushFile(a.ctx, serial, path, remoteDir)
}

func (a *App) PullFileWithPicker(serial, remotePath string) (string, error) {
	base := remotePath
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			base = base[i+1:]
			break
		}
	}
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Pull to…",
		DefaultFilename: base,
	})
	if err != nil || dst == "" {
		return "", err
	}
	return a.client.PullFile(a.ctx, serial, remotePath, dst)
}

// ─── Forwards ────────────────────────────────────────────────────────────

func (a *App) ListForwards(serial string) ([]adb.Forward, error) {
	return a.client.ListForwards(a.ctx, serial)
}
func (a *App) ListReverses(serial string) ([]adb.Forward, error) {
	return a.client.ListReverses(a.ctx, serial)
}
func (a *App) AddForward(serial, local, remote string) (string, error) {
	return a.client.AddForward(a.ctx, serial, local, remote)
}
func (a *App) AddReverse(serial, remote, local string) (string, error) {
	return a.client.AddReverse(a.ctx, serial, remote, local)
}
func (a *App) RemoveForward(serial, local string) (string, error) {
	return a.client.RemoveForward(a.ctx, serial, local)
}
func (a *App) RemoveReverse(serial, remote string) (string, error) {
	return a.client.RemoveReverse(a.ctx, serial, remote)
}

// ─── Frida ───────────────────────────────────────────────────────────────

func (a *App) ListFridaServers(serial string) ([]adb.FridaServer, error) {
	return a.client.ListFridaServers(a.ctx, serial)
}

// FridaArchInfo reports the device ABIs and which frida-server arches it can
// run, so the UI can show the detected target and offer overrides.
func (a *App) FridaArchInfo(serial string) (*adb.FridaArchInfo, error) {
	return a.client.FridaArchInfo(a.ctx, serial)
}

// ListFridaReleases returns the official frida-server versions installable on
// the device for the given arch (empty = auto-detect the primary ABI), each
// flagged if already on-device. Used by the one-click installer.
func (a *App) ListFridaReleases(serial, arch string) ([]adb.FridaRelease, error) {
	return a.client.ListFridaReleases(a.ctx, serial, arch)
}

// InstallFridaServer downloads (with SHA256 verification against GitHub's
// published digest), decompresses with a host tool, and pushes the chosen
// frida-server version for the given arch (empty = auto-detect). Progress is
// surfaced through the task tray.
func (a *App) InstallFridaServer(serial, version, arch string) (string, error) {
	id, _ := a.tasks.Create("frida-install", "Install frida-server "+version, serial)
	remote, err := a.client.InstallFridaServer(a.ctx, serial, version, arch, func(stage string) {
		a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = stage + " · " + serial })
	})
	if err != nil {
		a.tasks.Finish(id, "err", "", err.Error())
		return "", err
	}
	a.tasks.Finish(id, "ok", remote, "installed "+remote)
	return remote, nil
}
func (a *App) StartFrida(serial, path, iface string, port int) (string, error) {
	out, err := a.client.StartFrida(a.ctx, serial, path, iface, port)
	if err == nil && a.sessions != nil {
		a.sessions.Put(adb.Session{
			ID: "frida:" + serial, Kind: "frida", Serial: serial,
			RemoteFile: path, Detail: fmt.Sprintf("%s:%d", iface, port),
		})
	}
	return out, err
}

// FridaServerLog returns what the frida-server on the given port printed on its
// last launch. Empty output means it started cleanly — the server only writes
// here when something went wrong. Logs are per-port because a device can run
// several servers at once. Pass 0 for frida's default port.
func (a *App) FridaServerLog(serial string, port int) (string, error) {
	return a.client.FridaServerLog(a.ctx, serial, port)
}
func (a *App) ListPackageUIDs(serial string) (map[int]string, error) {
	return a.client.ListPackageUIDs(a.ctx, serial)
}
func (a *App) ChmodFile(serial, path, mode string, asRoot bool) (string, error) {
	return a.client.Chmod(a.ctx, serial, path, mode, asRoot)
}
func (a *App) ChownFile(serial, path, owner string, asRoot bool) (string, error) {
	return a.client.Chown(a.ctx, serial, path, owner, asRoot)
}
func (a *App) MoveFile(serial, src, dst string, asRoot bool) (string, error) {
	return a.client.MoveFileOnDevice(a.ctx, serial, src, dst, asRoot)
}
func (a *App) TcpipMode(serial string, port int) (string, error) {
	return a.client.TcpipMode(a.ctx, serial, port)
}
func (a *App) ScreenRecord(serial string, seconds int) (string, error) {
	return a.client.ScreenRecord(a.ctx, serial, "", seconds)
}

// RevealPath opens the host file manager focused on the given path
// (Finder on macOS, Explorer on Windows, default xdg-open on Linux).
func (a *App) RevealPath(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", "/select,", path)
	default:
		// xdg-open can't select; open the directory
		dir := path
		for i := len(dir) - 1; i >= 0; i-- {
			if dir[i] == '/' {
				dir = dir[:i]
				break
			}
		}
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}

// OpenPath launches the default host app for a path (Preview for PNGs, etc.).
func (a *App) OpenPath(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
func (a *App) StopFrida(serial string) (string, error) {
	out, err := a.client.StopFrida(a.ctx, serial)
	if a.sessions != nil {
		a.sessions.Remove("frida:" + serial)
	}
	return out, err
}

// ─── Frida host runtime (venvs + external interpreters) ────────────────────

// FridaHost reports whether a usable host Python is present, for the Runtime UI.
func (a *App) FridaHost() adb.FridaHostInfo {
	return adb.DetectFridaHost()
}

// ListFridaRuntimes returns every host runtime able to drive Frida — managed
// venvs and registered external interpreters — each tagged with its frida version.
func (a *App) ListFridaRuntimes() []adb.FridaRuntime {
	if a.frida == nil {
		return nil
	}
	return a.frida.ListRuntimes()
}

// DetectRunningFridaVersion asks the device's live frida-server for its version
// (authoritative), so the Runtime UI can offer a matching venv.
func (a *App) DetectRunningFridaVersion(serial string) (string, error) {
	return a.client.DetectRunningFridaVersion(a.ctx, serial)
}

// EnsureFridaVenv provisions (or reuses) a managed venv with frida pinned to the
// given version: it downloads the single host-matching wheel from PyPI, verifies
// its SHA256, and installs it offline (pip --no-index --no-deps --only-binary).
// Progress is streamed via the "frida-venv:progress" event.
func (a *App) EnsureFridaVenv(version string) (adb.FridaRuntime, error) {
	if a.frida == nil {
		return adb.FridaRuntime{}, fmt.Errorf("frida store unavailable")
	}
	return a.frida.EnsureVenv(a.ctx, version, func(stage string) {
		runtime.EventsEmit(a.ctx, "frida-venv:progress", map[string]string{"version": version, "stage": stage})
	})
}

// RegisterExternalFrida records a user-provided interpreter/venv path that
// already has frida installed (bring-your-own). adbq installs nothing; it only
// reads the frida + Python versions from the path.
func (a *App) RegisterExternalFrida(path string) (adb.FridaRuntime, error) {
	if a.frida == nil {
		return adb.FridaRuntime{}, fmt.Errorf("frida store unavailable")
	}
	return a.frida.RegisterExternal(path)
}

// PickExternalFridaInterpreter opens a file picker for a Python interpreter and
// registers it as an external runtime (empty result = user cancelled).
func (a *App) PickExternalFridaInterpreter() (adb.FridaRuntime, error) {
	if a.frida == nil {
		return adb.FridaRuntime{}, fmt.Errorf("frida store unavailable")
	}
	sel, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a Python interpreter with frida installed (its venv's bin/python)",
	})
	if err != nil {
		return adb.FridaRuntime{}, err
	}
	if strings.TrimSpace(sel) == "" {
		return adb.FridaRuntime{}, nil
	}
	return a.frida.RegisterExternal(sel)
}

// RemoveFridaRuntime deletes a managed venv or forgets an external interpreter.
func (a *App) RemoveFridaRuntime(id string) error {
	if a.frida == nil {
		return fmt.Errorf("frida store unavailable")
	}
	return a.frida.RemoveRuntime(id)
}

// FridaManagedEnabled reports whether adbq may auto-create managed venvs.
func (a *App) FridaManagedEnabled() bool {
	return a.frida != nil && a.frida.ManagedEnabled()
}

// SetFridaManagedEnabled toggles auto-managed venv installs (off = pure BYO).
func (a *App) SetFridaManagedEnabled(v bool) error {
	if a.frida == nil {
		return fmt.Errorf("frida store unavailable")
	}
	return a.frida.SetManagedEnabled(v)
}

// ─── Frida script library + per-app bindings ───────────────────────────────

// ListFridaScripts returns script metadata (no source), newest-updated first.
func (a *App) ListFridaScripts() []adb.FridaScript {
	if a.frida == nil {
		return nil
	}
	return a.frida.ListScripts()
}

// GetFridaScript returns one script including its source body.
func (a *App) GetFridaScript(id string) (adb.FridaScript, error) {
	if a.frida == nil {
		return adb.FridaScript{}, fmt.Errorf("frida store unavailable")
	}
	return a.frida.GetScript(id)
}

// SaveFridaScript creates (empty id) or updates a script and returns the saved entry.
func (a *App) SaveFridaScript(s adb.FridaScript) (adb.FridaScript, error) {
	if a.frida == nil {
		return adb.FridaScript{}, fmt.Errorf("frida store unavailable")
	}
	return a.frida.SaveScript(s)
}

// DeleteFridaScript removes a script and detaches it from every app binding.
func (a *App) DeleteFridaScript(id string) error {
	if a.frida == nil {
		return fmt.Errorf("frida store unavailable")
	}
	return a.frida.DeleteScript(id)
}

// GetAppFridaScripts returns the scripts bound to a package (device-independent).
func (a *App) GetAppFridaScripts(pkg string) adb.AppScripts {
	if a.frida == nil {
		return adb.AppScripts{Package: pkg, Mode: "spawn", ScriptIDs: []string{}}
	}
	return a.frida.GetAppScripts(pkg)
}

// SetAppFridaScripts replaces a package's script binding (mode: spawn|attach).
func (a *App) SetAppFridaScripts(pkg string, scriptIDs []string, mode, venvVer string) error {
	if a.frida == nil {
		return fmt.Errorf("frida store unavailable")
	}
	return a.frida.SetAppScripts(pkg, scriptIDs, mode, venvVer)
}

// ListAppFridaScripts returns every package→scripts binding for the App Scripts view.
func (a *App) ListAppFridaScripts() []adb.AppScripts {
	if a.frida == nil {
		return nil
	}
	return a.frida.ListAppScripts()
}

// ─── Frida CodeShare ───────────────────────────────────────────────────────

// SearchCodeshare returns CodeShare discovery results for a query (empty =
// browse the popular listing). Results carry owner/slug for a follow-up fetch.
func (a *App) SearchCodeshare(query string) ([]adb.CodeshareProject, error) {
	return adb.CodeshareSearch(a.ctx, query)
}

// BrowseCodeshare returns one page of the CodeShare popular/browse listing.
func (a *App) BrowseCodeshare(page int) ([]adb.CodeshareProject, error) {
	return adb.CodeshareBrowse(a.ctx, page)
}

// GetCodeshareScript fetches a CodeShare project's metadata + source (for the
// review-before-import preview). The source is untrusted until the user saves it.
func (a *App) GetCodeshareScript(owner, slug string) (*adb.CodeshareScript, error) {
	return adb.CodeshareGetProject(a.ctx, owner, slug)
}

// ImportCodeshareScript fetches a CodeShare project and saves it into the library
// as an untrusted script (updating in place if already imported).
func (a *App) ImportCodeshareScript(owner, slug string) (adb.FridaScript, error) {
	if a.frida == nil {
		return adb.FridaScript{}, fmt.Errorf("frida store unavailable")
	}
	cs, err := adb.CodeshareGetProject(a.ctx, owner, slug)
	if err != nil {
		return adb.FridaScript{}, err
	}
	return a.frida.ImportCodeshare(cs)
}

// ─── Frida live sessions ───────────────────────────────────────────────────

// fridaFlushInterval is how often a live session's new messages are batched out
// to the UI. Fast enough to feel live, slow enough that a noisy script can't
// turn every log line into its own event + React render.
const fridaFlushInterval = 100 * time.Millisecond

// StartFridaSession launches a host-side frida driver under the runtime matching
// runtimeVer, instrumenting pkg with the given library scripts. mode is "spawn"
// (cold-start) or "attach". port is the device port the frida-server listens on
// (0 = frida's default); a non-default one is reached through an adb forward.
// Messages stream via the "frida-session:<id>" event; call GetFridaSessionLog
// right after subscribing to backfill the start race.
func (a *App) StartFridaSession(serial, pkg, mode, runtimeVer string, port int, scriptIDs []string) (adb.FridaSessionInfo, error) {
	if a.frida == nil {
		return adb.FridaSessionInfo{}, fmt.Errorf("frida store unavailable")
	}
	rt, kind := a.frida.ResolveForVersion(runtimeVer)
	if kind == "none" {
		return adb.FridaSessionInfo{}, fmt.Errorf("no host runtime for frida %s — create a venv or register an interpreter in the Runtime tab", runtimeVer)
	}
	scripts, err := a.collectScripts(scriptIDs)
	if err != nil {
		return adb.FridaSessionInfo{}, err
	}

	a.fridaMu.Lock()
	a.fridaSeq++
	id := fmt.Sprintf("f-%d", a.fridaSeq)
	a.fridaMu.Unlock()

	sess, err := adb.StartFridaSession(a.ctx, a.client, rt, id, serial, pkg, mode, port, scripts)
	if err != nil {
		return adb.FridaSessionInfo{}, err
	}
	a.fridaMu.Lock()
	a.fridaSess[id] = sess
	a.fridaMu.Unlock()

	// Record this launch in the recents list so it can be repeated from the
	// Frida → Sessions tab without going back to Apps.
	names := make([]string, 0, len(scripts))
	for _, sc := range scripts {
		names = append(names, sc.Name)
	}
	a.frida.RecordHistory(adb.FridaHistoryEntry{
		Package: pkg, Mode: mode, RuntimeVer: runtimeVer,
		ScriptIDs: scriptIDs, ScriptNames: names,
	})

	eventName := "frida-session:" + id
	go func() {
		// Drain the session ring on a timer rather than emitting one event per
		// message: a hook that logs on every call outruns the event bridge, and
		// batching keeps the UI at one state update per tick instead of thousands.
		last := 0
		flush := func() {
			msgs := sess.LogSince(last)
			if len(msgs) == 0 {
				return
			}
			last = msgs[len(msgs)-1].Seq
			runtime.EventsEmit(a.ctx, eventName, msgs)
		}
		tick := time.NewTicker(fridaFlushInterval)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				flush()
			case <-sess.Done():
				flush()
				// Final state (ended/error) is reflected via the info snapshot the
				// UI re-reads; signal completion so it can stop showing "running".
				runtime.EventsEmit(a.ctx, eventName+":done", sess.Info())
				return
			}
		}
	}()
	return sess.Info(), nil
}

// collectScripts resolves library script IDs to name+source pairs for the driver.
func (a *App) collectScripts(ids []string) ([]adb.FridaScriptArg, error) {
	var out []adb.FridaScriptArg
	for _, id := range ids {
		sc, err := a.frida.GetScript(id)
		if err != nil {
			return nil, fmt.Errorf("script %s: %w", id, err)
		}
		out = append(out, adb.FridaScriptArg{Name: sc.Name, Source: sc.Source})
	}
	return out, nil
}

// ListFridaHistory returns the recents list (previously instrumented apps,
// newest first) for the Frida → Sessions tab's one-click repeat.
func (a *App) ListFridaHistory() []adb.FridaHistoryEntry {
	if a.frida == nil {
		return nil
	}
	return a.frida.ListHistory()
}

// RemoveFridaHistory drops one app from the recents list.
func (a *App) RemoveFridaHistory(pkg string) error {
	if a.frida == nil {
		return nil
	}
	return a.frida.RemoveHistory(pkg)
}

// ClearFridaHistory empties the recents list.
func (a *App) ClearFridaHistory() error {
	if a.frida == nil {
		return nil
	}
	return a.frida.ClearHistory()
}

// ListFridaSessions returns metadata for all live/finished sessions this run.
func (a *App) ListFridaSessions() []adb.FridaSessionInfo {
	a.fridaMu.Lock()
	defer a.fridaMu.Unlock()
	out := make([]adb.FridaSessionInfo, 0, len(a.fridaSess))
	for _, s := range a.fridaSess {
		out = append(out, s.Info())
	}
	return out
}

// GetFridaSessionLog returns buffered messages with seq > sinceSeq, so the UI can
// backfill what it missed before (or between) subscriptions and de-dupe by seq.
func (a *App) GetFridaSessionLog(id string, sinceSeq int) []adb.FridaMsg {
	a.fridaMu.Lock()
	s := a.fridaSess[id]
	a.fridaMu.Unlock()
	if s == nil {
		return nil
	}
	return s.LogSince(sinceSeq)
}

// StopFridaSession detaches and terminates a session's driver.
func (a *App) StopFridaSession(id string) {
	a.fridaMu.Lock()
	s := a.fridaSess[id]
	a.fridaMu.Unlock()
	if s != nil {
		s.Stop()
	}
}

// RemoveFridaSession stops a session and drops it from the list.
func (a *App) RemoveFridaSession(id string) {
	a.fridaMu.Lock()
	s := a.fridaSess[id]
	delete(a.fridaSess, id)
	a.fridaMu.Unlock()
	if s != nil {
		s.Stop()
	}
}

// StartAppWithFrida is the one-click orchestration behind "Start/Attach with
// Frida" in the Apps screen. It uses the package's saved script binding, ensures
// a frida-server is running on the device (auto-starting an installed binary when
// there's an unambiguous choice), detects that server's exact version, resolves a
// matching host runtime (auto-building a managed venv when allowed), and launches
// the session. Stages are surfaced via the "frida-start:progress" event; failures
// are actionable (pointing at the Server or Runtime tab) rather than opaque.
func (a *App) StartAppWithFrida(serial, pkg, mode string) (adb.FridaSessionInfo, error) {
	if a.frida == nil {
		return adb.FridaSessionInfo{}, fmt.Errorf("frida store unavailable")
	}
	progress := func(stage string) {
		runtime.EventsEmit(a.ctx, "frida-start:progress", map[string]string{"package": pkg, "stage": stage})
	}
	binding := a.frida.GetAppScripts(pkg)
	if mode == "" {
		mode = binding.Mode
	}

	progress("ensuring frida-server")
	ver, port, err := a.ensureDeviceFridaServer(serial, progress)
	if err != nil {
		return adb.FridaSessionInfo{}, err
	}

	progress("preparing host runtime frida " + ver)
	if _, kind := a.frida.ResolveForVersion(ver); kind == "none" {
		if !a.frida.ManagedEnabled() {
			return adb.FridaSessionInfo{}, fmt.Errorf("no host runtime for frida %s — add an interpreter in Frida → Runtime, or enable managed installs", ver)
		}
		if _, err := a.frida.EnsureVenv(a.ctx, ver, func(stage string) {
			runtime.EventsEmit(a.ctx, "frida-venv:progress", map[string]string{"version": ver, "stage": stage})
		}); err != nil {
			return adb.FridaSessionInfo{}, fmt.Errorf("prepare host frida %s: %w", ver, err)
		}
	}

	progress("launching")
	return a.StartFridaSession(serial, pkg, mode, ver, port, binding.ScriptIDs)
}

// ensureDeviceFridaServer returns the running frida-server's version and the
// port it listens on, starting an installed binary first when none is running.
func (a *App) ensureDeviceFridaServer(serial string, progress func(string)) (string, int, error) {
	servers, err := a.client.ListFridaServers(a.ctx, serial)
	if err != nil {
		return "", 0, fmt.Errorf("check frida-server: %w", err)
	}
	if ver, port, ok := a.runningFridaServer(serial, servers); ok {
		return ver, port, nil
	}

	pick := a.pickFridaServer(serial, servers)
	if pick == nil {
		if len(servers) == 0 {
			return "", 0, fmt.Errorf("no frida-server on the device — install one in Frida → Server first")
		}
		return "", 0, fmt.Errorf("none of the %d frida-server binaries on the device can run here — install one for this device's architecture in Frida → Server", len(servers))
	}
	progress("starting frida-server " + pick.Version)
	if _, err := a.client.StartFrida(a.ctx, serial, pick.Path, "", 0); err != nil {
		return "", 0, fmt.Errorf("start frida-server: %w", err)
	}
	// Give it a moment, then read back what actually came up.
	for range 10 {
		time.Sleep(300 * time.Millisecond)
		fresh, err := a.client.ListFridaServers(a.ctx, serial)
		if err != nil {
			continue
		}
		if ver, port, ok := a.runningFridaServer(serial, fresh); ok {
			return ver, port, nil
		}
	}
	return "", 0, fmt.Errorf("frida-server did not come up — check its log in Frida → Server")
}

// runningFridaServer reports the version and port of the server already running,
// if there is one.
func (a *App) runningFridaServer(serial string, servers []adb.FridaServer) (string, int, bool) {
	for _, s := range servers {
		if !s.Active {
			continue
		}
		if s.Version != "" {
			return s.Version, s.Port, true
		}
		// Running, but the filename says nothing about the version — ask the
		// binary itself, which is authoritative anyway.
		if ver, err := a.client.DetectRunningFridaVersion(a.ctx, serial); err == nil && ver != "" {
			return ver, s.Port, true
		}
	}
	// A server started from a path we don't inventory still answers this, and it
	// will be on frida's default port or nothing would reach it.
	if ver, err := a.client.DetectRunningFridaVersion(a.ctx, serial); err == nil && ver != "" {
		return ver, adb.FridaDefaultPort, true
	}
	return "", 0, false
}

// pickFridaServer gathers what the choice depends on and defers to
// chooseFridaServer.
func (a *App) pickFridaServer(serial string, servers []adb.FridaServer) *adb.FridaServer {
	arches := map[string]bool{}
	if info, err := a.client.FridaArchInfo(a.ctx, serial); err == nil && info != nil {
		for _, x := range info.Supported {
			arches[x] = true
		}
	}
	lastUsed := map[string]bool{}
	hasRuntime := func(string) bool { return false }
	if a.frida != nil {
		for _, h := range a.frida.ListHistory() {
			if h.RuntimeVer != "" {
				lastUsed[h.RuntimeVer] = true
			}
		}
		hasRuntime = func(ver string) bool {
			_, kind := a.frida.ResolveForVersion(ver)
			return kind != "none"
		}
	}
	return chooseFridaServer(servers, arches, hasRuntime, lastUsed)
}

// chooseFridaServer decides which installed binary the one-click flow should
// launch. Having several installed is normal — a pentest device accumulates them
// — and refusing to choose meant the flow could never start a server on any
// well-used device; it just said "multiple frida-server binaries" and stopped.
//
// Preference, strongest first: an architecture this device can actually execute,
// then a version the host already has a matching frida for (so no venv has to be
// built), then one previously used here, then the newest. arches may be empty
// when the probe told us nothing, in which case arch is not held against any
// candidate.
func chooseFridaServer(
	servers []adb.FridaServer,
	arches map[string]bool,
	hasRuntime func(string) bool,
	lastUsed map[string]bool,
) *adb.FridaServer {
	score := func(s adb.FridaServer) int {
		// An arch the device cannot execute is not a candidate at all.
		if len(arches) > 0 && s.Arch != "" && !arches[s.Arch] {
			return -1
		}
		n := 0
		if s.Arch != "" && arches[s.Arch] {
			n += 4
		}
		if hasRuntime != nil && hasRuntime(s.Version) {
			n += 2
		}
		if lastUsed[s.Version] {
			n++
		}
		return n
	}

	var best *adb.FridaServer
	bestScore := -1
	for i := range servers {
		s := servers[i]
		if !s.Runnable || s.Version == "" {
			continue
		}
		sc := score(s)
		if sc < 0 {
			continue
		}
		if best == nil || sc > bestScore ||
			(sc == bestScore && adb.CompareFridaVersions(s.Version, best.Version) > 0) {
			best, bestScore = &servers[i], sc
		}
	}
	return best
}

// ─── Network ─────────────────────────────────────────────────────────────

func (a *App) GetNetworkInfo(serial string) (*adb.NetworkInfo, error) {
	return a.client.GetNetworkInfo(a.ctx, serial)
}
func (a *App) GetProxy(serial string) (string, error) { return a.client.GetProxy(a.ctx, serial) }
func (a *App) SetProxy(serial, hostPort string) (string, error) {
	return a.client.SetProxy(a.ctx, serial, hostPort)
}

// ─── System ──────────────────────────────────────────────────────────────

func (a *App) Reboot(serial, mode string) (string, error) {
	return a.client.Reboot(a.ctx, serial, mode)
}
func (a *App) ListConnections(serial string) ([]adb.Connection, error) {
	return a.client.ListConnections(a.ctx, serial)
}
func (a *App) ClipboardSet(serial, text string) (string, error) {
	return a.client.ClipboardSet(a.ctx, serial, text)
}

// InstallSystemCertWithPicker prompts for a CA certificate file and installs it
// into the device trust store (system store on rooted devices, with a guided
// user-store fallback otherwise). Used by Network → Cert.
func (a *App) InstallSystemCertWithPicker(serial string) (*adb.CertInstallResult, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select CA certificate (.der / .pem / .crt)",
		Filters: []runtime.FileFilter{
			{DisplayName: "Certificates (*.der;*.pem;*.crt;*.cer)", Pattern: "*.der;*.pem;*.crt;*.cer"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	id, _ := a.tasks.Create("cert-install", "Install CA certificate", serial)
	res, err := a.client.InstallSystemCert(a.ctx, serial, path)
	if err != nil {
		a.tasks.Finish(id, "err", "", err.Error())
		return nil, err
	}
	a.tasks.Finish(id, "ok", res.Path, res.Strategy+" · "+res.Subject)
	return res, nil
}

// ListCACerts returns the certificates in the device CA trust store for the
// Network → Cert viewer.
func (a *App) ListCACerts(serial string) ([]adb.CACert, error) {
	return a.client.ListCACerts(a.ctx, serial)
}

// PushFridaBinaryWithPicker prompts for a host file and pushes it.
func (a *App) PushFridaBinaryWithPicker(serial string) (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select frida-server binary"})
	if err != nil || path == "" {
		return "", err
	}
	return a.client.PushFridaBinary(a.ctx, serial, path)
}

// ─── Recording (Start/Stop with live status events) ─────────────────────

// StartScreenRecord begins recording and emits task:update events. Returns the
// task ID so the UI can subscribe.
func (a *App) StartScreenRecord(serial string, maxSec int) (string, error) {
	a.srMu.Lock()
	if _, exists := a.srSess[serial]; exists {
		a.srMu.Unlock()
		return "", fmt.Errorf("recording already in progress")
	}
	a.srMu.Unlock()

	id, ctx := a.tasks.Create("screen-record", "Recording screen", serial)
	sess, err := a.client.StartScreenRecord(ctx, serial, maxSec)
	if err != nil {
		a.tasks.Finish(id, "err", "", err.Error())
		return "", err
	}
	a.srMu.Lock()
	a.srSess[serial] = &srEntry{sess: sess, taskID: id}
	a.srMu.Unlock()
	a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = "recording → " + sess.RemotePath })
	if a.sessions != nil {
		a.sessions.Put(adb.Session{
			ID: "rec:" + serial, Kind: "screen-record", Serial: serial,
			RemoteFile: sess.RemotePath,
		})
	}

	// Watch the elapsed time so the UI can show a counter.
	go func() {
		start := time.Now()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-sess.Done:
				return
			case <-tick.C:
				elapsed := int(time.Since(start).Seconds())
				a.tasks.Update(id, func(t *adb.TaskState) {
					t.Detail = fmt.Sprintf("recording · %ds elapsed", elapsed)
				})
			}
		}
	}()
	return id, nil
}

// StopScreenRecord finalizes the active recording, pulls the mp4, and updates
// the matching task to completed.
func (a *App) StopScreenRecord(serial string) (string, error) {
	a.srMu.Lock()
	entry := a.srSess[serial]
	delete(a.srSess, serial)
	a.srMu.Unlock()
	if entry == nil {
		return "", fmt.Errorf("no active recording")
	}
	taskID := entry.taskID
	path, err := a.client.StopScreenRecord(a.ctx, entry.sess, "")
	if err != nil {
		if taskID != "" {
			a.tasks.Finish(taskID, "err", "", err.Error())
		}
		return "", err
	}
	if taskID != "" {
		a.tasks.Finish(taskID, "ok", path, "")
	}
	if a.sessions != nil {
		a.sessions.Remove("rec:" + serial)
	}
	return path, nil
}

func (a *App) RecordingActive(serial string) bool {
	a.srMu.Lock()
	defer a.srMu.Unlock()
	_, ok := a.srSess[serial]
	return ok
}

// ExportAppDataWithPicker tars+gzips /data/data/<pkg> via root and writes to a
// host file chosen by the user. Long-running — emits task progress events.
func (a *App) ExportAppDataWithPicker(serial, pkg string) (string, error) {
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save app data as…",
		DefaultFilename: pkg + ".tar.gz",
	})
	if err != nil || dst == "" {
		return "", err
	}
	id, _ := a.tasks.Create("export-data", "Exporting "+pkg, "tar /data/data/"+pkg)
	go func() {
		out, err := a.client.ExportAppData(a.ctx, serial, pkg, dst)
		if err != nil {
			a.tasks.Finish(id, "err", "", err.Error())
			return
		}
		a.tasks.Finish(id, "ok", out, "")
	}()
	return id, nil
}

// ─── Screenshot ──────────────────────────────────────────────────────────

func (a *App) TakeScreenshot(serial string) (string, error) {
	return a.client.Screenshot(a.ctx, serial, "")
}

func (a *App) SaveScreenshotAs(serial string) (string, error) {
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save screenshot…",
		DefaultFilename: "screenshot.png",
	})
	if err != nil || dst == "" {
		return "", err
	}
	tmp, err := a.client.Screenshot(a.ctx, serial, "")
	if err != nil {
		return "", err
	}
	if err := moveFile(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	_ = os.Remove(src)
	return nil
}
