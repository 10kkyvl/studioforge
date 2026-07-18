//go:build !windows

package processes

import (
	"os/exec"
	"syscall"
)

func configureProcessTree(cmd *exec.Cmd)    { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func gracefulTerminate(cmd *exec.Cmd) error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
func forceKillTree(cmd *exec.Cmd) error     { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
