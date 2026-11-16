package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveGetDelete(t *testing.T) {
	s := New(t.TempDir())
	if _, ok, _ := s.Get("qise"); ok {
		t.Fatal("初始不应有凭据")
	}
	const cookie = "PHPSESSID=secret-value-123; token=abc"
	if err := s.Save("qise", cookie); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	rec, ok, err := s.Get("qise")
	if err != nil || !ok {
		t.Fatalf("读取失败: %v ok=%v", err, ok)
	}
	if rec.Cookie != cookie {
		t.Fatalf("Cookie 不一致: %q", rec.Cookie)
	}
	if rec.UpdatedAt == "" {
		t.Fatal("应记录更新时间")
	}
	has, at := s.Info("qise")
	if !has || at == "" {
		t.Fatal("Info 不对")
	}

	// 落盘文件不能出现明文
	b, err := os.ReadFile(filepath.Join(s.dir, "qise.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret-value-123") {
		t.Fatal("凭据文件里出现了明文 Cookie")
	}

	if err := s.Delete("qise"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("qise"); ok {
		t.Fatal("删除后不应还能读到")
	}
}

func TestRejectsBadInput(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Save("Bad/Id", "x"); err == nil {
		t.Fatal("非法源 id 应被拒绝")
	}
	if err := s.Save("qise", "   "); err == nil {
		t.Fatal("空 Cookie 应被拒绝")
	}
	if err := s.Save("qise", strings.Repeat("a", 9000)); err == nil {
		t.Fatal("超长 Cookie 应被拒绝")
	}
	// 换行会被清掉，不会写坏请求头
	if err := s.Save("qise", "a=1\r\nb=2"); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := s.Get("qise")
	if strings.ContainsAny(rec.Cookie, "\r\n") {
		t.Fatalf("Cookie 应已清理换行: %q", rec.Cookie)
	}
}
