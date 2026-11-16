//go:build windows

package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// focusExistingWindow 按窗口标题 “Reelkeep — <项目名>” 找到并聚焦已有窗口。
func focusExistingWindow(project string) bool {
	if project == "" {
		return false
	}
	found := false
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		buf := make([]uint16, 256)
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return 1
		}
		title := windows.UTF16ToString(buf[:n])
		if strings.HasSuffix(title, "— "+project) || strings.HasSuffix(title, "- "+project) {
			_, _, _ = procShowWindow.Call(hwnd, 9) // SW_RESTORE
			_, _, _ = procSetForegroundWindow.Call(hwnd)
			found = true
			return 0
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return found
}
