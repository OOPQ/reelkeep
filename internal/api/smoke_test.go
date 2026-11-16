package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
)

// TestHTTPContractSmoke 覆盖主要 /api 路由：不允许 404/405（路由缺失），
// 并对关键接口断言具体状态码。外部网络/会拉起桌面进程的接口不在其中。
func TestHTTPContractSmoke(t *testing.T) {
	s, _ := newP3Server(t)
	row := model.Row{Token: "smoke", Status: "new", Source: "local", Title: "Smoke"}
	if err := s.st.UpsertRows([]model.Row{row}); err != nil {
		t.Fatal(err)
	}
	reloadRows(t, s)

	type tc struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}
	cases := []tc{
		{"state", "GET", "/api/state", nil, 200},
		{"projects", "GET", "/api/projects", nil, 200},
		{"desktop", "GET", "/api/desktop", nil, 200},
		{"logo", "GET", "/api/logo", nil, 200},
		{"cover missing", "GET", "/api/cover?token=missing", nil, 404},
		{"deleted", "GET", "/api/deleted", nil, 200},
		{"collections list", "GET", "/api/collections", nil, 200},
		{"collections create", "POST", "/api/collections", map[string]any{"name": "Smoke", "color": "#3b82f6"}, 0},
		{"sources list", "GET", "/api/sources", nil, 0},
		{"source package", "GET", "/api/sources/package?id=missing", nil, 0},
		{"source export", "GET", "/api/sources/export?id=missing", nil, 0},
		{"source auth", "GET", "/api/sources/auth?id=missing", nil, 0},
		{"snapshots", "GET", "/api/snapshots?token=smoke", nil, 200},
		{"snapshot jobs", "GET", "/api/snapshot/jobs?token=smoke", nil, 200},
		{"download info", "GET", "/api/download", nil, 200},
		{"download add", "POST", "/api/download", map[string]any{}, 0},
		{"download pause", "POST", "/api/download/pause", map[string]any{}, 0},
		{"download cancel", "POST", "/api/download/cancel", map[string]any{}, 0},
		{"download clear", "POST", "/api/download/clear-done", nil, 0},
		{"download reset", "POST", "/api/download/reset", nil, 0},
		{"download repair", "POST", "/api/download/repair", nil, 200},
		{"config", "POST", "/api/config", map[string]any{"proxy": "", "concurrent": 2}, 200},
		{"scan", "POST", "/api/scan", map[string]any{}, 200},
		{"fav", "POST", "/api/fav", map[string]any{"tokens": []string{"smoke"}, "value": true}, 200},
		{"rate", "POST", "/api/rate", map[string]any{"tokens": []string{"smoke"}, "value": 4}, 200},
		{"mark", "POST", "/api/mark", map[string]any{"tokens": []string{"smoke"}, "value": "later"}, 200},
		{"watched", "POST", "/api/watched", map[string]any{"tokens": []string{"smoke"}, "value": true}, 200},
		{"delete", "POST", "/api/delete", map[string]any{"tokens": []string{"smoke"}}, 200},
		{"restore", "POST", "/api/restore", map[string]any{"tokens": []string{"smoke"}}, 200},
		{"media invalid", "GET", "/media/smoke.mp4?k=bad", nil, 404},
		{"diagnostics", "GET", "/api/diagnostics/scrape", nil, 200},
		{"client log", "POST", "/api/log", map[string]any{"level": "info", "message": "smoke"}, 0},
		{"project import", "POST", "/api/project/import", map[string]any{}, 200},
		{"import local empty", "POST", "/api/import/local", map[string]any{}, 0},
		{"snapshot empty", "POST", "/api/snapshot", map[string]any{}, 0},
		{"snapshot delete empty", "POST", "/api/snapshot/delete", map[string]any{}, 0},
		{"snapshot cover empty", "POST", "/api/snapshot/cover", map[string]any{}, 0},
		{"refresh empty", "POST", "/api/refresh-url", map[string]any{"tokens": []string{}}, 0},
		{"desktop window", "POST", "/api/desktop/window", map[string]any{"action": "minimize"}, 0},
		{"desktop picker", "POST", "/api/desktop/pick-folder", map[string]any{}, 0},
		{"desktop login", "POST", "/api/desktop/login", map[string]any{"id": "missing"}, 0},
	}
	if len(cases) < 25 {
		t.Fatalf("验收用例不足 25：%d", len(cases))
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body *bytes.Reader
			if c.body != nil {
				b, _ := json.Marshal(c.body)
				body = bytes.NewReader(b)
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(c.method, c.path, body)
			if c.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if (rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed) && rec.Code != c.want {
				t.Fatalf("%s %s 路由缺失: %d %s", c.method, c.path, rec.Code, rec.Body.String())
			}
			if c.want != 0 && rec.Code != c.want {
				t.Fatalf("%s %s = %d，期望 %d：%s", c.method, c.path, rec.Code, c.want, rec.Body.String())
			}
		})
	}
}
