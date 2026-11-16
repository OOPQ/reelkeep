package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDownloadRepair(t *testing.T) {
	ff := findFFmpeg()
	if ff == "" {
		t.Skip("没有 ffmpeg")
	}
	s, ts := newP3Server(t)
	media := filepath.Join(s.paths.Content, "local")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(media, "good.mp4")
	cmd := exec.Command(ff, "-y", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=10",
		"-t", "1", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", video)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("生成测试视频失败: %v %s", err, out)
	}
	body, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(video); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video+".part", body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "bad.mp4.part"), []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 已有正片 + 更小的 .part：应删除 .part
	if err := os.WriteFile(filepath.Join(media, "dup.mp4"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "dup.mp4.part"), []byte("0123"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(ts.URL+"/api/download/repair", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		OK        bool   `json:"ok"`
		Renamed   int    `json:"renamed"`
		Deleted   int    `json:"deleted"`
		Skipped   int    `json:"skipped"`
		Validator string `json:"validator"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Renamed != 1 || out.Deleted != 1 || out.Skipped != 1 {
		t.Fatalf("repair 结果不对: %+v", out)
	}
	if st, err := os.Stat(video); err != nil || st.Size() != int64(len(body)) {
		t.Fatalf("有效 .part 应改名为正片: %v", err)
	}
	if _, err := os.Stat(filepath.Join(media, "bad.mp4.part")); err != nil {
		t.Fatal("无效 .part 应保留")
	}
	if _, err := os.Stat(filepath.Join(media, "dup.mp4.part")); !os.IsNotExist(err) {
		t.Fatal("已有正片时 .part 应删除")
	}
}

var _ = httptest.NewServer
