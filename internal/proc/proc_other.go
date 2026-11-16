//go:build !windows

// Package proc 提供子进程的平台细节。
package proc

import "os/exec"

// HideWindow 在非 Windows 平台无需处理。
func HideWindow(cmd *exec.Cmd) {}
