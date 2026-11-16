//go:build !windows

package main

// focusExistingWindow 在非 Windows 平台不做原生聚焦，交给打开浏览器兜底。
func focusExistingWindow(project string) bool { return false }
