package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// withGuard 收紧本机 HTTP 接口的访问边界：
//   - 只接受回环 Host，挡住 DNS 重绑定；
//   - 跨站请求（Origin / Sec-Fetch-Site 不是本机）一律拒绝，挡住网页 CSRF。
func (s *Server) withGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) || !originAllowed(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "forbidden origin"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackHost 判断 Host 头（可带端口）是否指向本机回环地址。
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// originAllowed 在浏览器带上来源信息时，要求来源与 Host 同为本机。
func originAllowed(r *http.Request) bool {
	if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site == "cross-site" || site == "same-site" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true // 命令行/桌面端等非浏览器调用
	}
	if origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Host)
}
