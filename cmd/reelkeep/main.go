// Command reelkeep 是本地视频台账与下载管理器的服务端入口。
//
// 它是单进程、单二进制的形态：内置 HTTP 服务 + 嵌入的前端，
// 只监听 127.0.0.1，程序与项目数据分离。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/OOPQ/reelkeep/internal/boot"
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

	paths, err := boot.ResolveProject(*project)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	logf, closeLog := boot.SetupLog(paths)
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	inst, err := boot.Start(ctx, boot.Options{
		Paths: paths, Port: *port, Version: version,
		NoBrowser: *noBrowser, Logf: logf.Printf,
	})
	if err != nil {
		logf.Printf("启动失败: %v", err)
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	if inst.Existing {
		fmt.Println("控制台已经在运行:", inst.URL)
		if !*noBrowser {
			boot.OpenBrowser(inst.URL)
		}
		return
	}
	defer inst.Close()

	fmt.Println("")
	fmt.Println("  Reelkeep 已启动:", inst.URL)
	fmt.Println("  项目:", inst.Paths.Root)
	fmt.Println("  台账:", inst.Paths.LedgerCSV)
	fmt.Println("  日志:", filepath.Join(inst.Paths.Logs, "console.log"))
	fmt.Println("  Ctrl+C 停止")
	fmt.Println("")

	<-ctx.Done()
	fmt.Println("正在停止…")
}
