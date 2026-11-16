// Command reelkeep-desktop 是 Wails v3 桌面壳。
//
// 它复用 cmd/reelkeep 的启动器：先在本机起 HTTP 服务，再用 WebView2 窗口打开该地址。
// 业务逻辑、前端、API 与 CLI 完全同一套；壳只负责窗口与桌面集成。
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/OOPQ/reelkeep/internal/boot"
	"github.com/OOPQ/reelkeep/internal/desktoplogin"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed icon.png
var iconPNG []byte

var (
	version = "0.1.0-dev"
	cdpPort int
)

func main() {
	project := flag.String("project", "", "项目目录（数据单元）")
	flag.Parse()

	paths, err := boot.ResolveProject(*project)
	if err != nil {
		log.Fatal(err)
	}
	logf, closeLog := boot.SetupLog(paths)
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	inst, err := boot.Start(ctx, boot.Options{
		Paths: paths, Port: 8791, Version: version,
		NoBrowser: true, Logf: logf.Printf,
	})
	if err != nil {
		logf.Printf("启动失败: %v", err)
		return
	}
	// 已有实例：窗口直接指向它，不重复启动服务，也不在退出时关掉别人的服务
	if !inst.Existing {
		defer inst.Close()
	}

	if inst.Existing {
		// 同一项目已经有进程在跑：聚焦它的窗口；找不到（例如 CLI 起的服务）就开浏览器。
		// 不能在这里创建第二个 Wails 窗口：Existing 实例没有 Server/回调。
		if !focusExistingWindow(filepath.Base(inst.Paths.Root)) {
			boot.OpenBrowser(inst.URL)
		}
		return
	}

	// 桌面壳：深色原生窗口装饰 + 应用内项目切换
	inst.Server.SetDesktop(true)
	cdpPort = freePort()
	// WebView2 的 user data 目录不能跨进程共用（否则第二个进程创建
	// Controller 会报 8007139f 并崩溃）。每个进程用独立目录；登录凭据本身
	// 存在 secrets 里，不依赖 WebView2 的持久会话。
	webviewData := filepath.Join(os.TempDir(), "reelkeep-webview2", fmt.Sprintf("%d", os.Getpid()))
	app := application.New(application.Options{
		Name:        "Reelkeep",
		Description: "本地媒体台账与下载管理器",
		Icon:        iconPNG,
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewData,
			AdditionalBrowserArgs: []string{
				fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
				"--remote-allow-origins=*",
			},
		},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Reelkeep — " + filepath.Base(inst.Paths.Root),
		Width:            1280,
		Height:           820,
		MinWidth:         960,
		MinHeight:        600,
		Frameless:        true,
		BackgroundColour: application.NewRGB(8, 12, 17),
		Windows: application.WindowsWindow{
			Theme:                  application.Dark,
			NonClientRegionSupport: true,
		},
		URL: inst.URL,
	})
	inst.Server.SetWindowController(winController{window})
	inst.Server.SetFolderPicker(func() (string, error) {
		return app.Dialog.OpenFile().
			CanChooseDirectories(true).
			CanChooseFiles(false).
			CanCreateDirectories(true).
			SetTitle("打开或新建项目文件夹").
			AttachToWindow(window).
			PromptForSingleSelection()
	})
	domainsFor := func(id string) []string {
		if pkg, ok := inst.Source.Package(id); ok && pkg.Auth != nil {
			return pkg.Auth.Domains
		}
		return nil
	}
	inst.Server.SetLoginCapture(func(id string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return desktoplogin.Capture(ctx, cdpPort, domainsFor(id))
	})
	inst.Server.SetLoginWindow(func(id, loginURL string) error {
		loginWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            "登录 " + id + "（登录后关闭窗口自动保存）",
			Width:            980,
			Height:           720,
			MinWidth:         800,
			MinHeight:        560,
			BackgroundColour: application.NewRGB(8, 12, 17),
			Windows: application.WindowsWindow{
				Theme: application.Dark,
			},
			URL: loginURL,
		})
		loginWin.Show()
		// 关闭登录窗口时用 CDP 读 Cookie（含 HttpOnly）并保存
		loginWin.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			// 已有凭据时不自动覆盖：避免用户只是打开看看、没登录就关窗，把有效 Cookie 换成访客 Cookie
			if inst.Source.Cookie(id) != "" {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			cookie, err := desktoplogin.Capture(ctx, cdpPort, domainsFor(id))
			cancel()
			if err != nil || cookie == "" {
				return
			}
			if err := inst.Source.SetAuth(id, cookie); err == nil {
				inst.Server.LogBuffer().Add("auth", "登录窗口关闭，已保存 "+id+" 的 Cookie")
			}
		})
		return nil
	})

	go func() {
		<-ctx.Done()
		app.Quit()
	}()

	if err := app.Run(); err != nil {
		logf.Printf("窗口退出: %v", err)
	}
}

// winController 适配 Wails 窗口（Minimise 返回 Window，接口要求无返回值）。
type winController struct{ w *application.WebviewWindow }

func (c winController) Minimise()       { c.w.Minimise() }
func (c winController) ToggleMaximise() { c.w.ToggleMaximise() }
func (c winController) Close()          { c.w.Close() }
func (c winController) IsMaximised() bool {
	return c.w.IsMaximised()
}

func freePort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
