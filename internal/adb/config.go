package adb

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// configDir returns the host-side directory where adbq stores per-device
// state (hosts overrides, etc.).
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".adbq")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// HostsConfig is the per-device persisted hosts file we want enforced.
type HostsConfig struct {
	Serial    string `json:"serial"`
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updatedAt"`
}

func hostsPath(serial string) (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "hosts-"+sanitize(serial)+".json"), nil
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

// SaveHostsConfig persists the hosts content we last applied to the device.
func SaveHostsConfig(serial, content string) error {
	p, err := hostsPath(serial)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(HostsConfig{Serial: serial, Content: content}, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// LoadHostsConfig returns the persisted hosts content (empty when none).
func LoadHostsConfig(serial string) (string, error) {
	p, err := hostsPath(serial)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var c HostsConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return "", err
	}
	return c.Content, nil
}
