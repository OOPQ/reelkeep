package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 旧版 PowerShell 写出的 config.json 带 UTF-8 BOM，必须能读。
func TestLoadSettingsWithBOM(t *testing.T) {
	dir := t.TempDir()
	p := ForRoot(dir)
	if err := os.MkdirAll(p.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"proxy":"http://127.0.0.1:7890","concurrent":4,"ffmpegPath":"D:\\ffmpeg.exe","qiseCookie":"k=v"}`)
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, body...)
	if err := os.WriteFile(filepath.Join(p.DataDir, "config.json"), withBOM, 0o644); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(p)
	if s.Proxy != "http://127.0.0.1:7890" || s.Concurrent != 4 || s.QiseCookie != "k=v" {
		t.Fatalf("BOM 配置没读对: %+v", s)
	}
}

func TestSaveSettingsOmitsEmpty(t *testing.T) {
	dir := t.TempDir()
	p := ForRoot(dir)
	if err := SaveSettings(p, Settings{Concurrent: 3}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(p.DataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("配置为空")
	}
	// 不应写入绝对路径 contentDir
	if got := string(b); got == "" || contains(got, "contentDir") {
		t.Fatalf("config.json 不应包含 contentDir: %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
