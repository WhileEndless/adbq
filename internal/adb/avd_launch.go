package adb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// emulatorStartGrace is how long we wait after spawning `emulator` before
// calling the launch a success. The emulator is slower to fail than scrcpy —
// it parses the AVD and probes acceleration first — so this is longer than the
// 400 ms scrcpy uses.
const emulatorStartGrace = 1200 * time.Millisecond

// bootWaitTimeout bounds how long we wait for sys.boot_completed. A cold boot
// of an old API level on a loaded machine is genuinely slow.
const bootWaitTimeout = 5 * time.Minute

// emulatorProc is one adbq-launched emulator.
type emulatorProc struct {
	cmd  *exec.Cmd
	port int
	log  *hostLog
	done chan struct{}
}

// EmulatorManager owns AVD inventory and the emulator processes adbq starts.
//
// It deliberately tracks only its own children: an emulator the user launched
// from Android Studio is listed and can be stopped through adb, but adbq never
// kills a process it did not start.
type EmulatorManager struct {
	sdk    *SDKManager
	client *Client

	mu    sync.Mutex
	procs map[string]*emulatorProc // AVD name → process
	logs  map[string]*hostLog      // AVD name → log, retained after exit
}

func NewEmulatorManager(sdk *SDKManager, client *Client) *EmulatorManager {
	return &EmulatorManager{
		sdk:    sdk,
		client: client,
		procs:  map[string]*emulatorProc{},
		logs:   map[string]*hostLog{},
	}
}

// ─── inventory ─────────────────────────────────────────────────────────────

// ListAVDs returns every AVD defined on this machine, enriched with live state.
func (m *EmulatorManager) ListAVDs(ctx context.Context) ([]AVD, error) {
	info := m.sdk.Info()
	names, err := ListAVDNames(info.AVDHome)
	if err != nil {
		return nil, fmt.Errorf("cannot read the AVD directory %s: %w", info.AVDHome, err)
	}

	// One adb round-trip for the whole list, not one per AVD.
	live := m.liveEmulators(ctx)

	out := make([]AVD, 0, len(names))
	for _, n := range names {
		a := LoadAVD(info.AVDHome, info.SDKRoot, n)
		m.applyLiveState(ctx, a, live)
		a.Commands = m.commandsFor(a)
		out = append(out, *a)
	}
	return out, nil
}

// AVDByName loads a single AVD with live state, for the detail panel.
func (m *EmulatorManager) AVDByName(ctx context.Context, name string) (*AVD, error) {
	info := m.sdk.Info()
	if info.AVDHome == "" {
		return nil, sdkErr(info)
	}
	if _, err := os.Stat(avdIniPath(info.AVDHome, name)); err != nil {
		return nil, fmt.Errorf("no AVD named %q", name)
	}
	a := LoadAVD(info.AVDHome, info.SDKRoot, name)
	m.applyLiveState(ctx, a, m.liveEmulators(ctx))
	a.Commands = m.commandsFor(a)
	return a, nil
}

func avdIniPath(avdHome, name string) string {
	return avdHome + string(os.PathSeparator) + name + ".ini"
}

// liveEmulator is one running emulator transport as adb sees it.
type liveEmulator struct {
	serial string
	state  string
	avd    string
}

// liveEmulators asks adb which emulator transports exist and which AVD each one
// is running. `emu avd name` is the only reliable mapping — the console port
// tells us nothing about which AVD was booted on it.
func (m *EmulatorManager) liveEmulators(ctx context.Context) map[string]liveEmulator {
	out := map[string]liveEmulator{}
	devs, err := m.client.ListDevices(ctx)
	if err != nil {
		return out
	}
	for _, d := range devs {
		if !strings.HasPrefix(d.ID, "emulator-") {
			continue
		}
		e := liveEmulator{serial: d.ID, state: d.State}
		// An offline transport can't answer the console query; leave avd empty
		// and let the caller fall back to its own process bookkeeping.
		if d.Online {
			e.avd = m.consoleAVDName(ctx, d.ID)
		}
		out[d.ID] = e
	}
	return out
}

// consoleAVDName runs `adb -s <serial> emu avd name`, whose output is the AVD
// name followed by adb's "OK" line.
func (m *EmulatorManager) consoleAVDName(ctx context.Context, serial string) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd, err := m.client.DeviceCommand(cctx, serial, "emu", "avd", "name")
	if err != nil {
		return ""
	}
	out, err := Run(cmd)
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || ln == "OK" || strings.HasPrefix(ln, "KO") {
			continue
		}
		return ln
	}
	return ""
}

// applyLiveState fills in the running half of an AVD: which serial it is on,
// whether it has finished booting, and whether adbq started it.
func (m *EmulatorManager) applyLiveState(ctx context.Context, a *AVD, live map[string]liveEmulator) {
	if a.State == AVDError {
		return
	}
	a.State = AVDStopped

	m.mu.Lock()
	proc := m.procs[a.Name]
	m.mu.Unlock()

	var match *liveEmulator
	for _, e := range live {
		e := e
		if e.avd == a.Name {
			match = &e
			break
		}
		// A transport that is still offline can't be asked its AVD name, so
		// fall back to the port we assigned when we launched it ourselves.
		if e.avd == "" && proc != nil && proc.port > 0 && e.serial == SerialForPort(proc.port) {
			match = &e
			break
		}
	}
	if match == nil {
		// Our child is alive but adb hasn't seen it yet — that's the first few
		// seconds of every launch, and it is "booting", not "stopped".
		if proc != nil && procAlive(proc.cmd) {
			a.State = AVDBooting
			a.Managed = true
			a.Port = proc.port
			a.Serial = SerialForPort(proc.port)
		}
		return
	}

	a.Serial = match.serial
	a.Port = PortForSerial(match.serial)
	a.Managed = proc != nil && procAlive(proc.cmd)

	switch {
	case match.state != "device":
		a.State = AVDBooting
		if match.state == "offline" && (proc == nil || !procAlive(proc.cmd)) {
			// Nobody is driving it and it won't come online: wedged, not booting.
			a.State = AVDOffline
		}
	case !m.bootCompleted(ctx, match.serial):
		a.State = AVDBooting
	default:
		a.State = AVDRunning
		a.Root = m.rootKind(ctx, match.serial)
	}
}

// bootCompleted reads sys.boot_completed, the property Android sets once the
// home screen is usable. Until then adb works but most commands don't.
func (m *EmulatorManager) bootCompleted(ctx context.Context, serial string) bool {
	out, err := m.client.ShellTimeout(ctx, serial, "getprop sys.boot_completed", 5*time.Second)
	return err == nil && strings.TrimSpace(out) == "1"
}

// rootKind reports how root is available on a running emulator, so the UI can
// tell "already rooted via adb root" apart from "needs Magisk".
func (m *EmulatorManager) rootKind(ctx context.Context, serial string) string {
	out, err := m.client.ShellTimeout(ctx, serial, "id -u", 5*time.Second)
	if err == nil && strings.TrimSpace(out) == "0" {
		return "adb-root"
	}
	if _, _, err := m.client.ShellSU(ctx, serial, "id -u"); err == nil {
		return "su"
	}
	return "no"
}

// commandsFor renders the commands behind this AVD's row (CLAUDE.md §4.1).
func (m *EmulatorManager) commandsFor(a *AVD) []string {
	bin := m.sdk.Info().Emulator
	cmds := []string{EmulatorCommand(bin, a.Name, 0, EmulatorOpts{})}
	if a.Serial != "" {
		cmds = append(cmds, "adb -s "+a.Serial+" emu kill")
	}
	return cmds
}

// ─── lifecycle ─────────────────────────────────────────────────────────────

// Start launches an AVD and returns the adb serial it will appear as.
//
// A console port is allocated up front so the serial is known before the
// emulator has booted; without it the caller would have to guess which of
// several emulators it just started.
func (m *EmulatorManager) Start(ctx context.Context, name string, o EmulatorOpts) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("no AVD selected")
	}
	m.mu.Lock()
	if p, ok := m.procs[name]; ok && procAlive(p.cmd) {
		serial := SerialForPort(p.port)
		m.mu.Unlock()
		return serial, nil // already running under our supervision
	}
	m.mu.Unlock()

	bin, err := m.sdk.Emulator()
	if err != nil {
		return "", err
	}

	port, err := allocConsolePort()
	if err != nil {
		return "", err
	}

	args := EmulatorArgs(name, port, o)
	cmd := exec.Command(bin, args...)
	cmd.Env = emulatorEnv(m.sdk.Info())
	// The emulator must outlive the request that started it, so it is not bound
	// to ctx: cancelling a UI call should not kill a booting AVD.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	log := m.logFor(name)
	log.Append("$ "+EmulatorCommand(bin, name, port, o), false)

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("cannot start the emulator: %w", err)
	}

	proc := &emulatorProc{cmd: cmd, port: port, log: log, done: make(chan struct{})}
	m.mu.Lock()
	m.procs[name] = proc
	m.mu.Unlock()

	go pumpLines(stdout, log, false)
	go pumpLines(stderr, log, true)
	go func() {
		_ = cmd.Wait()
		close(proc.done)
		m.mu.Lock()
		if m.procs[name] == proc {
			delete(m.procs, name)
		}
		m.mu.Unlock()
	}()

	// An immediate exit means a bad flag, a busy port, or a broken AVD — report
	// it now instead of leaving the UI waiting for a boot that will never come.
	select {
	case <-proc.done:
		msg := log.LastMeaningful()
		if msg == "" {
			msg = "the emulator exited immediately — check the AVD definition and available disk space"
		}
		return "", errors.New(msg)
	case <-time.After(emulatorStartGrace):
	}
	return SerialForPort(port), nil
}

// WaitForBoot blocks until the emulator finishes booting, the context is
// cancelled, or bootWaitTimeout elapses. onStage reports progress.
func (m *EmulatorManager) WaitForBoot(ctx context.Context, serial string, onStage func(string)) error {
	if onStage == nil {
		onStage = func(string) {}
	}
	deadline := time.Now().Add(bootWaitTimeout)
	onStage("waiting for " + serial)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		devs, err := m.client.ListDevices(ctx)
		if err != nil {
			continue
		}
		online := false
		for _, d := range devs {
			if d.ID == serial && d.Online {
				online = true
				break
			}
		}
		if !online {
			continue
		}
		if m.bootCompleted(ctx, serial) {
			onStage("booted")
			return nil
		}
		onStage("booting " + serial)
	}
	return fmt.Errorf("%s did not finish booting within %s", serial, bootWaitTimeout)
}

// Stop shuts an emulator down. `emu kill` is the graceful path and works for
// emulators adbq did not start; killing our own child process is the fallback.
func (m *EmulatorManager) Stop(ctx context.Context, name string) error {
	m.mu.Lock()
	proc := m.procs[name]
	m.mu.Unlock()

	serial := ""
	if proc != nil {
		serial = SerialForPort(proc.port)
	} else {
		// Not ours: find it through adb.
		for _, e := range m.liveEmulators(ctx) {
			if e.avd == name {
				serial = e.serial
				break
			}
		}
	}
	if serial == "" {
		return fmt.Errorf("%s is not running", name)
	}

	kctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if cmd, err := m.client.DeviceCommand(kctx, serial, "emu", "kill"); err == nil {
		_, _ = Run(cmd)
	}

	if proc == nil {
		return nil // not our child; adb's kill is all we can do
	}
	select {
	case <-proc.done:
		return nil
	case <-time.After(8 * time.Second):
		if proc.cmd.Process != nil {
			return proc.cmd.Process.Kill()
		}
		return nil
	}
}

// StopAll terminates every emulator adbq started. Called on app shutdown so we
// don't leave orphaned VMs behind; emulators started elsewhere are left alone.
func (m *EmulatorManager) StopAll() {
	m.mu.Lock()
	procs := make([]*emulatorProc, 0, len(m.procs))
	for _, p := range m.procs {
		procs = append(procs, p)
	}
	m.procs = map[string]*emulatorProc{}
	m.mu.Unlock()
	for _, p := range procs {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
}

// IsManaged reports whether adbq launched (and still supervises) this AVD.
func (m *EmulatorManager) IsManaged(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.procs[name]
	return p != nil && procAlive(p.cmd)
}

// ─── logs ──────────────────────────────────────────────────────────────────

// logFor returns the AVD's log buffer, creating it on first use. Buffers
// outlive the process so a failed launch can still be inspected.
func (m *EmulatorManager) logFor(name string) *hostLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.logs[name]
	if l == nil {
		l = newHostLog()
		m.logs[name] = l
	}
	return l
}

// LogSince returns emulator output newer than sinceSeq (0 = everything held).
func (m *EmulatorManager) LogSince(name string, sinceSeq int) []HostLogLine {
	return m.logFor(name).Since(sinceSeq)
}

// ClearLog empties an AVD's log buffer.
func (m *EmulatorManager) ClearLog(name string) { m.logFor(name).Clear() }

// ─── helpers ───────────────────────────────────────────────────────────────

// allocConsolePort finds a free even console port in the emulator's range. The
// emulator needs both port and port+1, so we probe the pair; a race with
// another process is still possible, which the start grace window catches.
func allocConsolePort() (int, error) {
	for p := emulatorPortMin; p <= emulatorPortMax; p += 2 {
		if portFree(p) && portFree(p+1) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free emulator console port between %d and %d — too many emulators are already running",
		emulatorPortMin, emulatorPortMax)
}

func portFree(p int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// emulatorEnv gives the emulator an explicit ANDROID_SDK_ROOT/ANDROID_AVD_HOME.
// Launched from Finder or the Dock, adbq inherits almost no environment, and
// the emulator then can't find its own system images.
func emulatorEnv(info AndroidSDKInfo) []string {
	env := os.Environ()
	set := func(key, val string) {
		if val == "" {
			return
		}
		for i, e := range env {
			if strings.HasPrefix(e, key+"=") {
				env[i] = key + "=" + val
				return
			}
		}
		env = append(env, key+"="+val)
	}
	set("ANDROID_SDK_ROOT", info.SDKRoot)
	set("ANDROID_HOME", info.SDKRoot)
	set("ANDROID_AVD_HOME", info.AVDHome)
	return env
}

// procAlive reports whether a started child is still running, reaping the
// bookkeeping for one that has exited. Mirrors ScrcpyManager.IsActive.
func procAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return false
	}
	return cmd.Process.Signal(syscallZero) == nil
}

// pumpLines streams a child's output into the bounded log.
func pumpLines(r interface{ Read([]byte) (int, error) }, log *hostLog, isErr bool) {
	sc := LineScanner(r)
	for sc.Scan() {
		log.Append(sc.Text(), isErr)
	}
}
