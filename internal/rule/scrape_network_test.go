package rule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// netFetcher 用来模拟列表页/详情页的网络异常与反爬拦截。
type netFetcher struct {
	list      []byte
	failList  bool
	detailErr error
}

func (f *netFetcher) Get(_ context.Context, rawURL string, _ map[string]string) ([]byte, error) {
	if strings.HasSuffix(rawURL, "/list") {
		if f.failList {
			return nil, fmt.Errorf("%w: dial tcp 10.0.0.1: i/o timeout", ErrNetwork)
		}
		return f.list, nil
	}
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	return []byte(`<html><head><meta property="og:video" content="https://cdn.example.com/v.mp4"></head></html>`), nil
}

func scrapeNoop(_ model.Row) error { return nil }

func TestScrapeAbortsOnListNetworkError(t *testing.T) {
	rs := New(demoScrapePackage(4), &netFetcher{failList: true})
	err := rs.Scrape(context.Background(), source.ScrapeRequest{Pages: []int{1}, PageSize: 8}, scrapeNoop)
	if err == nil {
		t.Fatal("列表页网络异常时应返回错误")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("错误应包装 ErrNetwork: %v", err)
	}
	if !strings.Contains(err.Error(), "抓取中断") {
		t.Fatalf("错误应说明已中断: %v", err)
	}
}

func TestScrapeAbortsWhenAllDetailsFail(t *testing.T) {
	f := &netFetcher{list: demoListHTML(4), detailErr: fmt.Errorf("%w: connection reset", ErrNetwork)}
	rs := New(demoScrapePackage(4), f)
	emitted := 0
	err := rs.Scrape(context.Background(), source.ScrapeRequest{Pages: []int{1}, PageSize: 8}, func(model.Row) error {
		emitted++
		return nil
	})
	if err == nil {
		t.Fatal("详情页全部网络异常时应中断并返回错误")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("错误应包装 ErrNetwork: %v", err)
	}
	if !strings.Contains(err.Error(), "详情页网络异常") {
		t.Fatalf("错误信息不明确: %v", err)
	}
	if emitted != 0 {
		t.Fatalf("中断后不应再入库，实际 emit %d 条", emitted)
	}
}

func TestScrapeAbortsWhenAllDetailsBlocked(t *testing.T) {
	f := &netFetcher{list: demoListHTML(2), detailErr: fmt.Errorf("%w: just a moment", ErrChallenge)}
	rs := New(demoScrapePackage(4), f)
	err := rs.Scrape(context.Background(), source.ScrapeRequest{Pages: []int{1}, PageSize: 8}, scrapeNoop)
	if err == nil {
		t.Fatal("详情页全部被拦截时应中断")
	}
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("错误应包装 ErrChallenge: %v", err)
	}
	if !strings.Contains(err.Error(), "反爬拦截") {
		t.Fatalf("错误信息不明确: %v", err)
	}
}
