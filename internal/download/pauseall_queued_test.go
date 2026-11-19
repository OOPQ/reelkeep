package download

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// TestPauseAllIncludesQueued 全局暂停应把排队中的任务也置为暂停；
// 单条继续只恢复该条，其余保持暂停。
func TestPauseAllIncludesQueued(t *testing.T) {
	body := make([]byte, 2<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := 0
		if v := r.Header.Get("Range"); strings.HasPrefix(v, "bytes=") {
			fmt.Sscanf(v, "bytes=%d-", &start)
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)-start))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.WriteHeader(http.StatusPartialContent)
		}
		for i := start; i < len(body); i += 4096 {
			end := i + 4096
			if end > len(body) {
				end = len(body)
			}
			if _, err := w.Write(body[i:end]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer srv.Close()

	rows := []model.Row{
		{Token: "one", Status: "new", Source: "demo", URL: srv.URL + "/1.mp4"},
		{Token: "two", Status: "new", Source: "demo", URL: srv.URL + "/2.mp4"},
		{Token: "three", Status: "new", Source: "demo", URL: srv.URL + "/3.mp4"},
	}
	m, _, _ := newTestManager(t, source.EmptyRegistry(), rows...)
	for _, r := range rows {
		if _, err := m.Enqueue(model.DownloadTask{Token: r.Token, Source: "demo", URL: r.URL, Mode: "http", State: "queued"}); err != nil {
			t.Fatal(err)
		}
	}
	waitState(t, m, "one", "downloading", 10*time.Second)

	if n := m.Pause(true, nil); n < 3 {
		t.Fatalf("pause all n=%d，期望 3（含排队中的）", n)
	}
	for _, tk := range []string{"one", "two", "three"} {
		waitState(t, m, tk, "paused", 5*time.Second)
	}

	if n := m.Pause(false, []string{"two"}); n != 1 {
		t.Fatalf("resume one n=%d", n)
	}
	waitState(t, m, "two", "downloading", 10*time.Second)
	time.Sleep(150 * time.Millisecond)
	waitState(t, m, "one", "paused", 2*time.Second)
	waitState(t, m, "three", "paused", 2*time.Second)
	m.Cancel(nil)
	time.Sleep(300 * time.Millisecond)
}
