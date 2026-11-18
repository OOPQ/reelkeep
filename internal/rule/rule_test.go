package rule

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

func parseIP(s string) net.IP { return net.ParseIP(s) }

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadDemo(t *testing.T) *Package {
	t.Helper()
	p, err := LoadFile(filepath.Join("testdata", "demo.json"))
	if err != nil {
		t.Fatalf("加载 demo 失败: %v", err)
	}
	return p
}

func TestLoadAndManifest(t *testing.T) {
	p := loadDemo(t)
	if p.Origin() != "imported" {
		t.Fatalf("Origin 应为 imported，实际 %q", p.Origin())
	}
	m := p.Manifest()
	if m.Key != "demo" || m.Order != 900 || len(m.Ranks) != 2 {
		t.Fatalf("Manifest 不对: %+v", m)
	}
}

func TestScrapeHTMLWithDetail(t *testing.T) {
	p := loadDemo(t)
	ff := &FixtureFetcher{
		ListURL: p.ListURL("latest", 1),
		List:    readFile(t, "list.html"),
		Detail:  readFile(t, "detail.html"),
	}
	rs := New(p, ff)
	var rows []model.Row
	err := rs.Scrape(context.Background(), source.ScrapeRequest{
		Rank: "latest", Pages: []int{1}, PageSize: 10,
	}, func(r model.Row) error {
		rows = append(rows, r)
		return nil
	})
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(rows))
	}
	first := rows[0]
	if first.Token != "demo@101" {
		t.Fatalf("token 应为 demo@101，实际 %q", first.Token)
	}
	if first.Title != "First video (detail)" {
		t.Fatalf("标题应来自详情页 JSON-LD，实际 %q", first.Title)
	}
	if first.URL != "https://cdn.example.com/videos/101.mp4" {
		t.Fatalf("播放地址不对: %q", first.URL)
	}
	if first.CoverURL != "https://cdn.example.com/covers/101.jpg" {
		t.Fatalf("封面地址不对: %q", first.CoverURL)
	}
	if first.Seconds != "754" || first.Duration != "12:34" {
		t.Fatalf("时长解析不对: seconds=%q duration=%q", first.Seconds, first.Duration)
	}
	if first.Views != "4242" {
		t.Fatalf("观看数不对: %q", first.Views)
	}
	if first.Source != "demo" || first.SourceID != "101" {
		t.Fatalf("来源字段不对: %+v", first)
	}
}

func TestResolveHeadersAnd410(t *testing.T) {
	p := loadDemo(t)
	rs := New(p, &FixtureFetcher{ListURL: p.ListURL("latest", 1), List: readFile(t, "list.html")})
	row := model.Row{
		Token: "demo@101", Source: "demo", SourceID: "101",
		URL: "https://cdn.example.com/videos/101.mp4", PageURL: "https://demo.example.com/video/101",
	}
	spec, err := rs.Resolve(context.Background(), row)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if spec.URL != row.URL || spec.Mode != "http" {
		t.Fatalf("解析结果不对: %+v", spec)
	}
	if len(spec.Headers) != 0 {
		t.Fatalf("cdn.example.com 在 excludeHeaderHosts 里，不应带 headers: %v", spec.Headers)
	}
	if !spec.RefreshOn410 {
		t.Fatal("410 应触发刷新详情")
	}
}

func TestScrapeJSONAPI(t *testing.T) {
	p, err := LoadFile(filepath.Join("testdata", "api.json"))
	if err != nil {
		t.Fatalf("加载 api 源失败: %v", err)
	}
	rs := New(p, &FixtureFetcher{ListURL: p.ListURL("latest", 1), List: readFile(t, "api-list.json")})
	var rows []model.Row
	if err := rs.Scrape(context.Background(), source.ScrapeRequest{Rank: "latest", Pages: []int{1}, PageSize: 10},
		func(r model.Row) error { rows = append(rows, r); return nil }); err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(rows))
	}
	if rows[0].Token != "demo-api@a1" || rows[0].Title != "API video one" {
		t.Fatalf("第一条不对: %+v", rows[0])
	}
	if rows[0].Seconds != "90" || rows[0].Duration != "01:30" {
		t.Fatalf("时长不对: %+v", rows[0])
	}
	if rows[1].Seconds != "125" {
		t.Fatalf("ISO8601 时长解析不对: %q", rows[1].Seconds)
	}
}

func TestValidateErrors(t *testing.T) {
	p := loadDemo(t)
	p.List.Fields["title"] = Field{Selector: "a["}
	p.Download.URL = "{bogus}"
	errs := p.Validate()
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	if !strings.Contains(joined, "CSS 选择器不合法") {
		t.Fatalf("应报 CSS 选择器错误: %s", joined)
	}
	if !strings.Contains(joined, "未知占位符") {
		t.Fatalf("应报占位符错误: %s", joined)
	}
}

func TestValidateUnknownField(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"schema":1,"id":"x","name":"X","version":"1.0.0","baseUrl":"https://x.example.com","capabilities":["scrape"],"list":{"url":"/l","item":"a","fields":{"id":{"selector":"a","attr":"href"}}},"ooops":true}`)
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("应报未知字段: %v", err)
	}
}

func TestValidateMissingID(t *testing.T) {
	p := loadDemo(t)
	delete(p.List.Fields, "id")
	errs := p.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "list.fields.id") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报缺少 id: %v", errs)
	}
}

func TestAllowedHostsAndSchemes(t *testing.T) {
	p := loadDemo(t)
	hf, err := NewHTTPFetcher(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	ok := []string{"https://demo.example.com/a", "https://cdn.example.com/b"}
	bad := []string{"http://127.0.0.1/a", "file:///C:/a.mp4", "https://evil.example.org/a", "http://10.0.0.5/x"}
	for _, u := range ok {
		if err := hf.Allowed(u); err != nil {
			t.Fatalf("%s 应允许: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := hf.Allowed(u); err == nil {
			t.Fatalf("%s 应拒绝", u)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "169.254.1.1", "100.64.0.1", "::1", "fc00::1"} {
		if !isPrivateIP(parseIP(s)) {
			t.Fatalf("%s 应判为内网", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if isPrivateIP(parseIP(s)) {
			t.Fatalf("%s 不应判为内网", s)
		}
	}
}

func TestLoadDirSkipsBadFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), readFile(t, "demo.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs, errs := LoadDir(dir)
	if len(pkgs) != 1 || len(errs) != 1 {
		t.Fatalf("期望 1 个成功 + 1 个失败，实际 %d/%d", len(pkgs), len(errs))
	}
}

// 仓库里随附的源包必须始终能通过校验。
func TestShippedPackagesLoad(t *testing.T) {
	dir := filepath.Join("..", "..", "sources")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") || e.Name() == "index.json" {
			continue
		}
		n++
		if _, err := LoadFile(filepath.Join(dir, e.Name())); err != nil {
			t.Fatalf("%s 校验失败: %v", e.Name(), err)
		}
	}
	if n == 0 {
		t.Fatal("sources 目录里没有源包")
	}
}

func TestDetailPageURLAndRawRegex(t *testing.T) {
	p := &Package{
		Schema: 1, ID: "demo", Name: "Demo", Version: "1.0.0",
		BaseURL:      "https://demo.example.com",
		Capabilities: []string{"scrape", "detail"},
		Detail: &Detail{
			URL: "{pageUrl}",
			Fields: map[string]Field{
				"title":    {Selector: "meta[property='og:title']", Attr: "content"},
				"playable": {Raw: true, Regex: `data-src="(https?://[^"]+\.mp4[^"]*)"`},
			},
		},
	}
	p.normalize()
	rs := New(p, &FixtureFetcher{Detail: []byte(`<html><head><meta property="og:title" content="T"></head>
<body><video data-src="https://cdn.example.com/a.mp4?x=1&amp;y=2"></video></body></html>`)})
	row, err := rs.detailFor(context.Background(), "1", "https://demo.example.com/list/1")
	if err != nil {
		t.Fatalf("详情解析失败: %v", err)
	}
	if row.Title != "T" {
		t.Fatalf("标题不对: %q", row.Title)
	}
	if row.URL != "https://cdn.example.com/a.mp4?x=1&y=2" {
		t.Fatalf("raw 正则应取到并反转义 mp4 地址，实际 %q", row.URL)
	}
}

func TestAuthValidation(t *testing.T) {
	p := loadDemo(t)
	p.Auth = &Auth{Type: "cookie", Domains: []string{"example.com"}}
	if errs := p.Validate(); len(errs) != 0 {
		t.Fatalf("合法 auth 不应报错: %v", errs)
	}
	p.Auth.Type = "oauth"
	errs := p.Validate()
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "auth.type") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报 auth.type 错误: %v", errs)
	}
}

func TestScrapeURL(t *testing.T) {
	p := loadDemo(t)
	rs := New(p, &FixtureFetcher{Detail: readFile(t, "detail.html")})
	row, err := rs.ScrapeURL(context.Background(), "https://demo.example.com/video/101")
	if err != nil {
		t.Fatalf("链接抓取失败: %v", err)
	}
	if row.Token != "demo@101" || row.URL == "" {
		t.Fatalf("结果不对: %+v", row)
	}
	if row.PageURL != "https://demo.example.com/video/101" {
		t.Fatalf("page_url 应保留输入链接: %q", row.PageURL)
	}
	if _, err := rs.ScrapeURL(context.Background(), "https://demo.example.com/other/1"); err == nil {
		t.Fatal("不匹配的链接应报错")
	}
}
