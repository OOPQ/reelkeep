package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// findBrowser 找本机可用的 Chrome/Edge（可用 REELKEEP_BROWSER 覆盖）。
func findBrowser() string {
	if p := strings.TrimSpace(os.Getenv("REELKEEP_BROWSER")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
	}
	for _, p := range candidates {
		if p == "" || strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "Google") || strings.HasPrefix(p, "Microsoft") {
			continue // 环境变量缺失时拼出来的相对路径，跳过
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	for _, name := range []string{"chrome", "google-chrome", "chromium", "msedge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// browserProfileDir 是某个源的持久化浏览器目录：登录态和挑战 Cookie 会留在里面。
func browserProfileDir(sourceID string) string {
	if custom := strings.TrimSpace(os.Getenv("REELKEEP_BROWSER_DIR")); custom != "" {
		dir := filepath.Join(custom, sourceID)
		_ = os.MkdirAll(dir, 0o755)
		return dir
	}
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	id := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, sourceID)
	if id == "" {
		id = "default"
	}
	dir := filepath.Join(base, "Reelkeep", "browser", id)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// browserState 是每个源共享的一个浏览器实例（带持久 profile）。
type browserState struct {
	once sync.Once

	browser *rod.Browser
	cleanup func()
	err     error
}

// ensureBrowser 启动（或复用）本源的浏览器；失败会缓存错误，避免反复重试。
func (f *HTTPFetcher) ensureBrowser() (*rod.Browser, error) {
	f.bs.once.Do(func() {
		// 远程 CDP：把外部反检测浏览器（如 Chromium 系）直接挂上来，不自己启动。
		if remote := strings.TrimSpace(os.Getenv("REELKEEP_BROWSER_CDP")); remote != "" {
			b := rod.New().ControlURL(remote)
			if err := b.Connect(); err != nil {
				f.bs.err = fmt.Errorf("连接远程浏览器失败: %w", err)
				return
			}
			f.bs.browser = b
			f.bs.cleanup = func() {}
			return
		}
		l := launcher.New().
			Headless(true).
			Leakless(true).
			UserDataDir(browserProfileDir(f.pkgID())).
			Set("disable-blink-features", "AutomationControlled").
			Set("disable-dev-shm-usage").
			Set("no-first-run").
			Set("no-default-browser-check").
			Set("disable-extensions").
			Set("disable-background-networking")
		if p := findBrowser(); p != "" {
			l = l.Bin(p)
		}
		if runtime.GOOS == "linux" && os.Geteuid() == 0 {
			l = l.NoSandbox(true)
		}
		controlURL, err := l.Launch()
		if err != nil {
			f.bs.err = fmt.Errorf("启动浏览器失败: %w", err)
			return
		}
		b := rod.New().ControlURL(controlURL)
		if err := b.Connect(); err != nil {
			_ = b.Close()
			l.Kill()
			f.bs.err = fmt.Errorf("连接浏览器失败: %w", err)
			return
		}
		f.bs.browser = b
		f.bs.cleanup = func() { l.Kill() }
	})
	return f.bs.browser, f.bs.err
}

// CloseBrowser 关闭本源浏览器（进程退出或测试用）。
func (f *HTTPFetcher) CloseBrowser() {
	if f == nil || f.bs == nil {
		return
	}
	if f.bs.browser != nil {
		_ = f.bs.browser.Close()
	}
	if f.bs.cleanup != nil {
		f.bs.cleanup()
	}
}

// Post 对第三方解析接口发 POST：优先借用真实浏览器的 TLS/指纹/Cloudflare 状态，
// 没有浏览器时退回普通 HTTP POST。
func (f *HTTPFetcher) Post(ctx context.Context, rawURL string, headers map[string]string, body string) ([]byte, error) {
	if findBrowser() != "" || strings.TrimSpace(os.Getenv("REELKEEP_BROWSER_CDP")) != "" {
		if b, err := f.postViaBrowser(ctx, rawURL, headers, body); err == nil {
			return b, nil
		} else if findBrowser() == "" {
			return nil, err
		}
	}
	return f.postHTTP(ctx, rawURL, headers, body)
}

// postViaBrowser 先打开接口所在站点（带上已有 Cookie/挑战状态），再用页面内的
// fetch 发同源 POST，最后把响应体取回来。
func (f *HTTPFetcher) postViaBrowser(ctx context.Context, rawURL string, headers map[string]string, body string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("解析接口地址不合法: %s", rawURL)
	}
	b, err := f.ensureBrowser()
	if err != nil {
		return nil, err
	}
	timeout := f.timeout
	if timeout < 40*time.Second {
		timeout = 40 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	origin := u.Scheme + "://" + u.Host + "/"
	page, err := b.Page(proto.TargetCreateTarget{URL: origin})
	if err != nil {
		return nil, fmt.Errorf("打开解析站失败: %w", err)
	}
	defer func() { _ = page.Close() }()
	page = page.Context(ctx)
	_ = page.WaitLoad()
	_ = page.WaitStable(300 * time.Millisecond)

	res, err := page.Eval(`async (url, body, hdr) => {
		const r = await fetch(url, { method: 'POST', headers: hdr, body });
		const t = await r.text();
		return JSON.stringify({ status: r.status, text: t });
	}`, rawURL, body, headers)
	if err != nil {
		return nil, fmt.Errorf("解析接口请求失败: %w", err)
	}
	var out struct {
		Status int    `json:"status"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal([]byte(res.Value.Str()), &out); err != nil {
		return nil, fmt.Errorf("解析接口返回异常: %w", err)
	}
	if out.Status >= 400 {
		return nil, fmt.Errorf("解析接口 HTTP %d: %s", out.Status, strings.TrimSpace(out.Text))
	}
	return []byte(out.Text), nil
}

// postHTTP 是浏览器不可用时的兜底。
func (f *HTTPFetcher) postHTTP(ctx context.Context, rawURL string, headers map[string]string, body string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("解析接口地址不合法: %s", rawURL)
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.ua)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("解析接口 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

// Render 用浏览器渲染页面并返回 DOM（详情页 JSON-LD 缺失时的回退）。
func (f *HTTPFetcher) Render(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("地址不合法: %s", rawURL)
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	return f.getRendered(ctx, u, headers)
}

// getRendered 用真实浏览器渲染页面后取 DOM：
// 持久 profile 保留登录/挑战 Cookie，拦截图片/字体/媒体请求以提速。
func (f *HTTPFetcher) getRendered(ctx context.Context, u *url.URL, headers map[string]string) ([]byte, error) {
	b, err := f.ensureBrowser()
	if err != nil {
		return nil, err
	}
	timeout := f.timeout
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	page, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("打开页面失败: %w", err)
	}
	defer func() { _ = page.Close() }()
	page = page.Context(ctx)

	ua := f.ua
	if v := strings.TrimSpace(headers["User-Agent"]); v != "" {
		ua = v
	}
	if ua != "" {
		_ = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua})
	}

	// 只放行文档/脚本/接口请求，图片、字体、媒体、样式都拦掉。
	router := page.HijackRequests()
	if err := router.Add("*", "", func(h *rod.Hijack) {
		switch h.Request.Type() {
		case proto.NetworkResourceTypeImage, proto.NetworkResourceTypeFont,
			proto.NetworkResourceTypeMedia, proto.NetworkResourceTypeStylesheet:
			_ = h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}
		h.ContinueRequest(&proto.FetchContinueRequest{})
	}); err != nil {
		return nil, fmt.Errorf("初始化请求拦截失败: %w", err)
	}
	go router.Run()
	defer func() { router.Stop() }()

	if err := page.Navigate(u.String()); err != nil {
		return nil, fmt.Errorf("打开 %s 失败: %w", u.String(), err)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("等待加载失败: %w", err)
	}
	// 给前端渲染/跳转留一点时间；超时不算失败。
	_ = page.WaitStable(500 * time.Millisecond)

	html, err := page.HTML()
	if err != nil {
		return nil, fmt.Errorf("读取页面失败: %w", err)
	}
	bts := []byte(html)
	if int64(len(bts)) > f.maxBytes {
		bts = bts[:f.maxBytes]
	}
	if len(bts) == 0 {
		return nil, errors.New("浏览器没有返回内容")
	}
	// 把这次浏览器会话的 Cookie 回灌给该源（登录/挑战之后，HTTP 请求也能直接用）。
	if f.cookieSink != nil {
		if cookies, cerr := page.Cookies([]string{u.String()}); cerr == nil && len(cookies) > 0 {
			parts := make([]string, 0, len(cookies))
			for _, c := range cookies {
				if c.Name != "" {
					parts = append(parts, c.Name+"="+c.Value)
				}
			}
			if len(parts) > 0 {
				f.cookieSink(strings.Join(parts, "; "))
			}
		}
	}
	return bts, nil
}
