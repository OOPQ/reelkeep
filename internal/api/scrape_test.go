package api

import (
	"path/filepath"
	"testing"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/store"
)

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	root := t.TempDir()
	paths := config.ForRoot(root)
	st, err := store.Open(filepath.Join(paths.DataDir, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{paths: paths, st: st, deleted: map[string]model.DeletedToken{}}
	return s, func() { _ = st.Close() }
}

func TestApplyScrapeMergesWithoutClobberingUserFields(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	s.rows = []model.Row{
		{Token: "old", Status: "done", Fav: "1", Rating: "5", Title: "旧标题", FileName: "old.mp4"},
		{Token: "gone", Status: "recycled", Title: "回收站"},
	}
	if err := s.st.UpsertRows(s.rows); err != nil {
		t.Fatal(err)
	}
	s.deleted["dead"] = model.DeletedToken{Token: "dead"}

	added, updated, skipped, _, err := s.applyScrape([]model.Row{
		{Token: "old", Title: "新标题", URL: "https://x/old.mp4", CoverURL: "https://x/old.jpg"},
		{Token: "gone", Title: "回收站", URL: "https://x/gone.mp4"},
		{Token: "dead", Title: "已删除", URL: "https://x/dead.mp4"},
		{Token: "new", Title: "新条目", URL: "https://x/new.mp4"},
		{Token: "nourl", Title: "没有播放地址"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || updated != 1 || skipped != 3 {
		t.Fatalf("计数不对: added=%d updated=%d skipped=%d", added, updated, skipped)
	}

	rows, err := s.st.LoadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 { // old + new + gone
		t.Fatalf("应有 3 行，实际 %d", len(rows))
	}
	for _, r := range rows {
		switch r.Token {
		case "old":
			if r.Title != "新标题" || r.Status != "done" || r.Fav != "1" || r.Rating != "5" {
				t.Fatalf("用户字段被覆盖了: %+v", r)
			}
			if r.FileName != "old.mp4" {
				t.Fatalf("文件字段不应被清空: %+v", r)
			}
			if r.LastSeen == "" {
				t.Fatal("last_seen 应更新")
			}
		case "new":
			if r.Status != "new" || r.URL == "" {
				t.Fatalf("新条目不对: %+v", r)
			}
		}
	}
}

func TestApplyScrapeSkipDeletedFalse(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()
	s.deleted["dead"] = model.DeletedToken{Token: "dead"}
	added, _, skipped, _, err := s.applyScrape([]model.Row{
		{Token: "dead", Title: "已删除", URL: "https://x/dead.mp4"},
	}, false)
	if err != nil || added != 1 || skipped != 0 {
		t.Fatalf("跳过已删除关闭时应入库: added=%d skipped=%d err=%v", added, skipped, err)
	}
}
