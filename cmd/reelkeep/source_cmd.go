package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/rule"
	"github.com/OOPQ/reelkeep/internal/source"
)

// runSourceCommand 处理 `reelkeep source ...` 子命令。
func runSourceCommand(args []string) int {
	if len(args) == 0 {
		sourceUsage()
		return 2
	}
	switch args[0] {
	case "validate":
		return cmdSourceValidate(args[1:])
	case "test":
		return cmdSourceTest(args[1:])
	case "install":
		return cmdSourceInstall(args[1:])
	case "list":
		return cmdSourceList(args[1:])
	case "remove":
		return cmdSourceRemove(args[1:])
	case "export":
		return cmdSourceExport(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", args[0])
		sourceUsage()
		return 2
	}
}

func sourceUsage() {
	fmt.Fprint(os.Stderr, `用法:
  reelkeep source validate <file.json>   校验源包
  reelkeep source test <file.json>       跑一次抓取（需要网络）
      --rank latest --pages 1 --limit 5 --proxy http://127.0.0.1:7890
      --fixture-list list.html --fixture-detail detail.html   离线回放
      --resolve --json
      --url <page-url> --resolve                              链接抓取（单条）
  reelkeep source install <file.json>    安装到 %APPDATA%\Reelkeep\sources
      --overwrite
  reelkeep source list                   列出已安装的源
  reelkeep source remove <id>            删除一个源
  reelkeep source export <id>            导出源包 JSON
`)
}

// openManager 打开外部源管理器（安装目录固定为 %APPDATA%\Reelkeep\sources）。
func openManager() *rule.Manager {
	reg, err := source.NewRegistry(source.DefaultConfigPath())
	if err != nil {
		reg = source.EmptyRegistry()
	}
	m := rule.NewManager(config.ImportedSourcesDir(), reg)
	for _, e := range m.LoadAll() {
		fmt.Fprintln(os.Stderr, "警告:", e)
	}
	return m
}

func cmdSourceInstall(args []string) int {
	if len(args) < 1 {
		sourceUsage()
		return 2
	}
	file := args[0]
	fs := flag.NewFlagSet("source install", flag.ContinueOnError)
	overwrite := fs.Bool("overwrite", false, "覆盖同 id 的已安装源")
	proxy := fs.String("proxy", "", "显式代理")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	m := openManager()
	m.SetProxy(*proxy)
	p, err := m.Import(data, *overwrite)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ 安装失败:", err)
		return 1
	}
	fmt.Printf("✓ 已安装 %s (%s) v%s → %s\n", p.Name, p.ID, p.Version, config.ImportedSourcesDir())
	return 0
}

func cmdSourceList(args []string) int {
	m := openManager()
	items := m.List()
	if len(items) == 0 {
		fmt.Println("（还没有安装外部源）")
		return 0
	}
	for _, it := range items {
		state := "启用"
		if !it.Enabled {
			state = "禁用"
		}
		line := fmt.Sprintf("%-16s %-20s v%-8s %s  %s", it.ID, it.Name, it.Version, state, strings.Join(it.Capabilities, ","))
		if it.LastRunAt != "" {
			line += "  最近:" + it.LastRunAt
			if it.LastError != "" {
				line += " 错误:" + truncate(it.LastError, 60)
			}
		}
		fmt.Println(line)
	}
	return 0
}

func cmdSourceRemove(args []string) int {
	if len(args) != 1 {
		sourceUsage()
		return 2
	}
	m := openManager()
	if err := m.Remove(args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "删除失败:", err)
		return 1
	}
	fmt.Println("✓ 已删除", args[0])
	return 0
}

func cmdSourceExport(args []string) int {
	if len(args) != 1 {
		sourceUsage()
		return 2
	}
	m := openManager()
	data, err := m.Export(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "导出失败:", err)
		return 1
	}
	_, _ = os.Stdout.Write(data)
	return 0
}

func cmdSourceValidate(args []string) int {
	if len(args) != 1 {
		sourceUsage()
		return 2
	}
	p, err := rule.LoadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}
	hosts := []string{}
	if u, err := url.Parse(p.BaseURL); err == nil {
		hosts = append(hosts, u.Hostname())
	}
	hosts = append(hosts, p.Hosts...)
	fmt.Printf("✓ %s (%s) v%s\n", p.Name, p.ID, p.Version)
	fmt.Printf("  base        %s\n", p.BaseURL)
	fmt.Printf("  hosts       %s\n", strings.Join(hosts, ", "))
	fmt.Printf("  capabilities %s\n", strings.Join(p.Capabilities, ", "))
	fmt.Printf("  list        %d 个字段，%d 个榜单\n", len(p.List.Fields), len(p.List.Ranks))
	if p.Detail != nil {
		fmt.Printf("  detail      %d 个字段（always=%v）\n", len(p.Detail.Fields), p.Detail.Always)
	} else {
		fmt.Println("  detail      无")
	}
	if p.Download != nil {
		fmt.Printf("  download    %s\n", p.Download.Kind)
	} else {
		fmt.Println("  download    无")
	}
	return 0
}

type testRow struct {
	Token    string `json:"token"`
	Title    string `json:"title"`
	Playable string `json:"playable"`
	Cover    string `json:"cover"`
	Duration string `json:"duration"`
	Res      string `json:"res"`
	PageURL  string `json:"pageUrl"`
	Kind     string `json:"kind"`
}

func cmdSourceTest(args []string) int {
	if len(args) < 1 {
		sourceUsage()
		return 2
	}
	file := args[0]
	fs := flag.NewFlagSet("source test", flag.ContinueOnError)
	rank := fs.String("rank", "", "榜单 key，默认第一个")
	pages := fs.String("pages", "1", "页码，逗号分隔")
	limit := fs.Int("limit", 5, "最多输出多少条")
	fixtureList := fs.String("fixture-list", "", "用本地 HTML 代替列表页")
	fixtureDetail := fs.String("fixture-detail", "", "用本地 HTML 代替详情页")
	asJSON := fs.Bool("json", false, "输出 JSON")
	doResolve := fs.Bool("resolve", false, "同时测试下载地址解析")
	proxy := fs.String("proxy", "", "显式代理，例如 http://127.0.0.1:7890")
	targetURL := fs.String("url", "", "链接抓取：直接解析这个详情页 URL")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	p, err := rule.LoadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}

	pageNums := []int{}
	for _, s := range strings.Split(*pages, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
			pageNums = append(pageNums, n)
		}
	}
	if len(pageNums) == 0 {
		pageNums = []int{1}
	}

	var fetcher rule.Fetcher
	if *fixtureList != "" {
		listHTML, err := os.ReadFile(*fixtureList)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取 fixture 失败:", err)
			return 1
		}
		ff := &rule.FixtureFetcher{ListURL: p.ListURL(*rank, pageNums[0]), List: listHTML}
		if *fixtureDetail != "" {
			ff.Detail, err = os.ReadFile(*fixtureDetail)
			if err != nil {
				fmt.Fprintln(os.Stderr, "读取 detail fixture 失败:", err)
				return 1
			}
		}
		fetcher = ff
		fmt.Fprintln(os.Stderr, "（离线回放模式）")
	} else {
		hf, err := rule.NewHTTPFetcher(p, func() string { return *proxy })
		if err != nil {
			fmt.Fprintln(os.Stderr, "创建抓取器失败:", err)
			return 1
		}
		fetcher = hf
	}

	rs := rule.New(p, fetcher)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var rows []model.Row
	start := time.Now()
	if *targetURL != "" {
		row, err := rs.ScrapeURL(ctx, *targetURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 链接抓取失败:", err)
			return 1
		}
		rows = append(rows, row)
	} else {
		err = rs.Scrape(ctx, source.ScrapeRequest{
			Rank:     *rank,
			Pages:    pageNums,
			Limit:    *limit,
			PageSize: max(*limit, p.List.PageSize),
		}, func(r model.Row) error {
			rows = append(rows, r)
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 抓取失败:", err)
			return 1
		}
	}
	elapsed := time.Since(start)

	out := make([]testRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, testRow{
			Token: r.Token, Title: r.Title, Playable: r.URL,
			Cover: r.CoverURL, Duration: r.Duration, Res: r.Res,
			PageURL: r.PageURL, Kind: r.DownloadKind,
		})
	}
	if *asJSON {
		payload := map[string]any{
			"ok": true, "id": p.ID, "rank": *rank, "pages": pageNums,
			"count": len(out), "elapsedMs": elapsed.Milliseconds(), "rows": out,
		}
		if *doResolve {
			specs := make([]map[string]any, 0, len(rows))
			for _, r := range rows {
				spec, err := rs.Resolve(ctx, r)
				item := map[string]any{"token": r.Token}
				if err != nil {
					item["error"] = err.Error()
				} else {
					item["url"] = spec.URL
					item["mode"] = spec.Mode
					item["headers"] = spec.Headers
					item["refreshOn410"] = spec.RefreshOn410
				}
				specs = append(specs, item)
			}
			payload["resolved"] = specs
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(b))
		return 0
	}

	fmt.Printf("✓ %s 抓取 %d 条（%d ms）\n", p.ID, len(out), elapsed.Milliseconds())
	for i, r := range out {
		title := r.Title
		if len(title) > 40 {
			title = title[:40] + "…"
		}
		fmt.Printf("%2d. %-28s %-6s %s\n", i+1, r.Token, r.Duration, title)
		fmt.Printf("    播放 %s\n", truncate(r.Playable, 110))
		if r.Cover != "" {
			fmt.Printf("    封面 %s\n", truncate(r.Cover, 110))
		}
	}
	if *doResolve {
		fmt.Println("\n下载地址解析：")
		for _, r := range rows {
			spec, err := rs.Resolve(ctx, r)
			if err != nil {
				fmt.Printf("  %s  ✗ %v\n", r.Token, err)
				continue
			}
			fmt.Printf("  %s  %s  headers=%d  refreshOn410=%v\n", r.Token, spec.Mode, len(spec.Headers), spec.RefreshOn410)
			fmt.Printf("    %s\n", truncate(spec.URL, 110))
		}
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
