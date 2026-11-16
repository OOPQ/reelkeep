//go:build windows

// Package proc 提供子进程的 Windows 细节。
package proc

import (
	"os/exec"
	"syscall"
)

// HideWindow 让子进程不创建/闪烁控制台窗口（ffmpeg 等 console 程序）。
func HideWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
