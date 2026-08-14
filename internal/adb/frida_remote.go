package adb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Reaching a frida-server that is not on frida's default port.
//
// frida's own Android backend only ever dials 27042 on the device: it opens an
// adb transport to the serial and asks for "tcp:<DEFAULT_CONTROL_PORT>". There
// is no way to tell it otherwise through frida-python's get_device(). A server
// started on any other port is therefore invisible to it — not "connection
// refused" but indistinguishable from no server at all, which surfaces to the
// user as the misleading "need Gadget to attach on jailed Android".
//
// The way around it is the one frida documents for remote targets: forward the
// device port to a host port and connect to that host:port as a remote device.
// adb allocates the host side (tcp:0) so concurrent sessions — and several
// devices — never collide on it.

// fridaForward is an adb port forward held open for the lifetime of a session.
type fridaForward struct {
	serial   string
	hostPort int
	// Address is what the driver hands to frida's add_remote_device.
	Address string
}

// ForwardFridaPort opens host->device forwarding to a frida-server listening on
// devicePort and returns the handle. Call Close when the session ends.
func (c *Client) ForwardFridaPort(ctx context.Context, serial string, devicePort int) (*fridaForward, error) {
	devicePort = fridaPortOrDefault(devicePort)
	// tcp:0 makes adb pick a free host port and print it, so two sessions (or
	// two devices) on the same device port cannot fight over one.
	out, err := c.AddForward(ctx, serial, "tcp:0", "tcp:"+strconv.Itoa(devicePort))
	if err != nil {
		return nil, fmt.Errorf("forward device port %d: %w", devicePort, err)
	}
	hostPort, err := parseForwardedPort(out)
	if err != nil {
		return nil, fmt.Errorf("forward device port %d: %w", devicePort, err)
	}
	return &fridaForward{
		serial:   serial,
		hostPort: hostPort,
		Address:  "127.0.0.1:" + strconv.Itoa(hostPort),
	}, nil
}

// Close removes the forward. Safe to call more than once.
func (f *fridaForward) Close(ctx context.Context, c *Client) {
	if f == nil || f.hostPort == 0 {
		return
	}
	_, _ = c.RemoveForward(ctx, f.serial, "tcp:"+strconv.Itoa(f.hostPort))
	f.hostPort = 0
}

// parseForwardedPort reads the host port adb allocated for `forward tcp:0`.
// adb prints it on its own line; older builds print nothing extra, newer ones
// may add a leading blank line.
func parseForwardedPort(out string) (int, error) {
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if n, err := strconv.Atoi(ln); err == nil && n > 0 && n <= 65535 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("adb did not report an allocated host port (got %q)", strings.TrimSpace(out))
}
