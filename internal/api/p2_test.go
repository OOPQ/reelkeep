package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
	"github.com/OOPQ/reelkeep/internal/store"
)

type fakeURLSource struct {
	token  string
	newURL string
}

func (f *fakeURLSource) ID() string             { return "fake" }
func (f *fakeURLSource) Capabilities() []string { return []string{"scrape", "download"} }
func (f *fakeURLSource) Scrape(ctx context.Context, req source.ScrapeRequest, emit func(model.Row) error) error {
	return nil
}
func (f *fakeURLSource) HealthCheck(ctx context.Context) error { return nil }
func (f *fakeURLSource) ScrapeURL(ctx context.Context, rawURL string) (model.Row, error) {
	return model.Row{
		Token: f.token, Source: "fake", SourceID: "9", URL: f.newURL,
		PageURL: rawURL, Title: "Fake Video", Status: "new",
	}, nil
}
func (f *fakeURLSource) Resolve(ctx context.Context, r model.Row) (source.DownloadSpec, error) {
	return source.DownloadSpec{URL: r.URL, Mode: "http", Headers: map[string]string{}}, nil
}

func newP2Server(t *testing.T, fs *fakeURLSource, rows []model.Row) (*Server, *store.Store, *httptest.Server) {
	t.Helper()
	paths := config.ForRoot(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) > 0 {
		if err := st.UpsertRows(rows); err != nil {
			t.Fatal(err)
		}
	}
	reg := source.EmptyRegistry()
	reg.Add(source.Manifest{
		Key: "fake", Name: "Fake", Origin: "imported",
		Capabilities: []string{"scrape", "download"},
	}, fs)
	reg.Register("fake", func() source.Source { return fs })
	srv := New(paths, st, reg, nil, nil, config.Settings{Concurrent: 1}, nil)
	if err := srv.Boot(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = st.Close()
	})
	return srv, st, ts
}

func waitJob(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.jobMu.Lock()
		j := s.job
		running := j != nil && j.isRunning()
		s.jobMu.Unlock()
		if !running {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("抓取任务超时")
}

func TestScrapeByURL(t *testing.T) {
	fs := &fakeURLSource{token: "fake@9", newURL: "https://cdn.example.com/v.mp4"}
	s, st, ts := newP2Server(t, fs, nil)
	b, _ := json.Marshal(map[string]any{"source": "fake", "url": "https://example.com/v/9"})
	resp, err := http.Post(ts.URL+"/api/scrape", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	if out["ok"] != true {
		t.Fatalf("链接抓取没有启动: %v", out)
	}
	waitJob(t, s)
	row, ok, err := st.GetRow("fake@9")
	if err != nil || !ok {
		t.Fatalf("没有入库: %v", err)
	}
	if row.URL != "https://cdn.example.com/v.mp4" || row.PageURL != "https://example.com/v/9" {
		t.Fatalf("入库内容不对: %+v", row)
	}
}

func TestRefreshURL(t *testing.T) {
	fs := &fakeURLSource{token: "fake@9", newURL: "https://cdn.example.com/new.mp4"}
	row := model.Row{
		Token: "fake@9", Status: "new", Source: "fake", SourceID: "9",
		URL: "https://cdn.example.com/old.mp4", PageURL: "https://example.com/v/9", Title: "Fake",
	}
	_, st, ts := newP2Server(t, fs, []model.Row{row})
	b, _ := json.Marshal(map[string]any{"tokens": []string{"fake@9"}})
	resp, err := http.Post(ts.URL+"/api/refresh-url", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	if out["ok"] != true || int(out["refreshed"].(float64)) != 1 {
		t.Fatalf("刷新失败: %v", out)
	}
	got, ok, err := st.GetRow("fake@9")
	if err != nil || !ok || got.URL != "https://cdn.example.com/new.mp4" {
		t.Fatalf("地址没有更新: %+v %v", got, err)
	}
}
