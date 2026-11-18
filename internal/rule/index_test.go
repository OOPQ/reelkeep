package rule

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestSourceIndexUpToDate 保证 sources/index.json 与源包本身同步（改包忘跑脚本会挂）。
func TestSourceIndexUpToDate(t *testing.T) {
	dir := filepath.Join("..", "..", "sources")
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatalf("读不到 index.json: %v", err)
	}
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatalf("index.json 解析失败: %v", err)
	}
	byID := map[string]IndexEntry{}
	for _, e := range idx.Sources {
		byID[e.ID] = e
	}
	seen := 0
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		if filepath.Base(f) == "index.json" {
			continue
		}
		p, err := LoadFile(f)
		if err != nil {
			t.Fatalf("%s 校验失败: %v", f, err)
		}
		seen++
		e, ok := byID[p.ID]
		if !ok {
			t.Fatalf("index.json 缺少源 %s", p.ID)
		}
		if e.Version != p.Version {
			t.Fatalf("%s 版本不一致：index=%s 包=%s（请重跑 scripts/index-sources.ps1）", p.ID, e.Version, p.Version)
		}
		sum := sha256.Sum256(mustRead(t, f))
		if got := hex.EncodeToString(sum[:]); e.SHA256 != "" && e.SHA256 != got {
			t.Fatalf("%s 的 sha256 与 index.json 不一致（请重跑 scripts/index-sources.ps1）", p.ID)
		}
	}
	if seen != len(idx.Sources) {
		t.Fatalf("index.json 有 %d 项，目录里只有 %d 个源包", len(idx.Sources), seen)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.1", "1.0.2", true},
		{"1.0.2", "1.0.1", false},
		{"1.0.2", "1.0.2", false},
		{"1.2.0", "1.10.0", true},
		{"v1.0.0", "1.0.1", true},
		{"bad", "1.0.0", false},
	}
	for _, c := range cases {
		if got := VersionLess(c.a, c.b); got != c.want {
			t.Errorf("VersionLess(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}
