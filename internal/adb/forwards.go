package adb

import (
	"context"
	"strings"
)

// Forward is one row in `adb forward --list` / `adb reverse --list`.
type Forward struct {
	Serial string `json:"serial"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// ListForwards returns host->device forwards across all devices, optionally
// filtered to the given serial.
func (c *Client) ListForwards(ctx context.Context, serial string) ([]Forward, error) {
	cmd, err := c.Command(ctx, "forward", "--list")
	if err != nil {
		return nil, err
	}
	out, err := Run(cmd)
	if err != nil {
		return nil, err
	}
	return parseForwardList(out, serial), nil
}

// ListReverses returns device->host reverses for one device.
func (c *Client) ListReverses(ctx context.Context, serial string) ([]Forward, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "reverse", "--list")
	if err != nil {
		return nil, err
	}
	out, err := Run(cmd)
	if err != nil {
		return nil, err
	}
	res := []Forward{}
	for _, ln := range strings.Split(out, "\n") {
		fs := strings.Fields(ln)
		if len(fs) < 3 {
			continue
		}
		// "(reverse) tcp:8081 tcp:8081" or "tcp:8081 tcp:8081"
		i := 0
		if fs[0] == "(reverse)" {
			i = 1
		}
		if len(fs) < i+2 {
			continue
		}
		res = append(res, Forward{Serial: serial, Remote: fs[i], Local: fs[i+1]})
	}
	return res, nil
}

func parseForwardList(out, serial string) []Forward {
	res := []Forward{}
	for _, ln := range strings.Split(out, "\n") {
		fs := strings.Fields(ln)
		if len(fs) < 3 {
			continue
		}
		s := fs[0]
		if serial != "" && s != serial {
			continue
		}
		res = append(res, Forward{Serial: s, Local: fs[1], Remote: fs[2]})
	}
	return res
}

// AddForward registers a new host->device forward.
func (c *Client) AddForward(ctx context.Context, serial, local, remote string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "forward", local, remote)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// RemoveForward removes a forward by local spec.
func (c *Client) RemoveForward(ctx context.Context, serial, local string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "forward", "--remove", local)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// AddReverse registers a device->host reverse.
func (c *Client) AddReverse(ctx context.Context, serial, remote, local string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "reverse", remote, local)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}

// RemoveReverse removes a reverse by remote spec.
func (c *Client) RemoveReverse(ctx context.Context, serial, remote string) (string, error) {
	cmd, err := c.DeviceCommand(ctx, serial, "reverse", "--remove", remote)
	if err != nil {
		return "", err
	}
	return Run(cmd)
}
