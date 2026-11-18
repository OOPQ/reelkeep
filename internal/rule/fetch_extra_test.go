package rule

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProfileByName(t *testing.T) {
	for _, name := range []string{"chrome", "firefox", "safari", "Chrome"} {
		if _, ok := profileByName(name); !ok {
			t.Errorf("profileByName(%q) 不认识", name)
		}
	}
	for _, name := range []string{"", "opera", "unknown"} {
		if _, ok := profileByName(name); ok {
			t.Errorf("profileByName(%q) 不应认识", name)
		}
	}
}

// TestRenderBrowser 用本机 Chrome/Edge 无头渲染一个本地页面。
// 没有浏览器时跳过（CI/服务器环境）。
func TestRenderBrowser(t *testing.T) {
	if findBrowser() == "" {
		t.Skip("本机没有 Chrome/Edge")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><div id="app"></div>
<script>document.getElementById('app').textContent='rendered-by-js'</script></body></html>`)
	}))
	defer srv.Close()

	p := &Package{
		Schema: 1, ID: "demo", Name: "Demo", Version: "1.0.0",
		BaseURL: srv.URL, Capabilities: []string{"scrape"},
		Defaults: Defaults{Render: "browser", TimeoutSec: 40},
		List:     List{URL: "/", Item: "div", Fields: map[string]Field{"title": {Selector: "div"}}},
	}
	p.normalize()
	f, err := NewHTTPFetcher(p, func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := f.Get(ctx, srv.URL, nil)
	if err != nil {
		t.Fatalf("浏览器渲染失败: %v", err)
	}
	if !strings.Contains(string(b), "rendered-by-js") {
		t.Fatalf("渲染结果里没有 JS 产物: %.200s", string(b))
	}
}
