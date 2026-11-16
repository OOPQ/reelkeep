// Command reelkeep-desktop 是 Wails v3 桌面壳。
//
// 它复用 cmd/reelkeep 的启动器：先在本机起 HTTP 服务，再用 WebView2 窗口打开该地址。
// 业务逻辑、前端、API 与 CLI 完全同一套；壳只负责窗口与桌面集成。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/OOPQ/reelkeep/internal/boot"
	"github.com/wailsapp/wails/v3/pkg/application"
)

var version = "0.1.0-dev"

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

	// 桌面壳：深色原生窗口装饰 + 应用内项目切换
	inst.Server.SetDesktop(true)
	app := application.New(application.Options{
		Name:        "Reelkeep",
		Description: "本地媒体台账与下载管理器",
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Reelkeep — " + filepath.Base(inst.Paths.Root),
		Width:            1280,
		Height:           820,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(8, 12, 17),
		Windows: application.WindowsWindow{
			Theme: application.Dark,
		},
		URL: inst.URL,
	})

	go func() {
		<-ctx.Done()
		app.Quit()
	}()

	if err := app.Run(); err != nil {
		logf.Printf("窗口退出: %v", err)
	}
}
