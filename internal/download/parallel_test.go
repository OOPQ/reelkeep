package download

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// slowWriter 拉长每个 Write 的耗时，让多个分片请求更容易重叠。
type slowWriter struct {
	http.ResponseWriter
	delay time.Duration
}

func (w *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	return w.ResponseWriter.Write(p)
}

func testContent(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(i*31 + 7)
	}
	return b
}

func enableParallel(m *Manager, conns int) {
	m.SetSettings(Settings{Concurrent: 1, HTTPConnections: conns, RetryCount: 3, RetryDelay: 10 * time.Millisecond})
}

func TestHTTPParallelUsesMultipleConnections(t *testing.T) {
	content := testContent(10 << 20)
	var mu sync.Mutex
	active, maxActive := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()
		http.ServeContent(&slowWriter{ResponseWriter: w, delay: time.Millisecond}, r, "a.mp4", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()

	row := model.Row{Token: "p1", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	enableParallel(m, 4)
	if _, err := m.Enqueue(model.DownloadTask{
		Token: "p1", Source: "demo", URL: row.URL, Mode: "http", State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "p1", "done", 30*time.Second)

	got, err := os.ReadFile(filepath.Join(paths.Content, "demo", "p1.mp4"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("内容不一致: err=%v len=%d", err, len(got))
	}
	mu.Lock()
	max := maxActive
	mu.Unlock()
	if max < 2 {
		t.Fatalf("没有并发分片请求，最大并发 %d", max)
	}
}

func TestHTTPParallelFallsBackWhenRangeIgnored(t *testing.T) {
	content := testContent(9 << 20)
	requests := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		_, _ = w.Write(content) // 忽略 Range，始终返回 200 全量
	}))
	defer srv.Close()

	row := model.Row{Token: "p2", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	enableParallel(m, 4)
	if _, err := m.Enqueue(model.DownloadTask{
		Token: "p2", Source: "demo", URL: row.URL, Mode: "http", State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "p2", "done", 30*time.Second)

	got, err := os.ReadFile(filepath.Join(paths.Content, "demo", "p2.mp4"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("回退单连接后内容不一致: err=%v len=%d", err, len(got))
	}
	mu.Lock()
	n := requests
	mu.Unlock()
	if n > 3 {
		t.Fatalf("不支持 Range 时不应尝试多次分片，请求数 %d", n)
	}
}

func TestHTTPParallelResetsWhenPartMissing(t *testing.T) {
	content := testContent(8 << 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp4", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()

	row := model.Row{Token: "p4", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	enableParallel(m, 4)

	// sidecar 说分片 0 已完成，但 .part 实际不存在：不能跳过这段数据。
	part := filepath.Join(paths.Content, "demo", "p4.mp4.part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	segs := splitSegments(int64(len(content)), 4)
	segs[0].Done = segs[0].End - segs[0].Start + 1
	if err := saveParallelState(part, parallelState{Version: 1, Total: int64(len(content)), Segments: segs}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Enqueue(model.DownloadTask{
		Token: "p4", Source: "demo", URL: row.URL, Mode: "http", State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "p4", "done", 30*time.Second)

	got, err := os.ReadFile(filepath.Join(paths.Content, "demo", "p4.mp4"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf(".part 丢失后重建的文件不一致: err=%v len=%d", err, len(got))
	}
}

func TestHTTPParallelResumeFromState(t *testing.T) {
	content := testContent(8 << 20)
	var ranges []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		http.ServeContent(w, r, "a.mp4", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()

	row := model.Row{Token: "p3", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	enableParallel(m, 4)

	// 模拟上次下载到一半：分片 0 完整、分片 1 一半，文件已预分配。
	part := filepath.Join(paths.Content, "demo", "p3.mp4.part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	segs := splitSegments(int64(len(content)), 4)
	segs[0].Done = segs[0].End - segs[0].Start + 1
	segs[1].Done = (segs[1].End - segs[1].Start + 1) / 2
	if err := os.WriteFile(part, make([]byte, len(content)), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(part, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	for _, seg := range segs[:2] {
		if seg.Done > 0 {
			if _, err := f.WriteAt(content[seg.Start:seg.Start+seg.Done], seg.Start); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = f.Close()
	if err := saveParallelState(part, parallelState{Version: 1, Total: int64(len(content)), Segments: segs}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Enqueue(model.DownloadTask{
		Token: "p3", Source: "demo", URL: row.URL, Mode: "http", State: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "p3", "done", 30*time.Second)

	got, err := os.ReadFile(filepath.Join(paths.Content, "demo", "p3.mp4"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("续传后内容不一致: err=%v len=%d", err, len(got))
	}
	mu.Lock()
	reqs := append([]string(nil), ranges...)
	mu.Unlock()
	resumeStart := segs[1].Start + segs[1].Done
	foundResume := false
	for _, r := range reqs {
		if strings.HasPrefix(r, "bytes=0-") {
			continue // probe
		}
		if strings.HasPrefix(r, "bytes="+strconv.FormatInt(resumeStart, 10)+"-") {
			foundResume = true
		}
	}
	if !foundResume {
		t.Fatalf("没有从断点继续请求（期望起点 %d），实际 %v", resumeStart, reqs)
	}
	if _, err := os.Stat(parallelStatePath(part)); !os.IsNotExist(err) {
		t.Fatal(".part.json 完成后应删除")
	}
}
