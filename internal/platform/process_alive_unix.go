//go:build !windows

package platform

import "syscall"

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
