//go:build windows

package main_test

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {
	// No-op on Windows.
}

func killProcess(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
