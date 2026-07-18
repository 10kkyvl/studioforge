//go:build windows

package platform

import (
	"os/exec"
	"strconv"
	"strings"
)

func processAlive(pid int) bool {
	out, err := exec.Command("tasklist.exe", "/FI", "PID eq "+strconv.Itoa(pid), "/NH", "/FO", "CSV").Output()
	return err == nil && strings.Contains(string(out), `"`+strconv.Itoa(pid)+`"`)
}
