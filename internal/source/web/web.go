// Package web 是通用的「任意视频页面/直链」源，只支持链接抓取。
package web

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/netproxy"
	"github.com/OOPQ/reelkeep/internal/source"
)

// Adapter 实现 source.Source + URLScraper + DownloadResolver。
type Adapter struct{}

// Configure 设置抓取代理（来自应用设置）。
func Configure(proxy string) {
	proxyMu.Lock()
	proxyAddr = strings.TrimSpace(proxy)
	proxyMu.Unlock()
}

var (
	proxyMu   sync.RWMutex
	proxyAddr string
)

// New 供注册表使用。
func New() source.Source { return &Adapter{} }

// ID 返回源标识。
func (a *Adapter) ID() string { return "web" }

// Capabilities 返回能力。
func (a *Adapter) Capabilities() []string { return []string{"scrape", "download"} }

// Scrape 不支持榜单抓取：web 源必须给 URL。
func (a *Adapter) Scrape(ctx context.Context, req source.ScrapeRequest, emit func(model.Row) error) error {
	return fmt.Errorf("web 源只支持「链接抓取」：请粘贴视频页面或直链")
}

// HealthCheck 无固定站点。
func (a *Adapter) HealthCheck(ctx context.Context) error { return nil }

var mediaRe = regexp.MustCompile(`https?://[^"'\s<>]+\.(?:m3u8|mp4)(?:\?[^"'\s<>]*)?`)
var ldContentRe = regexp.MustCompile(`"contentUrl"\s*:\s*"([^"]+)"`)

// fetchPage 可在测试里替换。
var fetchPage = fetchReal

// ScrapeURL 解析一个页面或直链。
func (a *Adapter) ScrapeURL(ctx context.Context, rawURL string) (model.Row, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return model.Row{}, fmt.Errorf("链接不合法: %s", rawURL)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	id := shortHash(rawURL)
	token := "web@" + id

	if isMediaURL(rawURL) {
		return buildRow(token, id, directTitle(rawURL), rawURL, "", rawURL, now, "", ""), nil
	}

	body, err := fetchPage(ctx, rawURL)
	if err != nil {
		return model.Row{}, fmt.Errorf("页面请求失败: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return model.Row{}, err
	}

	title := firstMeta(doc, "meta[property='og:title']", "meta[name='twitter:title']")
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").First().Text())
	}
	cover := firstMeta(doc, "meta[property='og:image']", "meta[name='twitter:image']")
	cover = resolveURL(u, cover)

	media := firstMeta(doc, "meta[property='og:video']", "meta[property='og:video:url']", "meta[name='twitter:player:stream']")
	if media == "" {
		if s := doc.Find("video[src]").First(); s.Length() > 0 {
			media, _ = s.Attr("src")
		}
	}
	if media == "" {
		doc.Find("video source[src], source[src]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			v, _ := s.Attr("src")
			if v != "" {
				media = v
				return false
			}
			return true
		})
	}
	if media == "" {
		if m := ldContentRe.FindSubmatch(body); len(m) > 1 {
			media = html.UnescapeString(string(m[1]))
		}
	}
	if media == "" {
		if m := mediaRe.Find(body); len(m) > 0 {
			media = string(m)
		}
	}
	if media == "" {
		return model.Row{}, fmt.Errorf("页面里没有找到 m3u8/mp4: %s", rawURL)
	}
	media = html.UnescapeString(resolveURL(u, strings.TrimSpace(media)))

	duration, seconds := "", ""
	if v := firstMeta(doc, "meta[property='video:duration']"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			seconds = strconv.Itoa(n)
			duration = formatDuration(n)
		}
	}
	if title == "" {
		title = rawURL
	}
	return buildRow(token, id, title, rawURL, cover, media, now, duration, seconds), nil
}

// 兼容不同参数数量的构建（Go 不支持重载，这里用变参）。
func buildRow(token, id, title, rawURL, cover, media, now, duration, seconds string) model.Row {
	kind := "http"
	if isMediaURL(media) && strings.Contains(strings.ToLower(media), ".m3u8") {
		kind = "hls"
	}
	u, _ := url.Parse(rawURL)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	headers, _ := json.Marshal(map[string]string{"Referer": u.Scheme + "://" + u.Host + "/"})
	return model.Row{
		Token: token, Status: "new", Source: "web", SourceID: id,
		Site: host, Title: title, URL: media, PageURL: rawURL, CoverURL: cover,
		Duration: duration, Seconds: seconds, DownloadKind: kind,
		DownloadHeaders: string(headers), FirstSeen: now, LastSeen: now,
	}
}

// Resolve 返回已解析的下载地址。
func (a *Adapter) Resolve(ctx context.Context, r model.Row) (source.DownloadSpec, error) {
	spec := source.DownloadSpec{URL: r.URL, Mode: r.DownloadKind, Headers: map[string]string{}}
	if spec.Mode == "" {
		spec.Mode = "http"
	}
	if r.DownloadHeaders != "" {
		_ = json.Unmarshal([]byte(r.DownloadHeaders), &spec.Headers)
	}
	if spec.URL == "" {
		return spec, fmt.Errorf("没有播放地址")
	}
	return spec, nil
}

// ---------- 解析小工具 ----------

func shortHash(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

func isMediaURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4")
}

func directTitle(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
		return base
	}
	return rawURL
}

func firstMeta(doc *goquery.Document, selectors ...string) string {
	for _, sel := range selectors {
		s := doc.Find(sel).First()
		if s.Length() == 0 {
			continue
		}
		if v, ok := s.Attr("content"); ok {
			if v = strings.TrimSpace(html.UnescapeString(v)); v != "" {
				return v
			}
		}
	}
	return ""
}

func resolveURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || base == nil {
		return ref
	}
	if strings.HasPrefix(ref, "//") {
		ref = base.Scheme + ":" + ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

func formatDuration(total int) string {
	if total <= 0 {
		return ""
	}
	if total >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", total/3600, total%3600/60, total%60)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

// ---------- 网络 ----------

func fetchReal(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36")
	tr := &http.Transport{
		DialContext:           safeDial,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		proxyMu.RLock()
		p := proxyAddr
		proxyMu.RUnlock()
		return netproxy.RequestProxy(p, req)
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return readUntilHTML(resp.Body, 8<<20)
}

func readUntilHTML(r io.Reader, max int64) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 32*1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if int64(buf.Len()) > max {
				return nil, fmt.Errorf("响应超过 %d 字节", max)
			}
			if buf.Len() > 2048 {
				b := buf.Bytes()
				tail := b
				if len(tail) > 4096 {
					tail = tail[len(tail)-4096:]
				}
				if bytes.Contains(bytes.ToLower(tail), []byte("</html>")) {
					return buf.Bytes(), nil
				}
			}
		}
		if err == io.EOF {
			return buf.Bytes(), nil
		}
		if err != nil {
			return buf.Bytes(), err
		}
	}
}

func safeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	proxyMu.RLock()
	p := proxyAddr
	proxyMu.RUnlock()
	if pu, err := netproxy.ProxyURL(p); err == nil && pu != nil && netproxy.IsProxyAddr(addr, pu) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("解析失败: %s", host)
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for pass := 0; pass < 2; pass++ {
		for _, ip := range ips {
			if ip.IP == nil || ip.IP.IsLoopback() || ip.IP.IsPrivate() || ip.IP.IsLinkLocalUnicast() ||
				ip.IP.IsLinkLocalMulticast() || ip.IP.IsMulticast() || ip.IP.IsUnspecified() {
				continue
			}
			if pass == 0 && ip.IP.To4() == nil {
				continue
			}
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("拒绝访问内网地址: %s", host)
	}
	return nil, lastErr
}
