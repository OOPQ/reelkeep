// Command reelkeep 是本地视频台账与下载管理器的服务端入口。
//
// 它是单进程、单二进制的形态：内置 HTTP 服务 + 嵌入的前端，
// 只监听 127.0.0.1，程序与项目数据分离。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/OOPQ/reelkeep/internal/api"
	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/rule"
	"github.com/OOPQ/reelkeep/internal/source"
	"github.com/OOPQ/reelkeep/internal/store"
	"github.com/OOPQ/reelkeep/web"
)

var version = "0.1.0-dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "source" {
		os.Exit(runSourceCommand(os.Args[2:]))
	}

	var (
		port      = flag.Int("port", 8791, "监听端口（被占用时自动向后找）")
		project   = flag.String("project", "", "项目目录（数据单元）；留空用最近项目/默认目录")
		noBrowser = flag.Bool("no-browser", false, "启动后不自动打开浏览器")
		showVer   = flag.Bool("version", false, "打印版本号后退出")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}

	root, err := config.Resolve(*project)
	if err != nil {
		fatal(err.Error())
	}
	paths := config.ForRoot(root)
	if !config.IsProject(root) {
		if *project != "" {
			fatal("不是项目目录（缺少 project.json 或 data\\）：" + root)
		}
		// 首次使用：在默认目录初始化项目
		if err := paths.Ensure(); err != nil {
			fatal("初始化项目失败: " + err.Error())
		}
		fmt.Println("已创建新项目:", root)
	}
	if err := paths.Ensure(); err != nil {
		fatal("初始化项目失败: " + err.Error())
	}
	_ = config.SaveProject(root)

	logf, closer := setupLog(paths)
	defer closer()

	// 同步盘警告：SQLite WAL 在同步盘上可能损坏（沿用旧版经验）
	warnSyncDir(root)

	// 已经有实例在跑：只打开浏览器，不碰数据库
	if alive(*port) {
		url := fmt.Sprintf("http://127.0.0.1:%d/", *port)
		fmt.Println("控制台已经在运行:", url)
		if !*noBrowser {
			openBrowser(url)
		}
		return
	}

	st, err := store.Open(paths.Database)
	if err != nil {
		fatal("打开数据库失败: " + err.Error())
	}
	defer st.Close()

	reg, err := source.NewRegistry(source.DefaultConfigPath())
	if err != nil {
		// 源清单缺失不应阻止启动：前端仍能浏览老台账
		logf.Printf("源清单加载失败（抓取不可用）: %v", err)
		reg = source.EmptyRegistry()
	}

	settings := config.LoadSettings(paths)

	// 外部源包：%APPDATA%\Reelkeep\sources\*.json
	mgr := rule.NewManager(config.ImportedSourcesDir(), reg)
	mgr.SetProxy(settings.Proxy)
	loadErrs := mgr.LoadAll()

	// 旧版把 qise Cookie 存在项目 config.json；迁移到加密凭据里（只迁一次）。
	if settings.QiseCookie != "" {
		if _, ok := mgr.Package("qise"); ok && !mgr.HasAuth("qise") {
			if err := mgr.SetAuth("qise", settings.QiseCookie); err != nil {
				logf.Printf("迁移 qise Cookie 失败: %v", err)
			} else {
				logf.Printf("已把旧配置里的 qise Cookie 迁移到加密凭据")
			}
		}
	}

	assets, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		fatal("前端资源缺失: " + err.Error())
	}

	srv := api.New(paths, st, reg, mgr, settings, assets)
	if err := srv.Boot(); err != nil {
		fatal("启动失败: " + err.Error())
	}
	if installed := mgr.List(); len(installed) > 0 {
		srv.LogBuffer().Add("source", fmt.Sprintf("已加载 %d 个外部源包", len(installed)))
	}
	for _, e := range loadErrs {
		logf.Printf("源包加载失败: %v", e)
		srv.LogBuffer().Add("source", "源包加载失败: "+e.Error())
	}

	ln, actualPort, err := listen(*port)
	if err != nil {
		fatal(err.Error())
	}
	srv.SetPort(actualPort)

	url := fmt.Sprintf("http://127.0.0.1:%d/", actualPort)
	fmt.Println("")
	fmt.Println("  Reelkeep 已启动:", url)
	fmt.Println("  项目:", paths.Root)
	fmt.Println("  台账:", paths.LedgerCSV)
	fmt.Println("  日志:", filepath.Join(paths.Logs, "console.log"))
	fmt.Println("  Ctrl+C 停止")
	fmt.Println("")

	srv.LogBuffer().Add("boot", fmt.Sprintf("服务启动 %s（%s）", url, version))
	if !*noBrowser {
		openBrowser(url)
	}

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// 不设 WriteTimeout：媒体流和 SSE 都是长连接
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Println("正在停止…")
	case err := <-errCh:
		logf.Printf("服务异常退出: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "错误:", msg)
	os.Exit(1)
}

func setupLog(paths config.Paths) (*log.Logger, func()) {
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

func warnSyncDir(root string) {
	lower := strings.ToLower(root)
	for _, bad := range []string{"onedrive", "dropbox", "google drive", "坚果云", "nutstore", "baidunetdisk", "\\\\"} {
		if strings.Contains(lower, bad) {
			fmt.Fprintln(os.Stderr, "警告: 项目目录看起来在同步盘/网络盘上，SQLite WAL 可能损坏；建议换到本地磁盘。")
			return
		}
	}
}

// alive 判断端口上跑的是不是本程序的实例（只认带 project.root 的 /api/state）。
func alive(port int) bool {
	cli := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := cli.Get(fmt.Sprintf("http://127.0.0.1:%d/api/state", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var probe struct {
		OK      bool `json:"ok"`
		Project struct {
			Root string `json:"root"`
		} `json:"project"`
	}
	if json.NewDecoder(resp.Body).Decode(&probe) != nil {
		return false
	}
	return probe.OK && probe.Project.Root != ""
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

func openBrowser(url string) {
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
