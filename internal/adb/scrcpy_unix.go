//go:build !windows

package adb

import "syscall"

// syscallZero is signal 0; sending it tests liveness without affecting the
// process. On Windows there's no equivalent so we use a different path.
var syscallZero = syscall.Signal(0)
