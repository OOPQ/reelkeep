package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
	"github.com/OOPQ/reelkeep/internal/store"
)

func findFFmpeg() string {
	for _, p := range []string{`D:\software\oopz\ffmpeg.exe`} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	return ""
}

func newP3Server(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	paths := config.ForRoot(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(paths, st, source.EmptyRegistry(), nil, nil, config.Settings{
		Concurrent: 1, FFmpegPath: findFFmpeg(),
	}, nil)
	if err := srv.Boot(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = st.Close()
	})
	return srv, ts
}

func reloadRows(t *testing.T, s *Server) {
	t.Helper()
	rows, err := s.st.LoadRows()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.rows = rows
	s.mu.Unlock()
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func makeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 80, G: 120, B: 200, A: 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImportProjectFiles(t *testing.T) {
	s, _ := newP3Server(t)
	media := filepath.Join(s.paths.Content, "local", "a.mp4")
	if err := os.MkdirAll(filepath.Dir(media), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, skipped, err := s.importProjectFiles()
	if err != nil || added != 1 || skipped != 0 {
		t.Fatalf("首次导入应为 1/0，实际 %d/%d %v", added, skipped, err)
	}
	added, skipped, err = s.importProjectFiles()
	if err != nil || added != 0 || skipped != 1 {
		t.Fatalf("二次导入应跳过，实际 %d/%d %v", added, skipped, err)
	}
	rows, _ := s.st.LoadRows()
	if len(rows) != 1 || rows[0].Token != "a" || rows[0].Status != "done" || rows[0].FileName != "a.mp4" {
		t.Fatalf("导入行不对: %+v", rows)
	}
}

func TestImportLocalMovesFiles(t *testing.T) {
	s, ts := newP3Server(t)
	ext := t.TempDir()
	if err := os.WriteFile(filepath.Join(ext, "b.mp4"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := postJSON(t, ts, "/api/import/local", map[string]any{"path": ext})
	if out["ok"] != true {
		t.Fatalf("导入失败: %v", out)
	}
	if int(out["moved"].(float64)) != 1 || int(out["added"].(float64)) != 1 {
		t.Fatalf("计数不对: %v", out)
	}
	if _, err := os.Stat(filepath.Join(s.paths.Content, "local", "b.mp4")); err != nil {
		t.Fatalf("文件没有移动到 media/local: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ext, "b.mp4")); !os.IsNotExist(err) {
		t.Fatal("源文件应已移走")
	}
}

func TestCoverRepairRemoteAndThumb(t *testing.T) {
	s, ts := newP3Server(t)
	row := model.Row{Token: "c1", Status: "new", Source: "local", Title: "C1", CoverURL: "https://example.com/c.jpg"}
	if err := s.st.UpsertRows([]model.Row{row}); err != nil {
		t.Fatal(err)
	}
	reloadRows(t, s)
	s.imageFetch = func(string) ([]byte, error) { return makeJPEG(t), nil }

	out := postJSON(t, ts, "/api/covers/repair", map[string]any{"limit": 5})
	if out["ok"] != true || int(out["fixed"].(float64)) < 1 {
		t.Fatalf("封面修复失败: %v", out)
	}
	if st, err := os.Stat(s.coverPath("c1")); err != nil || st.Size() < 1024 {
		t.Fatalf("封面文件不对: %v", err)
	}
	if st, err := os.Stat(s.thumbPath("c1")); err != nil || st.Size() == 0 {
		t.Fatalf("缩略图未生成: %v", err)
	}
}

func TestSnapshotCreateCoverAndDelete(t *testing.T) {
	ff := findFFmpeg()
	if ff == "" {
		t.Skip("没有 ffmpeg")
	}
	s, ts := newP3Server(t)
	video := filepath.Join(s.paths.Content, "local", "snap.mp4")
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ff, "-y", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=10",
		"-t", "1", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", video)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("生成测试视频失败: %v %s", err, out)
	}
	row := model.Row{Token: "snap", Status: "done", Source: "local", FileName: "snap.mp4", FilePath: video}
	if err := s.st.UpsertRows([]model.Row{row}); err != nil {
		t.Fatal(err)
	}
	reloadRows(t, s)

	out := postJSON(t, ts, "/api/snapshot", map[string]any{"token": "snap", "time": 0.2})
	if out["ok"] != true || out["name"] == nil {
		t.Fatalf("创建快照失败: %v", out)
	}
	name := out["name"].(string)

	deadline := time.Now().Add(30 * time.Second)
	for {
		jobs := s.snapshotJobs("snap")
		if len(jobs) == 1 && jobs[0].Status != "running" {
			if jobs[0].Status != "done" {
				t.Fatalf("快照失败: %+v", jobs[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("快照超时: %+v", s.snapshotJobs("snap"))
		}
		time.Sleep(100 * time.Millisecond)
	}
	snapPath := filepath.Join(s.paths.Snapshots, name)
	if st, err := os.Stat(snapPath); err != nil || st.Size() == 0 {
		t.Fatalf("快照文件不存在: %v", err)
	}

	if out := postJSON(t, ts, "/api/snapshot/cover", map[string]any{"token": "snap", "name": name}); out["ok"] != true {
		t.Fatalf("设为封面失败: %v", out)
	}
	resp, err := http.Get(ts.URL + "/api/cover?token=snap")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(snapPath)
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(got, want) {
		t.Fatalf("封面没有使用快照: status=%d len=%d/%d", resp.StatusCode, len(got), len(want))
	}

	// 遮罩：删除快照后 cover-map 也清掉
	if out := postJSON(t, ts, "/api/snapshot/delete", map[string]any{"token": "snap", "name": name}); out["ok"] != true {
		t.Fatalf("删除快照失败: %v", out)
	}
	if _, err := os.Stat(snapPath); !os.IsNotExist(err) {
		t.Fatal("快照文件应已删除")
	}
	if s.readCoverMap()["snap"] != "" {
		t.Fatal("cover-map 应清理")
	}
}

func TestPlayCountOnly(t *testing.T) {
	s, ts := newP3Server(t)
	video := filepath.Join(s.paths.Content, "local", "p.mp4")
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := model.Row{Token: "p", Status: "done", Source: "local", FileName: "p.mp4", FilePath: video}
	if err := s.st.UpsertRows([]model.Row{row}); err != nil {
		t.Fatal(err)
	}
	reloadRows(t, s)

	out := postJSON(t, ts, "/api/play", map[string]any{"token": "p", "countOnly": true})
	if out["ok"] != true {
		t.Fatalf("播放计数失败: %v", out)
	}
	got, ok, _ := s.st.GetRow("p")
	if !ok || got.PlayCount != "1" || got.LastPlay == "" {
		t.Fatalf("play_count 没有写回: %+v", got)
	}
}
