// Package netproxy 解析“设置里的代理 / 系统代理”，供抓取、封面、下载共用。
package netproxy

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// URL 返回生效的代理地址：显式设置 > 环境变量 > 系统代理（Windows 注册表）。
func URL(explicit string) string {
	if s := normalize(strings.TrimSpace(explicit)); s != "" {
		return s
	}
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if s := normalize(strings.TrimSpace(os.Getenv(k))); s != "" {
			return s
		}
	}
	return normalize(systemProxy())
}

// ProxyURL 把生效代理转成 url.URL；没有代理时返回 nil。
func ProxyURL(explicit string) (*url.URL, error) {
	raw := URL(explicit)
	if raw == "" {
		return nil, nil
	}
	return url.Parse(raw)
}

// RequestProxy 和 ProxyURL 相同，但本机/回环地址永远直连（避免本地测试、媒体服务被代理）。
func RequestProxy(explicit string, req *http.Request) (*url.URL, error) {
	if req != nil && req.URL != nil && IsLoopback(req.URL.Hostname()) {
		return nil, nil
	}
	return ProxyURL(explicit)
}

// IsLoopback 判断主机名是不是 localhost / 回环 IP。
func IsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// IsProxyAddr 判断拨号目标是不是本机/系统代理，避免 SSRF 防护误拦代理。
func IsProxyAddr(addr string, pu *url.URL) bool {
	if pu == nil || pu.Host == "" {
		return false
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ph, pp := pu.Hostname(), pu.Port()
	if pp == "" {
		if strings.EqualFold(pu.Scheme, "https") {
			pp = "443"
		} else {
			pp = "80"
		}
	}
	return strings.EqualFold(host, ph) && port == pp
}

func normalize(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "=") {
		raw = pickServer(raw)
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return raw
}

// pickServer 处理 Windows 的 "http=host:port;https=host:port" 形式。
func pickServer(raw string) string {
	m := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			m[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	for _, k := range []string{"https", "http", "socks", "socks5"} {
		if v := m[k]; v != "" {
			if k == "socks" || k == "socks5" {
				return "socks5://" + strings.TrimPrefix(strings.TrimPrefix(v, "socks5://"), "socks://")
			}
			return v
		}
	}
	return ""
}
