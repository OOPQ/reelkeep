package download

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

func TestPauseAllResumeAll(t *testing.T) {
	body := make([]byte, 1<<20)
	var mu sync.Mutex
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		start := 0
		if v := r.Header.Get("Range"); strings.HasPrefix(v, "bytes=") {
			fmt.Sscanf(v, "bytes=%d-", &start)
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)-start))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.WriteHeader(http.StatusPartialContent)
		}
		for i := start; i < len(body); i += 8192 {
			end := i + 8192
			if end > len(body) {
				end = len(body)
			}
			if _, err := w.Write(body[i:end]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	row := model.Row{Token: "pauseall", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	if _, err := m.Enqueue(model.DownloadTask{Token: "pauseall", Source: "demo", URL: row.URL, Mode: "http", State: "queued"}); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(paths.Content, "demo", "pauseall.mp4.part")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(part); err == nil && st.Size() > 64*1024 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := m.Pause(true, nil); n != 1 {
		t.Fatalf("pause all n=%d", n)
	}
	waitState(t, m, "pauseall", "paused", 5*time.Second)
	if n := m.Pause(false, nil); n != 1 {
		t.Fatalf("resume all n=%d", n)
	}
	waitState(t, m, "pauseall", "done", 20*time.Second)
	got, err := os.ReadFile(filepath.Join(paths.Content, "demo", "pauseall.mp4"))
	if err != nil || len(got) != len(body) {
		t.Fatalf("最终文件不完整: %v %d", err, len(got))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) < 2 || ranges[len(ranges)-1] == "" {
		t.Fatalf("全局暂停后没有续传: %v", ranges)
	}
}
