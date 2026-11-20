package rule

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// slowFetcher 对详情页请求人为加延迟，用来验证并发与增量跳过。
type slowFetcher struct {
	mu          sync.Mutex
	list        []byte
	delay       time.Duration
	detailCalls int
}

func (f *slowFetcher) Get(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	if strings.HasSuffix(rawURL, "/list") {
		return f.list, nil
	}
	f.mu.Lock()
	f.detailCalls++
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(f.delay):
	}
	id := path.Base(rawURL)
	return []byte(fmt.Sprintf(`<html><head><meta property="og:video" content="https://cdn.example.com/%s.mp4"></head></html>`, id)), nil
}

func demoScrapePackage(workers int) *Package {
	p := &Package{
		Schema: 1, ID: "demo", Name: "Demo", Version: "1.0.0",
		BaseURL:      "https://demo.example.com",
		Hosts:        []string{"cdn.example.com"},
		Capabilities: []string{"scrape", "detail", "download"},
		Defaults:     Defaults{DetailWorkers: workers, TimeoutSec: 10},
		List: List{
			URL: "/list", PageSize: 8, Item: "a.card",
			Fields: map[string]Field{
				"id":    {Selector: "a.card", Attr: "href", Regex: `/v/(\d+)`},
				"title": {Selector: ".t"},
			},
		},
		Detail: &Detail{
			URL: "/v/{id}", Always: true,
			Fields: map[string]Field{
				"playable": {Selector: `meta[property='og:video']`, Attr: "content"},
			},
		},
	}
	p.normalize()
	return p
}

func demoListHTML(n int) []byte {
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<a class="card" href="/v/%d"><span class="t">T%d</span></a>`, i, i)
	}
	b.WriteString("</body></html>")
	return []byte(b.String())
}

// demoListHTMLFrom 生成从 start 开始的 n 条列表项（模拟第 2、3 页）。
func demoListHTMLFrom(start, n int) []byte {
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := start; i < start+n; i++ {
		fmt.Fprintf(&b, `<a class="card" href="/v/%d"><span class="t">T%d</span></a>`, i, i)
	}
	b.WriteString("</body></html>")
	return []byte(b.String())
}

// pagedFetcher 按 page 参数返回不同列表页，用来验证自动翻页下的范围抓取。
type pagedFetcher struct {
	pages       [][]byte
	detailCalls int
}

func (f *pagedFetcher) Get(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err == nil && u.Path == "/list" {
		page := 1
		if raw := u.Query().Get("page"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				page = n
			}
		}
		if page >= 1 && page <= len(f.pages) {
			return f.pages[page-1], nil
		}
		return []byte("<html><body></body></html>"), nil
	}
	f.detailCalls++
	id := path.Base(rawURL)
	return []byte(fmt.Sprintf(`<html><head><meta property="og:video" content="https://cdn.example.com/%s.mp4"></head></html>`, id)), nil
}

// TestScrapeDetailConcurrency 验证详情页并行抓取：8 条 × 100ms 应明显快于串行。
func TestScrapeDetailConcurrency(t *testing.T) {
	const n = 8
	f := &slowFetcher{list: demoListHTML(n), delay: 100 * time.Millisecond}
	rs := New(demoScrapePackage(8), f)

	start := time.Now()
	var rows []model.Row
	err := rs.Scrape(context.Background(), source.ScrapeRequest{Pages: []int{1}, PageSize: 8}, func(r model.Row) error {
		rows = append(rows, r)
		return nil
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != n {
		t.Fatalf("收到 %d 条，期望 %d", len(rows), n)
	}
	if f.detailCalls != n {
		t.Fatalf("详情请求 %d 次，期望 %d", f.detailCalls, n)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("并发抓取耗时 %v，串行约 800ms，没有生效", elapsed)
	}
	// 顺序必须保持列表顺序
	for i, r := range rows {
		want := fmt.Sprintf("T%d", i+1)
		if r.Title != want {
			t.Fatalf("第 %d 条标题 %q，期望 %q", i, r.Title, want)
		}
	}
}

// TestScrapeSkipRange 验证范围抓取：Skip=3, Limit=2 只返回第 4、5 条。
func TestScrapeSkipRange(t *testing.T) {
	f := &slowFetcher{list: demoListHTML(10), delay: 5 * time.Millisecond}
	rs := New(demoScrapePackage(4), f)
	var titles []string
	err := rs.Scrape(context.Background(), source.ScrapeRequest{Pages: []int{1}, PageSize: 10, Skip: 3, Limit: 2}, func(r model.Row) error {
		titles = append(titles, r.Title)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"T4", "T5"}
	if len(titles) != len(want) || titles[0] != want[0] || titles[1] != want[1] {
		t.Fatalf("收到 %v，期望 %v", titles, want)
	}
}

// TestScrapeSkipRangeNoDetailForSkipped 跳过部分不请求详情，只抓取目标条目。
func TestScrapeSkipRangeNoDetailForSkipped(t *testing.T) {
	f := &slowFetcher{list: demoListHTML(10), delay: 5 * time.Millisecond}
	rs := New(demoScrapePackage(4), f)
	var titles []string
	err := rs.Scrape(context.Background(), source.ScrapeRequest{Pages: []int{1}, PageSize: 10, Skip: 8, Limit: 1}, func(r model.Row) error {
		titles = append(titles, r.Title)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0] != "T9" {
		t.Fatalf("收到 %v，期望 [T9]", titles)
	}
	if f.detailCalls != 1 {
		t.Fatalf("详情请求 %d 次，期望 1 次（跳过的条目不应请求详情）", f.detailCalls)
	}
}

func TestScrapeIncrementalSkipsDetail(t *testing.T) {
	const n = 8
	f := &slowFetcher{list: demoListHTML(n), delay: 10 * time.Millisecond}
	rs := New(demoScrapePackage(8), f)

	known := map[string]bool{}
	for i := 1; i <= n; i++ {
		known[strings.ToLower(fmt.Sprintf("demo@%d", i))] = true
	}
	req := source.ScrapeRequest{Pages: []int{1}, PageSize: 8}
	req.KnownTokens = known
	var rows []model.Row
	if err := rs.Scrape(context.Background(), req, func(r model.Row) error {
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if f.detailCalls != 0 {
		t.Fatalf("增量模式仍请求了 %d 次详情", f.detailCalls)
	}
	if len(rows) != 0 {
		t.Fatalf("列表行没有播放地址时应跳过，实际 emit %d 条", len(rows))
	}
}

// TestScrapeSkipRangeCountsKnownItems 跳过段里的已知条目也要占位置，
// 否则 skip 永远减不完，后面的目标条目会被一起跳过（范围抓取拿不到数据）。
func TestScrapeSkipRangeCountsKnownItems(t *testing.T) {
	f := &slowFetcher{list: demoListHTML(10), delay: 5 * time.Millisecond}
	rs := New(demoScrapePackage(4), f)
	known := map[string]bool{"demo@1": true, "demo@2": true, "demo@3": true}
	var titles []string
	err := rs.Scrape(context.Background(), source.ScrapeRequest{
		Pages: []int{1}, PageSize: 10, Skip: 3, Limit: 1, KnownTokens: known,
	}, func(r model.Row) error {
		titles = append(titles, r.Title)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0] != "T4" {
		t.Fatalf("收到 %v，期望 [T4]", titles)
	}
	if f.detailCalls != 1 {
		t.Fatalf("详情请求 %d 次，期望 1 次", f.detailCalls)
	}
}

// TestScrapeSkipRangeAcrossKnownPages 自动翻页时，前几页全是已知条目也要继续翻，
// 不能因为「本页没有新条目」提前停止。
func TestScrapeSkipRangeAcrossKnownPages(t *testing.T) {
	f := &pagedFetcher{pages: [][]byte{
		demoListHTMLFrom(1, 8),
		demoListHTMLFrom(9, 8),
		demoListHTMLFrom(17, 8),
	}}
	pkg := demoScrapePackage(4)
	pkg.List.URL = "/list?page={page}"
	rs := New(pkg, f)
	known := map[string]bool{}
	for i := 1; i <= 16; i++ {
		known[fmt.Sprintf("demo@%d", i)] = true
	}
	var titles []string
	err := rs.Scrape(context.Background(), source.ScrapeRequest{
		PageSize: 8, Skip: 16, Limit: 1, KnownTokens: known,
	}, func(r model.Row) error {
		titles = append(titles, r.Title)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0] != "T17" {
		t.Fatalf("收到 %v，期望 [T17]", titles)
	}
	if f.detailCalls != 1 {
		t.Fatalf("详情请求 %d 次，期望 1 次（跳过段不应请求详情）", f.detailCalls)
	}
}

// TestScrapeLimitContinuesPastKnownPage 增量模式下第一页全是最近抓过的条目时，
// 「前 N 条」仍要继续翻到后面的新条目，而不是直接结束。
func TestScrapeLimitContinuesPastKnownPage(t *testing.T) {
	f := &pagedFetcher{pages: [][]byte{
		demoListHTMLFrom(1, 8),
		demoListHTMLFrom(9, 8),
	}}
	pkg := demoScrapePackage(4)
	pkg.List.URL = "/list?page={page}"
	rs := New(pkg, f)
	known := map[string]bool{}
	for i := 1; i <= 8; i++ {
		known[fmt.Sprintf("demo@%d", i)] = true
	}
	var titles []string
	err := rs.Scrape(context.Background(), source.ScrapeRequest{
		PageSize: 8, Limit: 9, KnownTokens: known,
	}, func(r model.Row) error {
		titles = append(titles, r.Title)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0] != "T9" {
		t.Fatalf("收到 %v，期望 [T9]", titles)
	}
	if f.detailCalls != 1 {
		t.Fatalf("详情请求 %d 次，期望 1 次（已知条目不应请求详情）", f.detailCalls)
	}
}
