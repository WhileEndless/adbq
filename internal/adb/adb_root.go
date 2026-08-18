package adb

import (
	"context"
	"strings"
	"time"
)

// Restarting adbd as root via `adb root`.
//
// A stock Android emulator image is the most common test target there is, and
// adbq could not use root on one at all: the image ships a `su` that refuses
// the shell user, so every form suStyleFor probes is denied, while `adb root`
// grants full root instantly. Everything privileged — starting frida-server,
// tcpdump, iptables, the system cert store, the full process list — was dead
// there for want of one command.
//
// This is deliberately narrow. `adb root` restarts adbd, which drops every
// in-flight adb stream (logcat, pcap), so it must not fire casually:
//
//   - Only on devices that can plausibly grant it: an emulator serial, or a
//     build that advertises ro.debuggable=1. A production build refuses, and
//     asking costs a round trip for nothing.
//   - Only once per serial. The outcome is latched, so a device that refuses is
//     never asked twice and a device that accepted is not restarted again.
//   - Only from the root probe, which runs during device discovery — early,
//     before the user has long-running streams open.

type adbRootState int

const (
	adbRootUntried    adbRootState = iota
	adbRootGranted                 // adbd is running as root because we asked
	adbRootRefused                 // this build will not grant it; never ask again
	adbRootIneligible              // not an emulator / not debuggable
)

// adbRootTimeout bounds the restart. `adb root` returns as soon as adbd is
// asked; the wait-for-device that follows is what can stall on a wedged device.
const adbRootTimeout = 15 * time.Second

// tryAdbRoot restarts adbd as root when the device looks like it would allow
// it, and reports whether the adb shell now runs as uid 0. The result is
// latched per serial, so repeated calls after the first are free.
func (c *Client) tryAdbRoot(ctx context.Context, serial string) bool {
	c.suMu.Lock()
	if c.adbRooted == nil {
		c.adbRooted = map[string]adbRootState{}
	}
	if st := c.adbRooted[serial]; st != adbRootUntried {
		c.suMu.Unlock()
		return st == adbRootGranted
	}
	c.suMu.Unlock()

	if !c.adbRootEligible(ctx, serial) {
		c.setAdbRootState(serial, adbRootIneligible)
		return false
	}

	rctx, cancel := context.WithTimeout(ctx, adbRootTimeout)
	defer cancel()

	cmd, err := c.DeviceCommand(rctx, serial, "root")
	if err != nil {
		c.setAdbRootState(serial, adbRootRefused)
		return false
	}
	out, _ := Run(cmd) // a refusal is reported on stdout, not as an exit code
	if strings.Contains(strings.ToLower(out), "cannot run as root") {
		c.setAdbRootState(serial, adbRootRefused)
		return false
	}

	// `adb root` restarts adbd, so the device briefly disappears from the
	// transport list; without this wait the `id` below races the restart.
	if wait, err := c.DeviceCommand(rctx, serial, "wait-for-device"); err == nil {
		_, _ = Run(wait)
	}

	idOut, _ := c.Shell(rctx, serial, "id")
	if !hasUID0(idOut) {
		c.setAdbRootState(serial, adbRootRefused)
		return false
	}
	// Root changes what the device will tell us: getenforce, and which binaries
	// resolve on PATH. The cached scan was taken as the shell user, so drop it.
	c.InvalidateCapabilities(serial)
	c.setAdbRootState(serial, adbRootGranted)
	return true
}

// adbRootEligible reports whether asking for root is worth a round trip.
func (c *Client) adbRootEligible(ctx context.Context, serial string) bool {
	if strings.HasPrefix(serial, "emulator-") {
		return true
	}
	// A userdebug/eng build advertises this; a production build does not, and
	// its adbd would refuse. Covers a debuggable device reached over Wi-Fi,
	// whose serial carries no hint that it is an emulator.
	//
	// From the capability scan: ro.debuggable is fixed for the connection, and
	// this sits on the root-probe path that every privileged caller goes
	// through, so a getprop of its own was being paid on every cold connect.
	return c.Capabilities(ctx, serial).Debuggable
}

func (c *Client) setAdbRootState(serial string, st adbRootState) {
	c.suMu.Lock()
	if c.adbRooted == nil {
		c.adbRooted = map[string]adbRootState{}
	}
	c.adbRooted[serial] = st
	c.suMu.Unlock()
}

// ForgetRootProbe clears every cached root decision for a serial: the su style,
// the negative-probe backoff, and the `adb root` latch. Called when a device
// disconnects, since it may come back rooted, unrooted, or as a different build.
func (c *Client) ForgetRootProbe(serial string) {
	c.suMu.Lock()
	delete(c.suStyles, serial)
	delete(c.suFailedAt, serial)
	delete(c.adbRooted, serial)
	c.suMu.Unlock()
}
