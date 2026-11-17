package download

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
	"github.com/OOPQ/reelkeep/internal/store"
)

func newTestManager(t *testing.T, reg *source.Registry, row model.Row) (*Manager, *store.Store, config.Paths) {
	t.Helper()
	paths := config.ForRoot(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	if row.Token != "" {
		if err := st.UpsertRows([]model.Row{row}); err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager(paths, st, reg, func(string) string { return "" }, nil, nil,
		func(kind, msg string) { t.Logf("[%s] %s", kind, msg) })
	m.SetSettings(Settings{Concurrent: 1})
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m.Stop()
		cancel()
		_ = st.Close()
	})
	return m, st, paths
}

func waitState(t *testing.T, m *Manager, token, want string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, it := range m.Info().Items {
			if it.Token == token && it.State == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s 没有进入状态 %s（当前 %+v）", token, want, m.Info().Items)
}

func TestHTTPResume(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789abcdef"), 128*1024) // 2MB
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		http.ServeContent(w, r, "a.mp4", time.Now(), bytes.NewReader(content))
	}))
	defer srv.Close()

	row := model.Row{Token: "t1", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)

	// 预置半个文件，验证续传
	part := filepath.Join(paths.Content, "demo", "t1.mp4.part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, content[:len(content)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Enqueue(model.DownloadTask{
		Token: "t1", Source: "demo", URL: row.URL, Mode: "http", State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "t1", "done", 20*time.Second)

	final := filepath.Join(paths.Content, "demo", "t1.mp4")
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("没有产出文件: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("文件内容不一致：%d vs %d", len(got), len(content))
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal(".part 应已改名")
	}
	sawRange := false
	for _, r := range ranges {
		if r != "" {
			sawRange = true
		}
	}
	if !sawRange {
		t.Fatalf("续传时应发送 Range，实际请求头: %v", ranges)
	}
}

// fakeSource 用来验证 410 刷新。
type fakeSource struct {
	refreshURL string
	refreshes  int
}

func (f *fakeSource) ID() string             { return "fake" }
func (f *fakeSource) Capabilities() []string { return []string{"download"} }
func (f *fakeSource) Scrape(ctx context.Context, req source.ScrapeRequest, emit func(model.Row) error) error {
	return nil
}
func (f *fakeSource) HealthCheck(ctx context.Context) error { return nil }
func (f *fakeSource) Resolve(ctx context.Context, r model.Row) (source.DownloadSpec, error) {
	return source.DownloadSpec{URL: f.refreshURL, Mode: "http", Headers: map[string]string{}, RefreshOn410: true}, nil
}
func (f *fakeSource) Refresh(ctx context.Context, r model.Row) (model.Row, error) {
	f.refreshes++
	r.URL = f.refreshURL
	return r, nil
}

func TestRefreshOn410(t *testing.T) {
	content := []byte("fresh-content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	fs := &fakeSource{refreshURL: srv.URL + "/good"}
	reg := source.EmptyRegistry()
	reg.Add(source.Manifest{Key: "fake", Name: "Fake", Origin: "imported", Capabilities: []string{"download"}}, fs)

	row := model.Row{Token: "t2", Status: "new", Source: "fake", URL: srv.URL + "/bad"}
	m, _, paths := newTestManager(t, reg, row)
	if _, err := m.Enqueue(model.DownloadTask{
		Token: "t2", Source: "fake", URL: srv.URL + "/bad", Mode: "http",
		RefreshOn410: true, State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "t2", "done", 20*time.Second)
	if fs.refreshes == 0 {
		t.Fatal("410 之后应调用源刷新")
	}
	got, err := os.ReadFile(filepath.Join(paths.Content, "fake", "t2.mp4"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("刷新后的文件不对: %v %q", err, got)
	}
}

func TestCancelRemovesPart(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)

	row := model.Row{Token: "t3", Status: "new", Source: "demo", URL: srv.URL + "/slow"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	if _, err := m.Enqueue(model.DownloadTask{
		Token: "t3", Source: "demo", URL: row.URL, Mode: "http", State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "t3", "downloading", 10*time.Second)
	if n := m.Cancel([]string{"t3"}); n != 1 {
		t.Fatalf("应取消 1 条，实际 %d", n)
	}
	for _, item := range m.Info().Items {
		if item.Token == "t3" {
			t.Fatal("取消后任务应从队列删除")
		}
	}
	part := filepath.Join(paths.Content, "demo", "t3.mp4.part")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(part); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(".part 应被删除")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestLegacyQueueMigration(t *testing.T) {
	paths := config.ForRoot(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	legacy := `[{"token":"old1","url":"https://example.com/a.mp4","mode":"http","headers":{},"source":"pektino","addedAt":"2026-11-01 10:00:00"}]`
	if err := os.WriteFile(filepath.Join(paths.DataDir, ".queue.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(paths, st, source.EmptyRegistry(), func(string) string { return "" }, nil, nil,
		func(kind, msg string) { t.Logf("[%s] %s", kind, msg) })
	m.migrateLegacyQueue()
	tasks, err := st.LoadTasks()
	if err != nil || len(tasks) != 1 || tasks[0].Token != "old1" || tasks[0].State != "queued" {
		t.Fatalf("旧队列迁移失败: %+v %v", tasks, err)
	}
	if _, err := os.Stat(filepath.Join(paths.DataDir, ".queue.json.migrated")); err != nil {
		t.Fatalf("旧文件应改名为 .migrated: %v", err)
	}
}
