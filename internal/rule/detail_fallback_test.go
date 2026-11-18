package rule

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// 年龄确认/前端渲染类页面：直连 HTML 里没有播放地址，需要用浏览器再渲染一次。
func TestDetailFallsBackToBrowser(t *testing.T) {
	if os.Getenv("REELKEEP_BROWSER_TEST") == "" || findBrowser() == "" {
		t.Skip("需要 REELKEEP_BROWSER_TEST=1 且本机有 Chrome/Edge")
	}
	t.Setenv("REELKEEP_BROWSER_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("Sec-Fetch-Mode") == "" {
			fmt.Fprint(w, `<html><body><h2>年龄确认</h2><button>是</button></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"VideoObject","name":"demo","contentUrl":"https://cdn.example.com/x.mp4"}
</script></head><body></body></html>`)
	}))
	defer srv.Close()

	p := &Package{
		Schema: 1, ID: "agegate", Name: "AgeGate", Version: "1.0.0",
		BaseURL: srv.URL, Capabilities: []string{"scrape", "detail", "download"},
		Defaults: Defaults{TimeoutSec: 40},
		List: List{URL: "/", Item: "div", Fields: map[string]Field{
			"id": {Selector: "a", Attr: "href", Regex: `video/(\d+)\.htm`},
		}},
		Detail: &Detail{URL: "/video/{id}.htm", Fields: map[string]Field{
			"title":    {JSONLD: "VideoObject.name"},
			"playable": {JSONLD: "VideoObject.contentUrl"},
		}},
		Download: &Download{Kind: "http", URL: "{playable}"},
	}
	p.normalize()
	f, err := NewHTTPFetcher(p, func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	// 测试用 httptest（127.0.0.1），绕过生产环境的 SSRF 内网拦截。
	f.client = &http.Client{Transport: &http.Transport{}}
	defer func() {
		f.CloseBrowser()
		time.Sleep(800 * time.Millisecond)
	}()
	rs := New(p, f)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	row, err := rs.ScrapeURL(ctx, srv.URL+"/video/7.htm")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(row.URL, "cdn.example.com/x.mp4") {
		t.Fatalf("没有从浏览器渲染结果里拿到播放地址: %+v", row)
	}
}
