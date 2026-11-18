package download

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

func TestSafeName(t *testing.T) {
	ok := []string{"demo@127663", "abc", "a_b-c.d", "视频@1"}
	bad := []string{"", ".", "..", "a/b", `a\b`, "a:b", "a*b", "a?b", "a\"b", "a<b", "a>b", "a|b"}
	for _, s := range ok {
		if !safeName(s) {
			t.Errorf("safeName(%q) = false", s)
		}
	}
	for _, s := range bad {
		if safeName(s) {
			t.Errorf("safeName(%q) = true", s)
		}
	}
}

func TestUnsafeTokenRejected(t *testing.T) {
	m, _, _ := newTestManager(t, source.EmptyRegistry(), model.Row{})
	cases := []model.DownloadTask{
		{Token: "../evil", Source: "demo", URL: "https://example.com/a.mp4", Mode: "http"},
		{Token: "ok", Source: "../evil", URL: "https://example.com/a.mp4", Mode: "http"},
	}
	for _, c := range cases {
		if _, err := m.Enqueue(c); err == nil {
			t.Fatalf("应拒绝非法路径: %+v", c)
		}
	}
}

func TestContentRangeMismatchRestarts(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 100)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Range") != "" {
			// 故意返回从 0 开始的范围，模拟不守规矩的服务器
			w.Header().Set("Content-Range", "bytes 0-9/100")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(content[:10])
			return
		}
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	row := model.Row{Token: "range", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	part := filepath.Join(paths.Content, "demo", "range.mp4.part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, content[:50], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue(model.DownloadTask{
		Token: "range", Source: "demo", URL: row.URL, Mode: "http", State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "range", "done", 10*time.Second)
	got, err := os.ReadFile(filepath.Join(paths.Content, "demo", "range.mp4"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("内容不对: %v len=%d", err, len(got))
	}
	if requests < 2 {
		t.Fatalf("范围不匹配后应重新请求，requests=%d", requests)
	}
}

func Test410RefreshOnlyOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	fs := &fakeSource{refreshURL: srv.URL + "/bad"}
	reg := source.EmptyRegistry()
	reg.Add(source.Manifest{Key: "fake", Name: "Fake", Origin: "imported", Capabilities: []string{"download"}}, fs)
	row := model.Row{Token: "t410", Status: "new", Source: "fake", URL: srv.URL + "/bad"}
	m, _, _ := newTestManager(t, reg, row)
	if _, err := m.Enqueue(model.DownloadTask{
		Token: "t410", Source: "fake", URL: srv.URL + "/bad", Mode: "http",
		RefreshOn410: true, State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "t410", "failed", 10*time.Second)
	if fs.refreshes != 1 {
		t.Fatalf("410 只应刷新一次，实际 %d", fs.refreshes)
	}
}
