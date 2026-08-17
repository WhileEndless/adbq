package adb

import (
	"context"
	"strconv"
)

// Command previews for the Frida screen.
//
// Frida is the part of adbq that does the most on the user's behalf: it pushes a
// binary, makes it executable, daemonizes it, forwards a port and drives a host
// interpreter. All of it was invisible. Showing the commands is also the fastest
// route out of the usual failures — a server that will not start, a version
// mismatch, an SELinux denial — because the same lines can be run by hand.

// fridaServerPlaceholder stands in for the binary before a version is chosen.
const fridaServerPlaceholder = "frida-server-<version>-android-<arch>"

// FridaCommands is one command list per Frida action.
type FridaCommands struct {
	// Install is the device half of installing a server: adbq downloads and
	// verifies the asset on this computer first, which is not a device command.
	Install []string `json:"install"`
	List    []string `json:"list"`
	Start   []string `json:"start"`
	Stop    []string `json:"stop"`
	Log     []string `json:"log"`
	Forward []string `json:"forward"`
	Version []string `json:"version"`
}

// FridaCommandsFor renders them for one server path and port. serverPath may be
// empty — the preview then names the placeholder, because that is honestly all
// that is known before a version is picked.
func FridaCommandsFor(serial, serverPath string, port int, render CommandRenderer) FridaCommands {
	port = fridaPortOrDefault(port)
	if serverPath == "" {
		serverPath = fridaServerDir + "/" + fridaServerPlaceholder
	}
	fc := FridaCommands{
		Install: []string{
			"# downloaded and checksum-verified on this computer first, then:",
			DeviceCommandText(serial, "push", "<frida-server>", serverPath),
			render(fridaChmodRemote(serverPath), true),
		},
		List:    []string{render(fridaListRemote(), false)},
		Start:   []string{render(fridaStartRemote(serverPath, "0.0.0.0", port), true)},
		Stop:    []string{render(fridaStopScript, true)},
		Log:     []string{render(fridaLogRemote(port), true)},
		Version: []string{render(shQuote(serverPath)+" --version", true)},
	}
	// The default port is reachable through frida's own Android backend; any
	// other one has to be forwarded and dialled as a remote device.
	if port != FridaDefaultPort {
		fc.Forward = []string{DeviceCommandText(serial, "forward", "tcp:0", "tcp:"+strconv.Itoa(port))}
	}
	return fc
}

// FridaCommandsFor is the device-aware entry point.
func (c *Client) FridaCommandsFor(ctx context.Context, serial, serverPath string, port int) FridaCommands {
	return FridaCommandsFor(serial, serverPath, port, c.Renderer(ctx, serial))
}

// FridaSessionCommands describes what starting a session runs on this computer.
//
// adbq does not shell out to the `frida` CLI: it runs a small driver script on a
// pinned interpreter, because a session has to survive, take script updates and
// report messages back. That is what Runner shows. The CLI line beside it is the
// closest thing a person can paste into a terminal to reproduce the attach, and
// it is labelled as such rather than passed off as what ran.
type FridaSessionCommands struct {
	Runner []string `json:"runner"`
	CLI    []string `json:"cli"`
}

// FridaSessionCommandsFor renders both. interpreter is the resolved python; job
// is the path to the driver's job file, which only exists while a session runs.
func FridaSessionCommandsFor(interpreter, driver, job, pkg, mode string, scripts int, remoteAddress string) FridaSessionCommands {
	var sc FridaSessionCommands
	if interpreter != "" && driver != "" {
		if job == "" {
			job = "<job.json>"
		}
		sc.Runner = []string{HostCommandText(interpreter, "-u", driver, job)}
	}
	cli := []string{"frida"}
	if remoteAddress != "" {
		cli = append(cli, "-H", remoteAddress)
	} else {
		cli = append(cli, "-U")
	}
	if mode == "attach" {
		cli = append(cli, "-n", pkg)
	} else {
		cli = append(cli, "-f", pkg)
	}
	for i := 0; i < scripts; i++ {
		cli = append(cli, "-l", "<script-"+strconv.Itoa(i+1)+".js>")
	}
	sc.CLI = []string{
		"# the equivalent attach with frida's own CLI:",
		HostCommandText(cli[0], cli[1:]...),
	}
	return sc
}
