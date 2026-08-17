package adb

import "context"

// Command previews for the Network screen's write actions.
//
// Installing a system CA and writing a hosts file are the two most invasive
// things adbq does to a device: both remount or overlay a read-only partition,
// and both walk an escalation of strategies until one sticks. Neither showed a
// command before, which is exactly backwards — the more a step can surprise
// someone, the more they should be able to read it first (CLAUDE.md §4.1 K2).
//
// The escalations come from the same builders the installers walk, so the list
// is what will be attempted, in order, not a description of it.

// certNamePlaceholder stands in for the trust-store file name, which is derived
// from the certificate's subject hash and so is unknown until a file is chosen.
const certNamePlaceholder = "<subject-hash>.0"

// certFilePlaceholder stands in for the certificate the picker has not been
// shown for yet.
const certFilePlaceholder = "<certificate.pem>"

// CertInstallPlan is what installing a CA certificate will attempt on this
// device, in order.
type CertInstallPlan struct {
	Rooted bool `json:"rooted"`
	// Store is "system" on a rooted device and "user" otherwise; the two paths
	// share nothing but the staging push.
	Store      string   `json:"store"`
	Path       string   `json:"path"`
	Persistent bool     `json:"persistent"`
	Commands   []string `json:"commands"`
	Note       string   `json:"note"`
}

// CertInstallPlanFor renders the install. Without root the certificate can only
// be staged for a manual import, so that is all the plan claims.
func CertInstallPlanFor(serial string, rooted bool, render CommandRenderer) CertInstallPlan {
	stage := cacertStage
	push := []string{
		DeviceCommandText(serial, "push", certFilePlaceholder, stage),
		render("chmod 644 "+stage, false),
	}
	if !rooted {
		dst := "/sdcard/Download/adbq-ca-<subject-hash>.crt"
		return CertInstallPlan{
			Store: "user", Path: dst,
			Commands: append(push,
				render("mkdir -p /sdcard/Download && cp "+stage+" "+dst, false),
				render("rm -f "+stage, false),
				render("am start -a android.settings.SECURITY_SETTINGS >/dev/null 2>&1 || true", false),
			),
			Note: "Not rooted: the certificate is staged for a manual import via Settings. Most apps on Android 7+ ignore user-store CAs.",
		}
	}
	dest := systemCacertsDir + "/" + certNamePlaceholder
	cmds := append(push, "# tried in order, stopping at the first one the store keeps:")
	for _, s := range cacertStrategies(stage, dest) {
		cmds = append(cmds, render(s.Cmd, true))
	}
	cmds = append(cmds,
		"# if /system stays read-only (Android 10+), overlay a tmpfs on the store instead:",
		render(cacertOverlayCmd(stage, certNamePlaceholder), true),
		render("rm -f "+stage, false),
	)
	return CertInstallPlan{
		Rooted: true, Store: "system", Path: dest, Persistent: true, Commands: cmds,
		Note: "The file name comes from the certificate's subject hash, so it is only known once a file is picked.",
	}
}

// PlanCertInstall is the device-aware entry point: it reports the store the
// device will actually use, because root decides that and nothing else.
func (c *Client) PlanCertInstall(ctx context.Context, serial string) CertInstallPlan {
	style, _ := c.suStyleFor(ctx, serial)
	return CertInstallPlanFor(serial, styleGrantsRoot(style), c.Renderer(ctx, serial))
}

// HostsApplyPlan is what writing the hosts file will attempt, in order.
type HostsApplyPlan struct {
	Path     string   `json:"path"`
	Commands []string `json:"commands"`
	Note     string   `json:"note"`
}

// HostsApplyPlanFor renders the write for the content currently in the editor,
// so the staged bytes in the preview are the bytes that get staged.
func HostsApplyPlanFor(serial, hostsPath, content string, render CommandRenderer) HostsApplyPlan {
	if hostsPath == "" {
		hostsPath = "/system/etc/hosts"
	}
	cmds := []string{
		render(hostsStageCmd(normalizeHostsContent(content)), false),
		"# tried in order, stopping at the first write that reads back intact:",
	}
	for _, s := range hostsStrategies(hostsStage, hostsPath) {
		cmds = append(cmds, render(s.Cmd, true))
		cmds = append(cmds, render(hostsVerifyCmd(hostsPath), true))
	}
	cmds = append(cmds,
		"# last resort, if every write is rejected — needs one reboot to take effect:",
		render(hostsMagiskModuleCmd(hostsStage, md5sum(normalizeHostsContent(content))), true),
		"# finally, so running apps stop using the old answers:",
		render(flushDNSScript(), true),
	)
	return HostsApplyPlan{Path: hostsPath, Commands: cmds,
		Note: "The staging file stays in place for the bind-mount strategy, which serves the live file from it."}
}

// PlanHostsApply resolves the real hosts path on the device — one of
// /system/etc/hosts and /etc/hosts is usually a symlink to the other — then
// renders the plan.
func (c *Client) PlanHostsApply(ctx context.Context, serial, content string) HostsApplyPlan {
	return HostsApplyPlanFor(serial, c.canonicalHostsPath(ctx, serial), content, c.Renderer(ctx, serial))
}

// NetCommands are the Network screen's smaller actions.
type NetCommands struct {
	Proxy       []string `json:"proxy"`
	ClearProxy  []string `json:"clearProxy"`
	FlushDNS    []string `json:"flushDns"`
	ReadProxy   []string `json:"readProxy"`
	ReadDNS     []string `json:"readDns"`
	Connections []string `json:"connections"`
}

// NetCommandsFor renders them for the proxy value currently in the form.
func NetCommandsFor(serial, hostPort string, render CommandRenderer) NetCommands {
	return NetCommands{
		Proxy:      []string{ProxyCommand(serial, hostPort)},
		ClearProxy: []string{ProxyCommand(serial, "")},
		FlushDNS:   []string{render(flushDNSScript(), true)},
		ReadProxy:  []string{render("settings get global http_proxy", false)},
		ReadDNS:    []string{render("getprop net.dns1; getprop net.dns2", false)},
		// Connections are read from procfs rather than `ss`, which stripped ROMs
		// omit.
		Connections: []string{render(connectionsRemote(), false)},
	}
}

// NetCommandsFor is the device-aware entry point.
func (c *Client) NetCommandsFor(ctx context.Context, serial, hostPort string) NetCommands {
	return NetCommandsFor(serial, hostPort, c.Renderer(ctx, serial))
}
