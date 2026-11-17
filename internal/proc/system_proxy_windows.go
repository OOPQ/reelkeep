//go:build windows

package proc

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func systemProxy() string {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err != nil || v == 0 {
		return ""
	}
	raw, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// ProxyServer 可能是 "host:port" 或 "http=host:port;https=host:port"
	if strings.Contains(raw, "=") {
		for _, part := range strings.Split(raw, ";") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 && (strings.EqualFold(kv[0], "https") || strings.EqualFold(kv[0], "http")) {
				raw = strings.TrimSpace(kv[1])
				break
			}
		}
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return raw
}
