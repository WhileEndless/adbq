//go:build !darwin || !cgo || (!dev && !production)

package menubar

import (
	"context"
	"errors"
)

const Supported = false

func Mount(func() []Item) func() { return func() {} }

func ShowDialog(context.Context, Dialog) (bool, error) {
	return false, errors.New("menu bar popups are unavailable")
}
