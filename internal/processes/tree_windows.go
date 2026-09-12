//go:build windows

package processes

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureProcessTree(cmd *exec.Cmd) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
	cmd.SysProcAttr = attr
}
func gracefulTerminate(cmd *exec.Cmd) error {
	return exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T").Run()
}
func forceKillTree(cmd *exec.Cmd) error {
	return exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
}
