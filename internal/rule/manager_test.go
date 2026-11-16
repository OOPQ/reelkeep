package rule

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/OOPQ/reelkeep/internal/source"
)

func TestManagerImportEnableRemove(t *testing.T) {
	dir := t.TempDir()
	reg := source.EmptyRegistry()
	m := NewManager(dir, reg)
	if errs := m.LoadAll(); len(errs) != 0 {
		t.Fatalf("LoadAll: %v", errs)
	}
	data := readFile(t, "demo.json")

	p, err := m.Import(data, false)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if p.ID != "demo" {
		t.Fatalf("id 不对: %s", p.ID)
	}
	if _, ok := os.ReadFile(filepath.Join(dir, "demo.json")); ok != nil {
		t.Fatalf("文件没写进去: %v", ok)
	}
	if _, ok := reg.Get("demo"); !ok {
		t.Fatal("导入后应出现在注册表")
	}
	if list := m.List(); len(list) != 1 || !list[0].Enabled {
		t.Fatalf("列表不对: %+v", list)
	}

	// 重复导入不带 overwrite 应报 ErrExists
	_, err = m.Import(data, false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("应返回 ErrExists，实际 %v", err)
	}
	if _, err := m.Import(data, true); err != nil {
		t.Fatalf("覆盖导入失败: %v", err)
	}

	// 禁用后应从注册表消失，且状态落盘
	if err := m.SetEnabled("demo", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("demo"); ok {
		t.Fatal("禁用后不应还在注册表")
	}

	m2 := NewManager(dir, reg)
	if errs := m2.LoadAll(); len(errs) != 0 {
		t.Fatalf("重新加载: %v", errs)
	}
	list := m2.List()
	if len(list) != 1 || list[0].Enabled {
		t.Fatalf("禁用状态没持久化: %+v", list)
	}

	// 导出内容应可再次解析
	raw, err := m2.Export("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw, "export.json"); err != nil {
		t.Fatalf("导出的 JSON 无法解析: %v", err)
	}

	if err := m2.Remove("demo"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("demo"); ok {
		t.Fatal("删除后不应还在注册表")
	}
	if _, err := os.Stat(filepath.Join(dir, "demo.json")); !os.IsNotExist(err) {
		t.Fatalf("文件应已删除: %v", err)
	}
}

func TestManagerRejectsBuiltinCollision(t *testing.T) {
	dir := t.TempDir()
	reg := source.EmptyRegistry()
	reg.Register("demo", func() source.Source { return nil })
	m := NewManager(dir, reg)
	_, err := m.Import(readFile(t, "demo.json"), false)
	if err == nil {
		t.Fatal("与内置实现同 id 应被拒绝")
	}
}

func TestManagerRecordResult(t *testing.T) {
	dir := t.TempDir()
	reg := source.EmptyRegistry()
	m := NewManager(dir, reg)
	if _, err := m.Import(readFile(t, "demo.json"), false); err != nil {
		t.Fatal(err)
	}
	m.RecordResult("demo", errors.New("boom"))
	list := m.List()
	if list[0].LastError != "boom" || list[0].LastRunOK {
		t.Fatalf("失败结果没记录: %+v", list[0])
	}
	m.RecordResult("demo", nil)
	list = m.List()
	if list[0].LastError != "" || !list[0].LastRunOK {
		t.Fatalf("成功结果没记录: %+v", list[0])
	}
}
