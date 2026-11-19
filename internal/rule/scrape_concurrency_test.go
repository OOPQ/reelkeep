package rule

import (
	"context"
	"fmt"
	"path"
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
