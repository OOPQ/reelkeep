package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
)

// TestScrapeDiagnosticsBaseline 固定诊断目录的文件名与关键字段。
func TestScrapeDiagnosticsBaseline(t *testing.T) {
	s, _, ts, paths := newAcceptanceServer(t)
	run := "20260101-000000-demo-new"
	j := &scrapeJob{Source: "demo", Rank: "new", Done: 1, Total: 1}
	rows := []model.Row{{Token: "demo@1", Title: "Demo", URL: "https://example.com/a.mp4"}}
	s.writeScrapeDiag(run, j, rows, "ok", "", 1, 0, 0)

	dir := filepath.Join(paths.Diagnostics, "scrape", run)
	for _, name := range []string{"meta.json", "progress.json", "result.csv", "scrape.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("缺少 %s: %v", name, err)
		}
	}
	var meta map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"run", "status", "source", "rank", "count", "added", "updated", "skipped", "finishedAt", "files", "url"} {
		if _, ok := meta[k]; !ok {
			t.Fatalf("meta 缺少 %s", k)
		}
	}
	if meta["run"] != run || meta["status"] != "ok" || meta["count"].(float64) != 1 {
		t.Fatalf("meta 内容不对: %v", meta)
	}

	csv, _ := os.ReadFile(filepath.Join(dir, "result.csv"))
	text := string(csv)
	if !strings.HasPrefix(text, "\ufeff") || !strings.Contains(text, "demo@1") || !strings.Contains(text, "token") {
		t.Fatalf("result.csv 内容不对")
	}

	out := getJSON(t, ts, "/api/diagnostics/scrape")
	items, _ := out["items"].([]any)
	if out["ok"] != true || len(items) != 1 {
		t.Fatalf("诊断列表不对: %v", out)
	}
}
