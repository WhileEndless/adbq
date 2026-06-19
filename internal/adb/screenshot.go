package adb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pngMagic is the 8-byte PNG file signature.
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// Screenshot captures the device screen as PNG and writes it to
// outDir/<timestamp>.png. Returns the saved path.
func (c *Client) Screenshot(ctx context.Context, serial, outDir string) (string, error) {
	if outDir == "" {
		outDir, _ = os.UserHomeDir()
		outDir = filepath.Join(outDir, "Pictures", "adbq")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	png, err := c.captureScreencap(ctx, serial)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("adbq-%s-%s.png", serial, time.Now().Format("20060102-150405"))
	outPath := filepath.Join(outDir, name)
	if err := os.WriteFile(outPath, png, 0o644); err != nil {
		return "", err
	}
	return outPath, nil
}

// captureScreencap returns a validated PNG of the device screen. It prefers
// `exec-out screencap -p` (clean binary stream, device API 23+); if that yields
// no valid PNG it retries `shell screencap -p` and un-mangles the shell
// transport's LF→CRLF translation, which is the standard fix for old adb /
// Android ≤5.1. The PNG signature is checked so an empty or corrupt capture is
// a real error instead of a 0-byte file written as "success".
func (c *Client) captureScreencap(ctx context.Context, serial string) ([]byte, error) {
	out, stderr1, err1 := c.runScreencap(ctx, serial, "exec-out")
	if err1 == nil && isPNG(out) {
		return out, nil
	}
	out2, stderr2, err2 := c.runScreencap(ctx, serial, "shell")
	if err2 == nil {
		if fixed := bytes.ReplaceAll(out2, []byte("\r\n"), []byte("\n")); isPNG(fixed) {
			return fixed, nil
		}
		if isPNG(out2) { // some adb builds don't mangle
			return out2, nil
		}
	}
	for _, msg := range []string{strings.TrimSpace(stderr1), strings.TrimSpace(stderr2)} {
		if msg != "" {
			return nil, fmt.Errorf("screenshot failed: %s", firstLine(msg))
		}
	}
	return nil, fmt.Errorf("screenshot failed: screencap returned no valid PNG (device may block screen capture)")
}

// runScreencap runs `adb <mode> screencap -p`, capturing stdout and stderr
// separately. mode is "exec-out" (binary-safe) or "shell" (fallback).
func (c *Client) runScreencap(ctx context.Context, serial, mode string) (stdout []byte, stderr string, err error) {
	cmd, cerr := c.DeviceCommand(ctx, serial, mode, "screencap", "-p")
	if cerr != nil {
		return nil, "", cerr
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.String(), err
}

// isPNG reports whether b begins with the PNG signature and carries more than
// just the header.
func isPNG(b []byte) bool {
	return len(b) > len(pngMagic) && bytes.Equal(b[:len(pngMagic)], pngMagic)
}

// Stats reports a snapshot of CPU/RAM/battery/storage/uptime.
type Stats struct {
	BatteryLevel   int     `json:"batteryLevel"`
	BatteryTemp    float64 `json:"batteryTemp"`
	BatteryVoltage int     `json:"batteryVoltage"` // mV
	Charging       bool    `json:"charging"`
	MemTotalKB     int64   `json:"memTotalKb"`
	MemAvailKB     int64   `json:"memAvailKb"`
	CPUPercent     float64 `json:"cpuPercent"` // 0..100 from `top` idle delta
	LoadAvg1       float64 `json:"loadAvg1"`   // may be 0 if /proc/loadavg + uptime both denied
	StorageTotalKB int64   `json:"storageTotalKb"`
	StorageFreeKB  int64   `json:"storageFreeKb"`
	UptimeSeconds  int64   `json:"uptimeSeconds"`
	NetRxBytes     int64   `json:"netRxBytes"`
	NetTxBytes     int64   `json:"netTxBytes"`
}
