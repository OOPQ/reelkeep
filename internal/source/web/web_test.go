package web

import (
	"context"
	"strings"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

func TestScrapeURLPage(t *testing.T) {
	old := fetchPage
	defer func() { fetchPage = old }()
	fetchPage = func(ctx context.Context, rawURL string) ([]byte, error) {
		return []byte(`<html><head>
<meta property="og:title" content="My Video">
<meta property="og:image" content="/cover.jpg">
<meta property="video:duration" content="125">
</head><body><video><source src="//cdn.example.com/v.mp4"></video></body></html>`), nil
	}
	a := &Adapter{}
	row, err := a.ScrapeURL(context.Background(), "https://example.com/watch/1")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if row.Title != "My Video" {
		t.Fatalf("标题不对: %q", row.Title)
	}
	if row.CoverURL != "https://example.com/cover.jpg" {
		t.Fatalf("封面应解析为绝对地址: %q", row.CoverURL)
	}
	if row.URL != "https://cdn.example.com/v.mp4" {
		t.Fatalf("媒体地址不对: %q", row.URL)
	}
	if row.DownloadKind != "http" {
		t.Fatalf("kind 不对: %q", row.DownloadKind)
	}
	if !strings.HasPrefix(row.Token, "web@") || row.SourceID == "" {
		t.Fatalf("token 不对: %+v", row)
	}
	if row.Seconds != "125" || row.Duration != "02:05" {
		t.Fatalf("时长不对: seconds=%q duration=%q", row.Seconds, row.Duration)
	}
}

func TestScrapeURLDirectMedia(t *testing.T) {
	a := &Adapter{}
	row, err := a.ScrapeURL(context.Background(), "https://cdn.example.com/path/a.m3u8?token=1")
	if err != nil {
		t.Fatal(err)
	}
	if row.DownloadKind != "hls" || row.URL == "" {
		t.Fatalf("直链应识别为 HLS: %+v", row)
	}
	if row.Title != "a.m3u8" {
		t.Fatalf("标题应取文件名: %q", row.Title)
	}
}

func TestScrapeListUnsupported(t *testing.T) {
	a := &Adapter{}
	err := a.Scrape(context.Background(), source.ScrapeRequest{}, func(m model.Row) error { return nil })
	if err == nil {
		t.Fatal("web 源不应支持榜单抓取")
	}
}
