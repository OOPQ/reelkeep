package download

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

func TestPerSourceConcurrency(t *testing.T) {
	content := testContent(256 << 10)
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
		time.Sleep(120 * time.Millisecond)
		http.ServeContent(w, r, "a.mp4", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()

	const n = 6
	rows := make([]model.Row, 0, n)
	tasks := make([]model.DownloadTask, 0, n)
	for i := 0; i < n; i++ {
		token := fmt.Sprintf("s%d", i)
		rows = append(rows, model.Row{Token: token, Status: "new", Source: "demo", URL: srv.URL + "/a.mp4"})
		tasks = append(tasks, model.DownloadTask{Token: token, Source: "demo", URL: srv.URL + "/a.mp4", Mode: "http", State: "queued"})
	}
	m, _, _ := newTestManager(t, source.EmptyRegistry(), rows...)
	m.SetSettings(Settings{Concurrent: 8, PerSourceConcurrent: 3, HTTPConnections: 1, RetryCount: 2, RetryDelay: 10 * time.Millisecond})
	for _, task := range tasks {
		if _, err := m.Enqueue(task); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		waitState(t, m, fmt.Sprintf("s%d", i), "done", 20*time.Second)
	}
	mu.Lock()
	max := maxActive
	mu.Unlock()
	if max != 3 {
		t.Fatalf("每来源最大并发 = %d，期望 3", max)
	}
}
