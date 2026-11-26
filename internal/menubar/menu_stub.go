//go:build !darwin || !cgo || (!dev && !production)

package menubar

const Supported = false

func Mount(func() []Item) func() { return func() {} }
