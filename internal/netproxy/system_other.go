//go:build !windows

package netproxy

func systemProxy() string { return "" }
