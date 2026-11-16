package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
	"github.com/OOPQ/reelkeep/internal/store"
	webassets "github.com/OOPQ/reelkeep/web"
)

// newAcceptanceServer 起一个带真实静态资源的完整 API 服务，用于 P0 验收。
func newAcceptanceServer(t *testing.T) (*Server, *store.Store, *httptest.Server, config.Paths) {
	t.Helper()
	paths := config.ForRoot(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	reg := source.EmptyRegistry()
	dl := download.NewManager(paths, st, reg, func(string) string { return "" }, nil, nil,
		func(string, string) {})
	dl.SetSettings(download.Settings{Concurrent: 2})
	if err := dl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := New(paths, st, reg, nil, dl, config.Settings{Concurrent: 2}, assets)
	srv.SetVersion("0.0.0-test")
	if err := srv.Boot(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		dl.Stop()
		_ = st.Close()
	})
	return srv, st, ts, paths
}

func getJSON(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}

func postJSONAccept(t *testing.T, ts *httptest.Server, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestP0Acceptance 移植旧版 25 项 HTTP 验收，并按当前契约固定断言。
func TestP0Acceptance(t *testing.T) {
	s, st, ts, paths := newAcceptanceServer(t)
	row := model.Row{Token: "acc", Status: "new", Source: "local", Title: "Acceptance"}
	if err := st.UpsertRows([]model.Row{row}); err != nil {
		t.Fatal(err)
	}
	reloadRows(t, s)

	t.Run("01 首页 HTML", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || !strings.Contains(string(b), "Reelkeep") {
			t.Fatalf("status=%d body=%s", resp.StatusCode, string(b[:min(200, len(b))]))
		}
	})

	t.Run("02 JS 资源", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		idx := strings.Index(string(b), "assets/")
		if idx < 0 {
			t.Fatal("首页没有 assets 引用")
		}
		start := idx
		end := strings.IndexAny(string(b)[start:], "\"'")
		if end < 0 {
			t.Fatal("无法解析资源路径")
		}
		asset := string(b)[start : start+end]
		r2, err := http.Get(ts.URL + "/" + asset)
		if err != nil {
			t.Fatal(err)
		}
		_ = r2.Body.Close()
		if r2.StatusCode != 200 {
			t.Fatalf("asset %s = %d", asset, r2.StatusCode)
		}
	})

	t.Run("03 路径穿越被拒", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/x", nil)
		req.URL.Path = "/assets/../../../../windows/win.ini"
		rec := httptest.NewRecorder()
		s.handleAssets(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("traversal = %d", rec.Code)
		}
	})

	t.Run("04 state 结构", func(t *testing.T) {
		out := getJSON(t, ts, "/api/state")
		for _, k := range []string{"ok", "api", "rows", "rowsRev", "stats", "project", "download"} {
			if _, ok := out[k]; !ok {
				t.Fatalf("state 缺少 %s", k)
			}
		}
		stats, _ := out["stats"].(map[string]any)
		if stats == nil {
			t.Fatal("stats 不是对象")
		}
		for _, k := range []string{"total", "new", "done", "downloaded", "missing"} {
			if _, ok := stats[k]; !ok {
				t.Fatalf("stats 缺少 %s", k)
			}
		}
	})

	t.Run("05 cover 缺 token 不 500", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/cover?token=__nope__")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 {
			t.Fatalf("cover = %d", resp.StatusCode)
		}
	})

	t.Run("06 export CSV", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/export")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || !bytes.Contains(b, []byte("token")) {
			t.Fatalf("export status=%d len=%d", resp.StatusCode, len(b))
		}
	})

	t.Run("07 project 结构", func(t *testing.T) {
		out := getJSON(t, ts, "/api/project")
		p, _ := out["project"].(map[string]any)
		for _, k := range []string{"root", "media", "videoCount", "exists"} {
			if _, ok := p[k]; !ok {
				t.Fatalf("project 缺少 %s", k)
			}
		}
	})

	t.Run("08 scan 结构", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/scan", map[string]any{})
		if status != 200 || out["ok"] != true {
			t.Fatalf("scan status=%d %v", status, out)
		}
		for _, k := range []string{"files", "downloaded", "changed"} {
			if _, ok := out[k]; !ok {
				t.Fatalf("scan 缺少 %s", k)
			}
		}
	})

	t.Run("09 mark 非法参数", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/mark", map[string]any{"tokens": []string{}, "status": "bogus"})
		if status >= 500 {
			t.Fatalf("mark = %d", status)
		}
		if status == 200 && out["ok"] == true {
			t.Fatal("mark 非法参数不应返回 ok=true")
		}
	})

	t.Run("10 rate 往返", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/rate", map[string]any{"tokens": []string{"acc"}, "value": 5})
		if status != 200 || out["ok"] != true {
			t.Fatalf("rate set status=%d %v", status, out)
		}
		state := getJSON(t, ts, "/api/state")
		rows, _ := state["rows"].([]any)
		found := false
		for _, r := range rows {
			m, _ := r.(map[string]any)
			if m["token"] == "acc" {
				found = strings.TrimSpace(toString(m["rating"])) == "5"
			}
		}
		if !found {
			t.Fatal("rate 读回不是 5")
		}
		_, _ = postJSONAccept(t, ts, "/api/rate", map[string]any{"tokens": []string{"acc"}, "value": 0})
	})

	t.Run("11 非法 JSON 400", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/mark", "application/json", strings.NewReader("{bad"))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid json = %d", resp.StatusCode)
		}
	})

	t.Run("12 play 缺 token 非 500", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/play", map[string]any{"token": "__nope__"})
		if status >= 500 || out["ok"] != false {
			t.Fatalf("play status=%d %v", status, out)
		}
	})

	t.Run("13 delete 缺 token 非 500", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/delete", map[string]any{"tokens": []string{"__nope__"}, "purge": true})
		if status >= 500 || out["ok"] != false {
			t.Fatalf("delete status=%d %v", status, out)
		}
	})

	t.Run("14 非法代理被拒", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/config", map[string]any{"proxy": "not a proxy !!"})
		if status != 200 || out["ok"] != false {
			t.Fatalf("proxy status=%d %v", status, out)
		}
	})

	t.Run("15 并发越界被拒", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/config", map[string]any{"concurrent": 99})
		if status != 200 || out["ok"] != false {
			t.Fatalf("concurrent status=%d %v", status, out)
		}
	})

	t.Run("16 并发 16 接受", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/config", map[string]any{"concurrent": 16})
		if status != 200 || out["ok"] != true || int(num(out["concurrent"])) != 16 {
			t.Fatalf("concurrent16 status=%d %v", status, out)
		}
		_, _ = postJSONAccept(t, ts, "/api/config", map[string]any{"concurrent": 2})
	})

	t.Run("17 scrape 非法榜单不启动任务", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/scrape", map[string]any{"rank": "bogus"})
		if status >= 500 || out["ok"] != false {
			t.Fatalf("scrape status=%d %v", status, out)
		}
	})

	t.Run("18 download paused 字段", func(t *testing.T) {
		out := getJSON(t, ts, "/api/download")
		d, _ := out["download"].(map[string]any)
		if _, ok := d["paused"]; !ok {
			t.Fatal("download 缺少 paused")
		}
	})

	t.Run("19 pause 空闲", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/download/pause", map[string]any{"on": true})
		if status != 200 || out["ok"] != true {
			t.Fatalf("pause status=%d %v", status, out)
		}
		_, _ = postJSONAccept(t, ts, "/api/download/pause", map[string]any{"on": false})
	})

	t.Run("20 reset 空闲幂等", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/download/reset", map[string]any{})
		if status != 200 || out["ok"] != true {
			t.Fatalf("reset status=%d %v", status, out)
		}
	})

	t.Run("21 SSE hello", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("content-type=%s", resp.Header.Get("Content-Type"))
		}
		buf := make([]byte, 256)
		n, _ := resp.Body.Read(buf)
		if !strings.Contains(string(buf[:n]), "hello") {
			t.Fatalf("SSE 首包=%q", string(buf[:n]))
		}
	})

	t.Run("22 项目导入新增", func(t *testing.T) {
		media := filepath.Join(paths.Content, "local")
		if err := os.MkdirAll(media, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(media, "import-me.mp4"), []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
		status, out := postJSONAccept(t, ts, "/api/project/import", map[string]any{})
		if status != 200 || num(out["added"]) < 1 {
			t.Fatalf("import status=%d %v", status, out)
		}
	})

	t.Run("23 rowsRev 增量", func(t *testing.T) {
		before := getJSON(t, ts, "/api/state")
		rev := num(before["rowsRev"])
		out := getJSON(t, ts, "/api/state?rowsRev="+strconv.FormatFloat(rev, 'f', -1, 64))
		if _, hasRows := out["rows"]; hasRows {
			t.Fatal("rowsRev 未变化时不应下发 rows")
		}
	})

	t.Run("24 收藏往返", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/fav", map[string]any{"tokens": []string{"acc"}, "value": true})
		if status != 200 || out["ok"] != true {
			t.Fatalf("fav status=%d %v", status, out)
		}
		state := getJSON(t, ts, "/api/state")
		rows, _ := state["rows"].([]any)
		for _, r := range rows {
			m, _ := r.(map[string]any)
			if m["token"] == "acc" && toString(m["fav"]) == "" {
				t.Fatal("fav 没有写回")
			}
		}
	})

	t.Run("25 下载器未启用时错误清晰", func(t *testing.T) {
		status, out := postJSONAccept(t, ts, "/api/download", map[string]any{"tokens": []string{"acc"}})
		if status >= 500 || out["ok"] != false {
			t.Fatalf("download status=%d %v", status, out)
		}
		if toString(out["error"]) == "" {
			t.Fatal("缺少 error")
		}
	})
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
