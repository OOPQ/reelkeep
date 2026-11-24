package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
)

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuild(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.mp4"))
	mustWrite(t, filepath.Join(dir, "a (1).mp4"))
	mustWrite(t, filepath.Join(dir, "sub", "b.mp4"))
	mustWrite(t, filepath.Join(dir, "c.part"))

	idx := Build(dir)
	if !idx.Exists {
		t.Fatal("目录存在时 Exists 应为 true")
	}
	if len(idx.Files) != 2 {
		t.Fatalf("期望 2 个视频，实际 %d: %v", len(idx.Files), idx.Files)
	}
	if _, ok := idx.Files["a"]; !ok {
		t.Fatal("应识别 a.mp4")
	}
	if _, ok := idx.Files["b"]; !ok {
		t.Fatal("应递归识别 sub/b.mp4")
	}
	if got := idx.Files["a"].Name; got != "a.mp4" {
		t.Fatalf("重名后缀应被去掉且先到先得，实际 %q", got)
	}
	if len(idx.Partial) != 1 {
		t.Fatalf("期望 1 个 .part，实际 %d", len(idx.Partial))
	}
}

func TestBuildMissingDir(t *testing.T) {
	idx := Build(filepath.Join(t.TempDir(), "nope"))
	if idx.Exists {
		t.Fatal("目录不存在时 Exists 应为 false")
	}
	if len(idx.Files) != 0 {
		t.Fatal("不应返回文件")
	}
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.mp4"))
	mustWrite(t, filepath.Join(dir, "b.mp4")) // 台账里没有 → 孤立文件

	idx := Build(dir)
	rows := []model.Row{
		{Token: "a", Status: "new", Title: "A"},
		{Token: "c", Status: "done", Note: "auto:file", FileName: "c.mp4", FilePath: filepath.Join(dir, "c.mp4")},
	}
	dirty, found, orphans := idx.Apply(rows, false)

	if found != 1 {
		t.Fatalf("命中文件数应为 1，实际 %d", found)
	}
	if rows[0].Status != "done" || rows[0].FileName != "a.mp4" {
		t.Fatalf("有文件的行应标 done 并回填文件名：%+v", rows[0])
	}
	if rows[1].FileName != "" || rows[1].Status != "new" {
		t.Fatalf("文件消失的自动 done 行应回退 new：%+v", rows[1])
	}
	if len(orphans) != 1 || orphans[0].Name != "b.mp4" {
		t.Fatalf("应报告孤立文件 b.mp4，实际 %+v", orphans)
	}
	if len(dirty) != 2 {
		t.Fatalf("两行都应落库，实际 %d", len(dirty))
	}
}

func TestApplyKeepsStatusWhenDirMissing(t *testing.T) {
	idx := Index{Files: map[string]Entry{}}
	rows := []model.Row{{Token: "a", Status: "done", Note: "auto:file", FileName: "a.mp4"}}
	dirty, found, _ := idx.Apply(rows, false)
	if len(dirty) != 0 || found != 0 {
		t.Fatal("目录不可达时不应改台账")
	}
	if rows[0].Status != "done" || rows[0].FileName != "a.mp4" {
		t.Fatalf("目录不可达时不应清状态：%+v", rows[0])
	}
}

func TestApplySourceIDFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "42.mp4")) // 迅雷常见：只存 source_id
	rows := []model.Row{{Token: "demo@42", Source: "demo", SourceID: "42", Status: "new"}}
	dirty, found, _ := Build(dir).Apply(rows, false)
	if found != 1 || len(dirty) == 0 || rows[0].Status != "done" || rows[0].FileName != "42.mp4" {
		t.Fatalf("source_id 文件未绑定: found=%d dirty=%d row=%+v", found, len(dirty), rows[0])
	}
}

func TestApplySourceHintDisambiguates(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a", "42.mp4"))
	rows := []model.Row{
		{Token: "a@42", Source: "a", SourceID: "42", Status: "new"},
		{Token: "b@42", Source: "b", SourceID: "42", Status: "new"},
	}
	Build(dir).Apply(rows, false)
	if rows[0].Status != "done" || rows[0].FileName != "42.mp4" {
		t.Fatalf("目录来源 a 应绑定: %+v", rows[0])
	}
	if rows[1].Status == "done" || rows[1].FileName != "" {
		t.Fatalf("来源 b 不应被误绑: %+v", rows[1])
	}
}

func TestSourceKeys(t *testing.T) {
	keys := SourceKeys(model.Row{Token: "demo@42", Source: "demo", SourceID: "42", URL: "https://cdn.example.com/v/xyz.mp4?tag=1"})
	want := []string{"demo@42", model.MediaUID("demo", "42", "demo@42"), "42", "xyz"}
	if len(keys) != len(want) {
		t.Fatalf("keys=%v，期望 %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys=%v，期望 %v", keys, want)
		}
	}
}

func TestApplyStableMediaUID(t *testing.T) {
	dir := t.TempDir()
	uid := model.MediaUID("demo", "42", "demo@42")
	mustWrite(t, filepath.Join(dir, uid+".mp4"))
	rows := []model.Row{{Token: "demo@42", Source: "demo", SourceID: "42", Status: "new"}}
	dirty, found, _ := Build(dir).Apply(rows, false)
	if found != 1 || len(dirty) != 1 || rows[0].Status != "done" || rows[0].FileName != uid+".mp4" {
		t.Fatalf("stable media uid not matched: found=%d dirty=%d row=%+v", found, len(dirty), rows[0])
	}
}
