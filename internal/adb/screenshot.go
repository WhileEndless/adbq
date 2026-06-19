package adb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Screenshot captures the device screen as PNG via `adb exec-out screencap -p`
// and writes it to outDir/<timestamp>.png. Returns the saved path.
func (c *Client) Screenshot(ctx context.Context, serial, outDir string) (string, error) {
	if outDir == "" {
		outDir, _ = os.UserHomeDir()
		outDir = filepath.Join(outDir, "Pictures", "adbq")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	cmd, err := c.DeviceCommand(ctx, serial, "exec-out", "screencap", "-p")
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	name := fmt.Sprintf("adbq-%s-%s.png", serial, time.Now().Format("20060102-150405"))
	outPath := filepath.Join(outDir, name)
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return outPath, nil
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
