//go:build !windows && !darwin

package platform

import "os/exec"

func openBrowser(url string) error { return exec.Command("xdg-open", url).Start() }
