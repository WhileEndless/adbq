//go:build windows

package adb

import "os"

// On Windows we can't send signal 0 portably; use a no-op that always reports
// "alive" — the goroutine in Start() will fix the map when the process exits.
var syscallZero os.Signal = nil
