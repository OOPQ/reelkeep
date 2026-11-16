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
	hf, err := NewHTTPFetcher(p)
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
