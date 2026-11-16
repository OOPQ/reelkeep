package store

import (
	"path/filepath"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}
	defer st.Close()

	rows := []model.Row{
		{Token: "a", Status: "new", Title: "A", FirstSeen: "2026-11-01 10:00:00"},
		{Token: "b", Status: "done", Title: "B", FileName: "b.mp4"},
	}
	if err := st.UpsertRows(rows); err != nil {
		t.Fatalf("写入: %v", err)
	}
	got, err := st.LoadRows()
	if err != nil || len(got) != 2 {
		t.Fatalf("读取: %v, %d 行", err, len(got))
	}

	if err := st.SetFav([]string{"a"}, true); err != nil {
		t.Fatal(err)
	}
	r, ok, _ := st.GetRow("a")
	if !ok || r.Fav != "1" {
		t.Fatalf("收藏没写进去: %+v", r)
	}

	if err := st.Trash([]string{"b"}); err != nil {
		t.Fatal(err)
	}
	r, _, _ = st.GetRow("b")
	if r.Status != "recycled" || r.RecycledFrom != "done" || r.RecycledAt == "" {
		t.Fatalf("回收站应记原状态: %+v", r)
	}
	if err := st.Restore([]string{"b"}); err != nil {
		t.Fatal(err)
	}
	r, _, _ = st.GetRow("b")
	if r.Status != "done" || r.RecycledFrom != "" {
		t.Fatalf("恢复应回到 done: %+v", r)
	}

	cid, err := st.CreateCollection("合集A", "#4f8cff")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToCollection(cid, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	tokens, _ := st.CollectionTokens(cid)
	if len(tokens) != 2 {
		t.Fatalf("合集应有 2 条，实际 %v", tokens)
	}
	per, _ := st.CollectionsForToken("a")
	if !per[cid] {
		t.Fatal("应能查到条目所属合集")
	}
	if _, err := st.CreateCollection("合集A", "#000"); err == nil {
		t.Fatal("重名合集的唯一索引应生效")
	}

	if err := st.RemovePermanently([]string{"a"}, []model.DeletedToken{{Token: "a", DeletedAt: "2026-11-16 10:00:00"}}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.LoadRows()
	if len(got) != 1 {
		t.Fatalf("彻底删除后应剩 1 行，实际 %d", len(got))
	}
	del := st.LoadDeleted(dir)
	if _, ok := del["a"]; !ok {
		t.Fatal("彻底删除应写入 deleted_tokens")
	}
}

func TestOpenCreatesSchema(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cols := map[string]bool{}
	rows, err := st.DB().Query("PRAGMA table_info(items)")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil {
			cols[name] = true
		}
	}
	rows.Close()
	for _, c := range model.Columns {
		if !cols[c] {
			t.Fatalf("items 表缺少列 %s", c)
		}
	}
}
