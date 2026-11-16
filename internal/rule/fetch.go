package rule

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"

// Fetcher 是源包唯一允许的网络入口。
type Fetcher interface {
	Get(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error)
}

// HTTPFetcher 只允许访问 baseUrl 与 hosts 里声明的域名，并拦截内网地址。
type HTTPFetcher struct {
	client   *http.Client
	allowed  map[string]bool
	ua       string
	maxBytes int64
}

// NewHTTPFetcher 按源包构建抓取器。proxyFn 返回显式代理（空表示不指定）。
func NewHTTPFetcher(p *Package, proxyFn func() string) (*HTTPFetcher, error) {
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("baseUrl 不合法: %q", p.BaseURL)
	}
	allowed := map[string]bool{strings.ToLower(u.Hostname()): true}
	for _, h := range p.Hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			allowed[h] = true
		}
	}
	f := &HTTPFetcher{allowed: allowed, ua: p.Defaults.UserAgent, maxBytes: p.Defaults.MaxBytes}
	if f.ua == "" {
		f.ua = defaultUA
	}
	if f.maxBytes <= 0 {
		f.maxBytes = 8 << 20
	}
	timeout := time.Duration(p.Defaults.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// 本地代理（如 127.0.0.1:7890）本身是内网地址，必须放行；
			// 目标站点的直连仍然走 safeDial 的 SSRF 检查。
			if isProxyAddr(addr, effectiveProxy(p, proxyFn)) {
				return dialer.DialContext(ctx, network, addr)
			}
			return safeDial(ctx, dialer, network, addr)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	// 代理每个请求动态决定：UI 里改设置后不需要重建所有源。
	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		raw := ""
		if proxyFn != nil {
			raw = strings.TrimSpace(proxyFn())
		}
		if raw != "" {
			return url.Parse(raw)
		}
		if strings.EqualFold(p.Defaults.Proxy, "system") {
			return http.ProxyFromEnvironment(req)
		}
		return nil, nil
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("重定向次数过多")
			}
			return f.checkURL(req.URL)
		},
	}
	f.client = client
	return f, nil
}

// Allowed 校验一个地址能否访问（下载器解析地址时也用它）。
func (f *HTTPFetcher) Allowed(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL 不合法: %w", err)
	}
	return f.checkURL(u)
}

func (f *HTTPFetcher) checkURL(u *url.URL) error {
	if u == nil {
		return errors.New("空 URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("只允许 http(s)，拒绝 %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return errors.New("URL 缺少 host")
	}
	if !f.allowed[host] {
		return fmt.Errorf("域名不在白名单（请在 hosts 里声明）: %s", host)
	}
	return nil
}

// Get 抓取一个页面/接口。
func (f *HTTPFetcher) Get(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.ua)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > f.maxBytes {
		return nil, fmt.Errorf("响应超过 %d 字节上限", f.maxBytes)
	}
	return b, nil
}

// FixtureFetcher 用本地样本回放，供 `source test --fixture-*` 与单元测试使用。
type FixtureFetcher struct {
	ListURL string
	List    []byte
	Detail  []byte
}

// Get 按 URL 返回样本：列表页优先，其余视为详情页。
func (f *FixtureFetcher) Get(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	if f.ListURL != "" && rawURL == f.ListURL && f.List != nil {
		return f.List, nil
	}
	if f.Detail != nil {
		return f.Detail, nil
	}
	if f.List != nil {
		return f.List, nil
	}
	return nil, fmt.Errorf("fixture 没有对应响应: %s", rawURL)
}

// effectiveProxy 返回本次实际会用的代理：显式设置优先，其次是 system 的环境变量。
func effectiveProxy(p *Package, proxyFn func() string) string {
	if proxyFn != nil {
		if raw := strings.TrimSpace(proxyFn()); raw != "" {
			return raw
		}
	}
	if strings.EqualFold(p.Defaults.Proxy, "system") {
		for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				return v
			}
		}
	}
	return ""
}

// isProxyAddr 判断这次拨号是不是在连本地代理。
func isProxyAddr(addr, rawProxy string) bool {
	if rawProxy == "" {
		return false
	}
	pu, err := url.Parse(rawProxy)
	if err != nil || pu.Host == "" {
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

// ---------- 网络边界 ----------

func safeDial(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", host, err)
	}
	var chosen net.IP
	for _, ip := range ips {
		if !isPrivateIP(ip.IP) {
			chosen = ip.IP
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("拒绝访问内网地址: %s", host)
	}
	return d.DialContext(ctx, network, net.JoinHostPort(chosen.String(), port))
}

var cgnat = mustCIDR("100.64.0.0/10")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		cgnat.Contains(ip)
}
