// Package boot 把「打开项目 → 数据库 → 源 → API → 下载器 → HTTP 监听」
// 抽成可复用启动器，CLI（cmd/reelkeep）与桌面壳（desktop）共用。
package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/api"
	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/proc"
	"github.com/OOPQ/reelkeep/internal/rule"
	"github.com/OOPQ/reelkeep/internal/source"
	websource "github.com/OOPQ/reelkeep/internal/source/web"
	"github.com/OOPQ/reelkeep/internal/store"
	"github.com/OOPQ/reelkeep/web"
)

// Options 是启动参数。
type Options struct {
	Paths     config.Paths
	Port      int
	Version   string
	NoBrowser bool
	Logf      func(format string, args ...any)
}

// Instance 是启动后的运行实例。
type Instance struct {
	URL      string
	Port     int
	Paths    config.Paths
	Store    *store.Store
	Server   *api.Server
	Download *download.Manager
	Source   *rule.Manager
	Existing bool

	http *http.Server
	ln   net.Listener
}

// ResolveProject 决定项目目录并初始化结构（与 CLI 行为一致）。
func ResolveProject(flag string) (config.Paths, error) {
	root, err := config.Resolve(flag)
	if err != nil {
		return config.Paths{}, err
	}
	paths := config.ForRoot(root)
	if err := paths.Ensure(); err != nil {
		return paths, fmt.Errorf("初始化项目失败: %w", err)
	}
	_ = config.SaveProject(root)
	return paths, nil
}

// Start 启动服务；已有实例在跑时返回 Existing=true 的实例。
func Start(ctx context.Context, opt Options) (*Instance, error) {
	logf := opt.Logf
	if logf == nil {
		logf = log.Printf
	}
	paths := opt.Paths
	warnSyncDir(paths.Root)

	port := opt.Port
	if port <= 0 {
		port = 8791
	}
	// 同一项目可能已经在相邻端口跑着（项目切换/多窗口），先扫描并复用。
	if existing, ok := findExisting(paths.Root, port); ok {
		return &Instance{
			URL: fmt.Sprintf("http://127.0.0.1:%d/", existing), Port: existing,
			Paths: paths, Existing: true,
		}, nil
	}

	st, err := store.Open(paths.Database)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	cleanup := func() { _ = st.Close() }

	settings := config.LoadSettings(paths)
	if strings.TrimSpace(settings.FFmpegPath) == "" {
		if ff := proc.FindFFmpeg(""); ff != "" {
			settings.FFmpegPath = ff
			_ = config.SaveSettings(paths, settings)
			logf("自动检测到 ffmpeg: %s", ff)
		}
	}
	reg, err := source.NewRegistry(source.DefaultConfigPath())
	if err != nil {
		logf("源清单加载失败（抓取不可用）: %v", err)
		reg = source.EmptyRegistry()
	}
	// 通用「链接抓取」源（原生实现）
	reg.Register("web", websource.New)
	reg.Register("other", websource.New)
	websource.Configure(settings.Proxy)
	mgr := rule.NewManager(config.ImportedSourcesDir(), reg)
	seedBuiltinSources()
	mgr.SetProxy(settings.Proxy)
	loadErrs := mgr.LoadAll()

	// 旧版 qise Cookie 迁移到加密凭据（只迁一次）
	if settings.QiseCookie != "" {
		if _, ok := mgr.Package("qise"); ok && !mgr.HasAuth("qise") {
			if err := mgr.SetAuth("qise", settings.QiseCookie); err != nil {
				logf("迁移 qise Cookie 失败: %v", err)
			}
		}
	}

	assets, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("前端资源缺失: %w", err)
	}
	dl := download.NewManager(paths, st, reg, mgr.Cookie, nil, nil,
		func(kind, msg string) { logf("[%s] %s", kind, msg) })
	dl.SetSettings(download.Settings{
		Concurrent: settings.Concurrent, FFmpeg: settings.FFmpegPath, Proxy: settings.Proxy,
		RetryCount: settings.RetryCount, RetryDelay: time.Duration(settings.RetryDelay) * time.Second,
		TorrentLimitKB: settings.TorrentLimitKB,
		HTTPLimitKB:    settings.HTTPLimitKB,
		TorrentBackend: settings.TorrentBackend,
		QbtURL:         settings.QbtURL, QbtUser: settings.QbtUser, QbtPass: settings.QbtPass,
	})

	srv := api.New(paths, st, reg, mgr, dl, settings, assets)
	srv.SetVersion(opt.Version)
	if err := srv.Boot(); err != nil {
		cleanup()
		return nil, fmt.Errorf("启动失败: %w", err)
	}
	dl.SetOnRow(srv.ReloadRow)

	ln, actualPort, err := listen(port)
	if err != nil {
		cleanup()
		return nil, err
	}
	srv.SetPort(actualPort)

	if err := dl.Start(context.Background()); err != nil {
		logf("下载器启动失败: %v", err)
	}

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logf("HTTP 服务退出: %v", err)
		}
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/", actualPort)
	inst := &Instance{
		URL: url, Port: actualPort, Paths: paths, Store: st, Server: srv,
		Download: dl, Source: mgr, http: httpSrv, ln: ln,
	}
	if installed := mgr.List(); len(installed) > 0 {
		srv.LogBuffer().Add("source", fmt.Sprintf("已加载 %d 个外部源包", len(installed)))
	}
	for _, e := range loadErrs {
		logf("源包加载失败: %v", e)
		srv.LogBuffer().Add("source", "源包加载失败: "+e.Error())
	}
	srv.LogBuffer().Add("boot", fmt.Sprintf("服务启动 %s（%s）", url, opt.Version))
	if !opt.NoBrowser {
		OpenBrowser(url)
	}
	return inst, nil
}

// Close 停止 HTTP、下载器与数据库。
func (i *Instance) Close() {
	if i == nil || i.Existing {
		return
	}
	if i.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = i.http.Shutdown(ctx)
		cancel()
	}
	if i.Download != nil {
		i.Download.Stop()
	}
	if i.Store != nil {
		_ = i.Store.Close()
	}
}

// SetupLog 写 项目\data\logs\console.log，超过 5MB 轮转。
func SetupLog(paths config.Paths) (*log.Logger, func()) {
	if err := os.MkdirAll(paths.Logs, 0o755); err != nil {
		return log.New(os.Stderr, "", log.LstdFlags), func() {}
	}
	p := filepath.Join(paths.Logs, "console.log")
	if info, err := os.Stat(p); err == nil && info.Size() > 5<<20 {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return log.New(os.Stderr, "", log.LstdFlags), func() {}
	}
	return log.New(io.MultiWriter(os.Stderr, f), "", log.LstdFlags), func() { _ = f.Close() }
}

// ---------- 内部工具 ----------

func warnSyncDir(root string) {
	lower := strings.ToLower(root)
	for _, bad := range []string{"onedrive", "dropbox", "google drive", "坚果云", "nutstore", "baidunetdisk", "\\\\"} {
		if strings.Contains(lower, bad) {
			fmt.Fprintln(os.Stderr, "警告: 项目目录看起来在同步盘/网络盘上，SQLite WAL 可能损坏；建议换到本地磁盘。")
			return
		}
	}
}

// findExisting 在 base..base+19 里找同一项目已运行的实例。
func findExisting(root string, base int) (int, bool) {
	for p := base; p < base+20; p++ {
		got, ok := probe(p)
		if ok && samePath(got, root) {
			return p, true
		}
	}
	return 0, false
}

// probe 判断端口上跑的是不是本程序，并返回它的项目根目录。
func probe(port int) (string, bool) {
	cli := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := cli.Get(fmt.Sprintf("http://127.0.0.1:%d/api/state", port))
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var p struct {
		OK      bool `json:"ok"`
		Project struct {
			Root string `json:"root"`
		} `json:"project"`
	}
	if json.NewDecoder(resp.Body).Decode(&p) != nil {
		return "", false
	}
	if !p.OK || p.Project.Root == "" {
		return "", false
	}
	return p.Project.Root, true
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func listen(port int) (net.Listener, int, error) {
	for p := port; p < port+20; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			return ln, p, nil
		}
		fmt.Printf("端口 %d 不可用，尝试下一个…\n", p)
	}
	return nil, 0, fmt.Errorf("%d-%d 全部端口被占用", port, port+19)
}

// OpenBrowser 用系统默认浏览器打开地址。
func OpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "打开浏览器失败:", err)
	}
}
