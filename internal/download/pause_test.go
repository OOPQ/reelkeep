package download

import (
	"fmt"
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

func TestPauseResumeSingleItem(t *testing.T) {
	body := make([]byte, 1<<20)
	for i := range body {
		body[i] = byte(i)
	}
	var mu sync.Mutex
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		start := 0
		if v := r.Header.Get("Range"); strings.HasPrefix(v, "bytes=") {
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(v, "bytes="), "-")); err == nil {
				start = n
			}
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

	row := model.Row{Token: "pausetest", Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"}
	m, _, paths := newTestManager(t, source.EmptyRegistry(), row)
	if _, err := m.Enqueue(model.DownloadTask{Token: "pausetest", Source: "demo", URL: row.URL, Mode: "http", State: "queued"}); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(paths.Content, "demo", "pausetest.mp4.part")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(part); err == nil && st.Size() > 64*1024 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st, err := os.Stat(part); err != nil || st.Size() == 0 {
		t.Fatalf("没有开始写入 .part: %v", err)
	}
	if n := m.Pause(true, []string{"pausetest"}); n != 1 {
		t.Fatalf("pause n=%d", n)
	}
	waitState(t, m, "pausetest", "paused", 5*time.Second)
	st, err := os.Stat(part)
	if err != nil || st.Size() == 0 || st.Size() >= int64(len(body)) {
		t.Fatalf("暂停后 .part 大小异常: %v %d", err, func() int64 {
			if st != nil {
				return st.Size()
			}
			return 0
		}())
	}
	if n := m.Pause(false, []string{"pausetest"}); n != 1 {
		t.Fatalf("resume n=%d", n)
	}
	waitState(t, m, "pausetest", "done", 20*time.Second)
	got, err := os.ReadFile(filepath.Join(paths.Content, "demo", "pausetest.mp4"))
	if err != nil || len(got) != len(body) {
		t.Fatalf("最终文件不完整: %v %d", err, len(got))
	}
	mu.Lock()
	defer mu.Unlock()
	sawRange := false
	for _, h := range ranges {
		if strings.HasPrefix(h, "bytes=") && h != "bytes=0-" {
			sawRange = true
		}
	}
	if !sawRange {
		t.Fatalf("继续下载没有发 Range: %v", ranges)
	}
}
