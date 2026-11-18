package rule

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	fhttp "github.com/bogdanfinn/fhttp"
)

// 可用的 TLS 指纹名字（源包 defaults.tlsProfile）。
func profileByName(name string) (profiles.ClientProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "chrome":
		return profiles.Chrome_133, true
	case "firefox":
		return profiles.Firefox_135, true
	case "safari":
		return profiles.Safari_16_0, true
	}
	return profiles.ClientProfile{}, false
}

func (f *HTTPFetcher) tlsHTTPClient() (tlsclient.HttpClient, error) {
	prof, ok := profileByName(f.tlsProfile)
	if !ok {
		return nil, fmt.Errorf("未知的 tlsProfile %q（可选 chrome / firefox / safari）", f.tlsProfile)
	}
	proxy := ""
	if f.proxyFn != nil {
		proxy = strings.TrimSpace(f.proxyFn())
	}
	key := f.tlsProfile + "|" + proxy
	f.tlsMu.Lock()
	defer f.tlsMu.Unlock()
	if f.tlsClient != nil && f.tlsKey == key {
		return f.tlsClient.(tlsclient.HttpClient), nil
	}
	secs := int(f.timeout.Seconds()) + 5
	opts := []tlsclient.HttpClientOption{
		tlsclient.WithClientProfile(prof),
		tlsclient.WithTimeoutSeconds(secs),
		tlsclient.WithRandomTLSExtensionOrder(),
	}
	if proxy != "" {
		opts = append(opts, tlsclient.WithProxyUrl(proxy))
	}
	c, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), opts...)
	if err != nil {
		return nil, err
	}
	f.tlsClient = c
	f.tlsKey = key
	return c, nil
}

// getTLS 用浏览器指纹的 TLS 客户端抓取；目标解析仍做内网地址拦截。
func (f *HTTPFetcher) getTLS(ctx context.Context, u *url.URL, headers map[string]string) ([]byte, error) {
	client, err := f.tlsHTTPClient()
	if err != nil {
		return nil, err
	}
	proxy := ""
	if f.proxyFn != nil {
		proxy = strings.TrimSpace(f.proxyFn())
	}
	if proxy == "" {
		if err := assertPublicHost(ctx, u.Hostname()); err != nil {
			return nil, err
		}
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := fhttp.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", f.ua)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if err := f.limiter.Wait(ctx, u.Hostname()); err != nil {
			return nil, err
		}
		if f.auth != nil && f.cookieFn != nil && authHost(u.Hostname(), f.auth, f.baseHost) {
			if c := sanitizeCookie(f.cookieFn()); c != "" && req.Header.Get("Cookie") == "" {
				req.Header.Set("Cookie", c)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 400 {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
				continue
			}
			return nil, lastErr
		}
		b, rerr := readBody(resp.Body, f.maxBytes)
		_ = resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		return b, nil
	}
	return nil, lastErr
}

// assertPublicHost 拒绝解析到内网/回环地址的主机（浏览器指纹客户端的 SSRF 兜底）。
func assertPublicHost(ctx context.Context, host string) error {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("域名解析失败: %s", host)
	}
	for _, a := range addrs {
		if isPrivateIP(a.IP) {
			return fmt.Errorf("拒绝访问内网地址: %s", a.IP)
		}
	}
	return nil
}
