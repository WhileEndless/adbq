package main

import (
	"fmt"
	"os"
	"strings"

	"adbq/internal/adb"
)

// ─── Profile CRUD bindings ──────────────────────────────────────────────────

// ListProfiles returns all saved profiles.
func (a *App) ListProfiles() ([]adb.Profile, error) {
	if a.profiles == nil {
		return nil, fmt.Errorf("profile store unavailable")
	}
	return a.profiles.ListProfiles(), nil
}

// GetProfile returns a profile by id.
func (a *App) GetProfile(id string) (*adb.Profile, error) {
	p, ok := a.profiles.GetProfile(id)
	if !ok {
		return nil, fmt.Errorf("profile %s not found", id)
	}
	return &p, nil
}

// SaveProfile creates or updates a profile and returns the stored copy.
func (a *App) SaveProfile(p adb.Profile) (*adb.Profile, error) {
	if a.profiles == nil {
		return nil, fmt.Errorf("profile store unavailable")
	}
	saved, err := a.profiles.SaveProfile(p)
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// DeleteProfile removes a profile and unbinds any device using it.
func (a *App) DeleteProfile(id string) error {
	return a.profiles.DeleteProfile(id)
}

// ─── Device records & binding ───────────────────────────────────────────────

// ListDeviceRecords returns the known device history (connected or not).
func (a *App) ListDeviceRecords() ([]adb.DeviceRecord, error) {
	if a.profiles == nil {
		return nil, fmt.Errorf("profile store unavailable")
	}
	return a.profiles.ListDeviceRecords(), nil
}

// RegisterDevice records that a device was seen (refreshes metadata/LastSeen).
// The frontend calls this for every polled device so the history stays current.
func (a *App) RegisterDevice(d adb.Device) error {
	if a.profiles == nil {
		return nil
	}
	a.profiles.UpsertDeviceSeen(adb.DeviceRecord{
		Key:            adb.DeviceKey(&d),
		AdbSerial:      d.ID,
		HardwareSerial: d.HardwareSerial,
		Label:          d.Label,
		Model:          d.Model,
		Manufacturer:   d.Manufacturer,
	})
	return nil
}

// LookupDeviceProfile returns the profile id bound to a device key, or "".
func (a *App) LookupDeviceProfile(key string) (string, error) {
	rec, ok := a.profiles.GetDevice(key)
	if !ok {
		return "", nil
	}
	return rec.BoundProfileID, nil
}

// DeviceKey returns the stable identity key for a connected serial.
func (a *App) DeviceKey(serial string) (string, error) {
	d, err := a.DeviceDetails(serial)
	if err != nil {
		return "", err
	}
	return adb.DeviceKey(d), nil
}

// BindDeviceProfile binds a profile to the connected device's stable key.
func (a *App) BindDeviceProfile(serial, profileID string) error {
	d, err := a.DeviceDetails(serial)
	if err != nil {
		return err
	}
	return a.profiles.BindProfile(adb.DeviceKey(d), profileID)
}

// BindDeviceProfileByKey binds a profile to a device key directly, so the user
// can change a disconnected device's profile from the past-devices view.
func (a *App) BindDeviceProfileByKey(key, profileID string) error {
	return a.profiles.BindProfile(key, profileID)
}

// ─── Apply engine ────────────────────────────────────────────────────────────

const (
	defaultFridaIface = "0.0.0.0"
	defaultFridaPort  = 27042
)

// PreviewProfile describes what applying a profile to the connected device would
// do, so the UI can show a confirmation and grey out root-gated steps.
func (a *App) PreviewProfile(serial, profileID string) ([]adb.StepPreview, error) {
	p, ok := a.profiles.GetProfile(profileID)
	if !ok {
		return nil, fmt.Errorf("profile %s not found", profileID)
	}
	d, err := a.DeviceDetails(serial)
	if err != nil {
		return nil, err
	}
	// One renderer for the whole preview: each step's commands come from the same
	// builders the apply engine runs, so the list is the work rather than a
	// description of it.
	render := a.client.Renderer(a.ctx, serial)
	var out []adb.StepPreview
	add := func(name, title, detail string, needsRoot bool, commands []string) {
		sp := adb.StepPreview{Name: name, Title: title, Detail: detail, NeedsRoot: needsRoot, Commands: commands}
		if needsRoot && !d.Root {
			sp.WillSkip = true
			sp.SkipReason = "device is not rooted"
		}
		out = append(out, sp)
	}
	if p.Iptables.Enabled && (p.Iptables.V4Blob != "" || p.Iptables.V6Blob != "") {
		var cmds []string
		for _, fam := range []adb.IPFamily{adb.IPv4, adb.IPv6} {
			blob := p.Iptables.V4Blob
			if fam == adb.IPv6 {
				blob = p.Iptables.V6Blob
			}
			if blob == "" {
				continue
			}
			ic := adb.IptablesCommandsFor(serial, adb.IptablesCommandRequest{Family: string(fam)}, render)
			cmds = append(cmds, ic.Import...)
		}
		add("iptables", "Restore iptables ruleset", "imports saved iptables-save blob(s)", true, cmds)
	}
	if p.Hosts.Enabled {
		hp := a.client.PlanHostsApply(a.ctx, serial, p.Hosts.Content)
		add("hosts", "Apply /etc/hosts override", fmt.Sprintf("%d bytes", len(p.Hosts.Content)), true, hp.Commands)
	}
	if p.Cert.Enabled && p.Cert.PEM != "" {
		cp := a.client.PlanCertInstall(a.ctx, serial)
		add("cert", "Install CA certificate", p.Cert.Subject+" (system store needs root; else user store)", false, cp.Commands)
	}
	if p.Frida.Enabled {
		ver := p.Frida.Version
		if ver == "" {
			ver = "latest"
		}
		title := "Install frida-server " + ver
		if p.Frida.Start {
			title += " + start"
		}
		fc := a.client.FridaCommandsFor(a.ctx, serial, "", p.Frida.Port)
		cmds := fc.Install
		if p.Frida.Start {
			cmds = append(append([]string{}, cmds...), fc.Start...)
		}
		add("frida", title, "arch resolved from device", p.Frida.Start, cmds)
	}
	if p.Forwards.Enabled && (len(p.Forwards.Forwards) > 0 || len(p.Forwards.Reverses) > 0) {
		var cmds []string
		for _, f := range p.Forwards.Forwards {
			cmds = append(cmds, adb.ForwardCommandsFor(serial, "forward", f.Local, f.Remote).Add...)
		}
		for _, r := range p.Forwards.Reverses {
			cmds = append(cmds, adb.ForwardCommandsFor(serial, "reverse", r.Local, r.Remote).Add...)
		}
		add("forwards", "Set up forwards/reverses",
			fmt.Sprintf("%d forward(s), %d reverse(s)", len(p.Forwards.Forwards), len(p.Forwards.Reverses)), false, cmds)
	}
	if p.Proxy.Enabled {
		var cmds []string
		if p.Proxy.HostPort == "auto" {
			// The host address is resolved against this machine's interfaces at
			// apply time, so the exact value is not knowable here — say so rather
			// than print a guess.
			cmds = []string{"# host:port resolved from this computer's addresses when applied, then:",
				adb.ProxyCommand(serial, "<host>:"+fmt.Sprint(p.Proxy.Port))}
		} else {
			cmds = []string{adb.ProxyCommand(serial, p.Proxy.HostPort)}
		}
		add("proxy", "Set global HTTP proxy", proxyDetail(p.Proxy), false, cmds)
	}
	return out, nil
}

func proxyDetail(s adb.ProxyStep) string {
	switch s.HostPort {
	case "":
		return "clear proxy"
	case "auto":
		return "auto-resolve host:port"
	default:
		return s.HostPort
	}
}

// ApplyProfile runs every enabled step of a profile against the connected
// device, reporting per-step results. On success the device remembers the
// profile as its default (so it re-applies on reconnect, after confirmation).
func (a *App) ApplyProfile(serial, profileID string) (*adb.ApplyReport, error) {
	p, ok := a.profiles.GetProfile(profileID)
	if !ok {
		return nil, fmt.Errorf("profile %s not found", profileID)
	}
	d, err := a.DeviceDetails(serial)
	if err != nil {
		return nil, err
	}
	report := &adb.ApplyReport{ProfileID: p.ID, ProfileName: p.Name, Serial: serial, Rooted: d.Root}

	id, _ := a.tasks.Create("profile-apply", "Apply profile: "+p.Name, serial)
	note := func(step string) { a.tasks.Update(id, func(t *adb.TaskState) { t.Detail = step + " · " + serial }) }

	// Order: ruleset → hosts → cert → frida → forwards → proxy (network last).
	if p.Iptables.Enabled && (p.Iptables.V4Blob != "" || p.Iptables.V6Blob != "") {
		note("iptables")
		report.Steps = append(report.Steps, a.applyIptables(serial, d, p.Iptables))
	}
	if p.Hosts.Enabled {
		note("hosts")
		report.Steps = append(report.Steps, a.applyHosts(serial, d, p.Hosts))
	}
	if p.Cert.Enabled && p.Cert.PEM != "" {
		note("cert")
		report.Steps = append(report.Steps, a.applyCert(serial, p.Cert))
	}
	if p.Frida.Enabled {
		note("frida")
		report.Steps = append(report.Steps, a.applyFrida(serial, d, p.Frida, note))
	}
	if p.Forwards.Enabled && (len(p.Forwards.Forwards) > 0 || len(p.Forwards.Reverses) > 0) {
		note("forwards")
		report.Steps = append(report.Steps, a.applyForwards(serial, p.Forwards))
	}
	if p.Proxy.Enabled {
		note("proxy")
		report.Steps = append(report.Steps, a.applyProxy(serial, p.Proxy))
	}

	var anyOK, anyErr bool
	for _, s := range report.Steps {
		if s.NeedsReboot {
			report.NeedsReboot = true
		}
		switch s.Status {
		case "ok":
			anyOK = true
		case "err":
			anyErr = true
		}
	}
	// Remember this profile as the device's default for next reconnect — but not
	// if every step failed (don't adopt a profile that did nothing useful).
	if anyOK || len(report.Steps) == 0 {
		_ = a.profiles.BindProfile(adb.DeviceKey(d), profileID)
	}

	status := "ok"
	if anyErr {
		status = "err"
	}
	a.tasks.Finish(id, status, "", summarizeReport(report))
	return report, nil
}

func summarizeReport(r *adb.ApplyReport) string {
	var ok, skip, errc int
	for _, s := range r.Steps {
		switch s.Status {
		case "ok":
			ok++
		case "skip":
			skip++
		case "err":
			errc++
		}
	}
	return fmt.Sprintf("%d ok · %d skipped · %d failed", ok, skip, errc)
}

func skipNoRoot(name string) adb.StepResult {
	return adb.StepResult{Name: name, Status: "skip", Message: "requires root"}
}

func (a *App) applyIptables(serial string, d *adb.Device, step adb.IptablesStep) adb.StepResult {
	if !d.Root {
		return skipNoRoot("iptables")
	}
	var msgs []string
	apply := func(fam adb.IPFamily, blob string) error {
		if blob == "" {
			return nil
		}
		if pb, _ := a.client.ProbeIptables(a.ctx, serial, fam); pb != nil && pb.ReadOnly {
			return fmt.Errorf("%s: device is nftables-only (import unsupported)", fam)
		}
		return a.client.ImportIptables(a.ctx, serial, fam, blob)
	}
	if err := apply(adb.IPv4, step.V4Blob); err != nil {
		return adb.StepResult{Name: "iptables", Status: "err", Message: err.Error()}
	} else if step.V4Blob != "" {
		msgs = append(msgs, "ipv4 imported")
	}
	if err := apply(adb.IPv6, step.V6Blob); err != nil {
		return adb.StepResult{Name: "iptables", Status: "err", Message: err.Error()}
	} else if step.V6Blob != "" {
		msgs = append(msgs, "ipv6 imported")
	}
	return adb.StepResult{Name: "iptables", Status: "ok", Message: strings.Join(msgs, ", ")}
}

func (a *App) applyHosts(serial string, d *adb.Device, step adb.HostsStep) adb.StepResult {
	if !d.Root {
		return skipNoRoot("hosts")
	}
	res, err := a.client.ApplyHostsRobust(a.ctx, serial, step.Content)
	if err != nil {
		return adb.StepResult{Name: "hosts", Status: "err", Message: err.Error()}
	}
	_ = adb.SaveHostsConfig(serial, step.Content)
	if step.FlushDNS {
		_, _ = a.client.FlushDNS(a.ctx, serial)
	}
	return adb.StepResult{Name: "hosts", Status: "ok", Message: "applied via " + res.Strategy, NeedsReboot: res.NeedsReboot}
}

func (a *App) applyCert(serial string, step adb.CertStep) adb.StepResult {
	tmp, err := os.CreateTemp("", "adbq-profile-cert-*.pem")
	if err != nil {
		return adb.StepResult{Name: "cert", Status: "err", Message: err.Error()}
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(step.PEM); err != nil {
		tmp.Close()
		return adb.StepResult{Name: "cert", Status: "err", Message: err.Error()}
	}
	tmp.Close()
	res, err := a.client.InstallSystemCert(a.ctx, serial, tmp.Name())
	if err != nil {
		return adb.StepResult{Name: "cert", Status: "err", Message: err.Error()}
	}
	// Only a rooted, non-persistent (tmpfs-overlay) install actually benefits
	// from a reboot note. A user-store install (unrooted) is also non-persistent
	// but a reboot does nothing for it, so don't flag it.
	return adb.StepResult{Name: "cert", Status: "ok", Message: res.Note, NeedsReboot: res.Rooted && !res.Persistent}
}

func (a *App) applyFrida(serial string, d *adb.Device, step adb.FridaStep, note func(string)) adb.StepResult {
	arch := step.Arch
	if step.AutoArch || arch == "" {
		info, err := a.client.FridaArchInfo(a.ctx, serial)
		if err != nil || info.Primary == "" {
			return adb.StepResult{Name: "frida", Status: "err", Message: "could not detect device frida arch"}
		}
		arch = info.Primary
	}
	version := step.Version
	// Resolve "latest" only when no version is pinned — avoids an unnecessary
	// GitHub round-trip on every apply for a pinned version.
	if version == "" {
		releases, relErr := a.client.ListFridaReleases(a.ctx, serial, arch)
		if relErr != nil || len(releases) == 0 {
			return adb.StepResult{Name: "frida", Status: "err", Message: "could not resolve latest frida-server version"}
		}
		version = releases[0].Version
	}
	// Reuse an already-installed matching server if present, and note whether a
	// frida-server is already running so we don't start a second instance.
	path := ""
	var running *adb.FridaServer
	if servers, err := a.client.ListFridaServers(a.ctx, serial); err == nil {
		for i := range servers {
			s := servers[i]
			if path == "" && s.Version == version && (s.Arch == "" || s.Arch == arch) {
				path = s.Path
			}
			if s.Active {
				sc := s
				running = &sc
			}
		}
	}
	if path == "" {
		note("frida: install " + version)
		p, err := a.client.InstallFridaServer(a.ctx, serial, version, arch, func(stage string) { note("frida: " + stage) })
		if err != nil {
			return adb.StepResult{Name: "frida", Status: "err", Message: "install failed: " + err.Error()}
		}
		path = p
	}
	if !step.Start {
		return adb.StepResult{Name: "frida", Status: "ok", Message: "installed " + version + " (" + arch + ")"}
	}
	// Already up — don't launch a second instance (it would just collide on the
	// port). Idempotent: "apply" on a device that already has frida running is a
	// no-op for this step.
	if running != nil {
		return adb.StepResult{Name: "frida", Status: "ok", Message: fmt.Sprintf("already running: %s (pid %d, port %d)", running.Name, running.PID, running.Port)}
	}
	if !d.Root {
		return adb.StepResult{Name: "frida", Status: "skip", Message: "installed " + version + "; starting needs root"}
	}
	iface := step.Iface
	if iface == "" {
		iface = defaultFridaIface
	}
	port := step.Port
	if port <= 0 {
		port = defaultFridaPort
	}
	if _, err := a.client.StartFrida(a.ctx, serial, path, iface, port); err != nil {
		return adb.StepResult{Name: "frida", Status: "err", Message: "start failed: " + err.Error()}
	}
	return adb.StepResult{Name: "frida", Status: "ok", Message: fmt.Sprintf("started %s (%s) on %s:%d", version, arch, iface, port)}
}

func (a *App) applyForwards(serial string, step adb.ForwardsStep) adb.StepResult {
	have := map[string]bool{}
	if fwds, err := a.client.ListForwards(a.ctx, serial); err == nil {
		for _, f := range fwds {
			have["f|"+f.Local+"|"+f.Remote] = true
		}
	}
	if revs, err := a.client.ListReverses(a.ctx, serial); err == nil {
		for _, f := range revs {
			have["r|"+f.Remote+"|"+f.Local] = true
		}
	}
	var added int
	var errs []string
	for _, f := range step.Forwards {
		if have["f|"+f.Local+"|"+f.Remote] {
			continue
		}
		if _, err := a.client.AddForward(a.ctx, serial, f.Local, f.Remote); err != nil {
			errs = append(errs, fmt.Sprintf("forward %s→%s: %v", f.Local, f.Remote, err))
		} else {
			added++
		}
	}
	for _, r := range step.Reverses {
		if have["r|"+r.Remote+"|"+r.Local] {
			continue
		}
		if _, err := a.client.AddReverse(a.ctx, serial, r.Remote, r.Local); err != nil {
			errs = append(errs, fmt.Sprintf("reverse %s→%s: %v", r.Remote, r.Local, err))
		} else {
			added++
		}
	}
	if len(errs) > 0 {
		return adb.StepResult{Name: "forwards", Status: "err", Message: strings.Join(errs, "; ")}
	}
	return adb.StepResult{Name: "forwards", Status: "ok", Message: fmt.Sprintf("%d added (others already present)", added)}
}

func (a *App) applyProxy(serial string, step adb.ProxyStep) adb.StepResult {
	target := step.HostPort
	if target == "auto" {
		sugg, err := a.SuggestProxyHost(serial, step.Port)
		if err != nil || sugg == nil || sugg.Host == "" {
			return adb.StepResult{Name: "proxy", Status: "err", Message: "could not auto-resolve proxy host"}
		}
		if sugg.NeedsReverse {
			_, _ = a.client.AddReverse(a.ctx, serial, fmt.Sprintf("tcp:%d", sugg.Port), fmt.Sprintf("tcp:%d", sugg.Port))
		}
		target = fmt.Sprintf("%s:%d", sugg.Host, sugg.Port)
	}
	if cur, err := a.client.GetProxy(a.ctx, serial); err == nil && strings.TrimSpace(cur) == target && target != "" {
		return adb.StepResult{Name: "proxy", Status: "ok", Message: "already set to " + target}
	}
	if _, err := a.client.SetProxy(a.ctx, serial, target); err != nil {
		return adb.StepResult{Name: "proxy", Status: "err", Message: err.Error()}
	}
	if target == "" {
		return adb.StepResult{Name: "proxy", Status: "ok", Message: "proxy cleared"}
	}
	return adb.StepResult{Name: "proxy", Status: "ok", Message: "set to " + target}
}

// ─── Capture-from-device ─────────────────────────────────────────────────────

// CaptureProfileFromDevice snapshots the connected device's current settings
// into a new saved profile (best-effort; a feature that errors stays disabled).
func (a *App) CaptureProfileFromDevice(serial, name string) (*adb.Profile, error) {
	if _, err := a.DeviceDetails(serial); err != nil {
		return nil, err
	}
	p := adb.Profile{Name: name}
	// Non-nil so they marshal as [] not null — the editor would crash mapping a
	// null array.
	p.Forwards.Forwards = []adb.ForwardSpec{}
	p.Forwards.Reverses = []adb.ReverseSpec{}

	if fwds, err := a.client.ListForwards(a.ctx, serial); err == nil {
		for _, f := range fwds {
			p.Forwards.Forwards = append(p.Forwards.Forwards, adb.ForwardSpec{Local: f.Local, Remote: f.Remote})
		}
	}
	if revs, err := a.client.ListReverses(a.ctx, serial); err == nil {
		for _, r := range revs {
			p.Forwards.Reverses = append(p.Forwards.Reverses, adb.ReverseSpec{Remote: r.Remote, Local: r.Local})
		}
	}
	p.Forwards.Enabled = len(p.Forwards.Forwards) > 0 || len(p.Forwards.Reverses) > 0

	if proxy, err := a.client.GetProxy(a.ctx, serial); err == nil && strings.TrimSpace(proxy) != "" {
		p.Proxy = adb.ProxyStep{ProfileStep: adb.ProfileStep{Enabled: true}, HostPort: strings.TrimSpace(proxy)}
	}

	if content, err := adb.LoadHostsConfig(serial); err == nil && strings.TrimSpace(content) != "" {
		p.Hosts = adb.HostsStep{ProfileStep: adb.ProfileStep{Enabled: true}, Content: content, FlushDNS: true}
	}

	if servers, err := a.client.ListFridaServers(a.ctx, serial); err == nil && len(servers) > 0 {
		pick := servers[0]
		anyActive := false
		for _, s := range servers {
			if s.Active {
				pick, anyActive = s, true
				break
			}
		}
		p.Frida = adb.FridaStep{
			ProfileStep: adb.ProfileStep{Enabled: true},
			Version:     pick.Version,
			AutoArch:    true,
			Start:       anyActive,
			Port:        defaultFridaPort,
		}
	}

	if blob, err := a.client.ExportIptables(a.ctx, serial, adb.IPv4); err == nil && strings.TrimSpace(blob) != "" {
		p.Iptables = adb.IptablesStep{ProfileStep: adb.ProfileStep{Enabled: false}, V4Blob: blob}
	}

	saved, err := a.profiles.SaveProfile(p)
	if err != nil {
		return nil, err
	}
	return &saved, nil
}
