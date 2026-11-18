package rule

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	// 非 Windows 或装在 PATH 里的情况
	for _, name := range []string{"chrome", "google-chrome", "chromium", "msedge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// getRendered 用无头浏览器渲染页面后取 DOM（适合纯 JS 站点 / 挑战页）。
// 注意：浏览器模式不发送源包的自定义头与登录 Cookie。
func (f *HTTPFetcher) getRendered(ctx context.Context, u *url.URL, headers map[string]string) ([]byte, error) {
	bin := findBrowser()
	if bin == "" {
		return nil, errors.New("没有找到 Chrome/Edge，浏览器模式不可用（可设置 REELKEEP_BROWSER）")
	}
	dir, err := os.MkdirTemp("", "reelkeep-render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	timeout := f.timeout
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ua := f.ua
	if v := strings.TrimSpace(headers["User-Agent"]); v != "" {
		ua = v
	}
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-background-networking",
		"--virtual-time-budget=8000",
		"--user-data-dir=" + dir,
	}
	if ua != "" {
		args = append(args, "--user-agent="+ua)
	}
	if os.Getenv("REELKEEP_BROWSER_NO_SANDBOX") == "1" || (runtime.GOOS == "linux" && os.Geteuid() == 0) {
		args = append(args, "--no-sandbox")
	}
	args = append(args, "--dump-dom", u.String())

	var out, errBuf bytes.Buffer
	run := func(extra []string) ([]byte, string, error) {
		out.Reset()
		errBuf.Reset()
		full := append(append([]string{}, args...), extra...)
		cmd := exec.CommandContext(cctx, bin, full...)
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		return out.Bytes(), strings.TrimSpace(errBuf.String()), err
	}
	b, stderr, err := run(nil)
	if err != nil && strings.Contains(strings.ToLower(stderr), "sandbox") {
		// 容器 / CI 里没有可用的内核沙箱，退一步再试一次。
		b, stderr, err = run([]string{"--no-sandbox"})
	}
	if err != nil {
		if stderr != "" {
			return nil, fmt.Errorf("浏览器渲染失败: %v（%s）", err, firstLine(stderr))
		}
		return nil, fmt.Errorf("浏览器渲染失败: %v", err)
	}
	if len(b) == 0 {
		return nil, errors.New("浏览器没有返回内容")
	}
	if int64(len(b)) > f.maxBytes {
		b = b[:f.maxBytes]
	}
	return b, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}
