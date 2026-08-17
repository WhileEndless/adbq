package adb

import (
	"context"
	"strings"
)

// Rendering the commands adbq runs, for display.
//
// Every screen that offers a device action must be able to show the command
// behind it (CLAUDE.md §4.1), and the text has to be the *same* text in every
// screen — a `pm clear` line that quotes its argument here and not there reads
// like two different tools. These helpers are the single place that decides how
// an `adb` invocation is spelled, so a builder anywhere in the package only has
// to describe the arguments.
//
// The output is paste-ready: arguments that a host shell would mangle are
// quoted, and a remote command travels as one argument, exactly as adb receives
// it.

// quoteArg quotes s for a host shell when it needs it, and leaves plain words
// alone so the common case stays readable.
func quoteArg(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\r'\"$`\\|&;<>()*?[]{}#~!^") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// HostCommandText renders a command adbq runs on this computer (emulator,
// sdkmanager, jadx, frida…).
func HostCommandText(bin string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(bin))
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// DeviceCommandText renders `adb -s <serial> <args…>`. The serial is always
// spelled out: a copied command has to keep working with several devices
// attached, which is when the user needs it most.
func DeviceCommandText(serial string, args ...string) string {
	parts := make([]string, 0, len(args)+3)
	parts = append(parts, "adb")
	if serial != "" {
		parts = append(parts, "-s", quoteArg(serial))
	}
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// ShellCommandText renders `adb -s <serial> shell '<remote>'`. The remote
// command is quoted as a single argument because that is how Client.Shell
// passes it — splitting it would change what the device runs.
func ShellCommandText(serial, remote string) string {
	return DeviceCommandText(serial, "shell", remote)
}

// ShellCommandTextRoot renders what ShellSU would run, including the `su` form
// this particular device accepts, so the displayed line is the line that runs.
//
// When root is unavailable the action itself will fail; showing the plain `su
// -c` form is the honest preview of the attempt, and the failure is reported by
// the action rather than hidden in the preview.
func (c *Client) ShellCommandTextRoot(ctx context.Context, serial, remote string, asRoot bool) string {
	if !asRoot {
		return ShellCommandText(serial, remote)
	}
	if wrapped, err := c.rootWrap(ctx, serial, remote); err == nil {
		return ShellCommandText(serial, wrapped)
	}
	return ShellCommandText(serial, "su -c "+shQuote(remote))
}
