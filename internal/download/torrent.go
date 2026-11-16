package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/OOPQ/reelkeep/internal/proc"
)

// TorrentFile 是磁力任务里的一个文件。
type TorrentFile struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	Length int64  `json:"length"`
}

// ListTorrentFiles 只取元数据，返回磁力任务的文件列表。
func ListTorrentFiles(ctx context.Context, magnet string) ([]TorrentFile, error) {
	dir, err := os.MkdirTemp("", "reelkeep-torrentmeta-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dir
	cfg.NoUpload = true
	cfg.Seed = false
	cfg.ListenPort = 0
	cfg.DisableIPv6 = true
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	tor, err := client.AddMagnet(magnet)
	if err != nil {
		return nil, err
	}
	tor.AddTrackers([][]string{
		{"udp://tracker.opentrackr.org:1337/announce", "udp://open.stealth.si:80/announce", "udp://tracker.torrent.eu.org:451/announce"},
	})
	wait, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	select {
	case <-wait.Done():
		return nil, errors.New("等待磁力元数据超时")
	case <-tor.GotInfo():
	}
	files := tor.Files()
	out := make([]TorrentFile, 0, len(files))
	for i, f := range files {
		out = append(out, TorrentFile{Index: i, Path: f.Path(), Length: f.Length()})
	}
	return out, nil
}

func isVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mkv", ".webm", ".mov", ".m4v", ".avi", ".ts", ".flv", ".wmv":
		return true
	}
	return false
}

// runTorrent 用内置 BT 引擎下载 magnet，只下载用户勾选的文件。
func (m *Manager) runTorrent(ctx context.Context, t *Task) error {
	final, _ := m.pathsFor(t)
	if st, err := os.Stat(final); err == nil && st.Size() > 0 {
		return nil
	}
	dataDir := filepath.Join(m.paths.DataDir, "torrents", t.M.Token)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.NoUpload = true
	cfg.Seed = false
	cfg.ListenPort = 0
	cfg.DisableIPv6 = true
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("启动 BT 引擎失败: %w", err)
	}
	defer client.Close()

	tor, err := client.AddMagnet(t.M.URL)
	if err != nil {
		return fmt.Errorf("磁力链接解析失败: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tor.GotInfo():
	}
	files := tor.Files()
	if len(files) == 0 {
		return errors.New("磁力任务没有可用文件")
	}
	selected := selectTorrentFiles(files, t.M.Files)
	if len(selected) == 0 {
		return errors.New("没有选中可下载的文件")
	}
	var total int64
	for _, f := range selected {
		total += f.Length()
	}
	m.mu.Lock()
	t.M.Total = total
	m.mu.Unlock()
	for _, f := range files {
		f.SetPriority(torrent.PiecePriorityNone)
	}
	for _, f := range selected {
		f.SetPriority(torrent.PiecePriorityNormal)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var done int64
			for _, f := range selected {
				done += f.BytesCompleted()
			}
			m.updateProgress(t, done)
			if done >= total {
				goto complete
			}
		}
	}

complete:
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	primary := -1
	for i, f := range selected {
		if isVideoFile(f.Path()) {
			primary = i
			break
		}
	}
	if primary < 0 {
		return errors.New("选中的文件里没有视频")
	}
	exported := 0
	for i, f := range selected {
		if !isVideoFile(f.Path()) {
			continue
		}
		src := filepath.Join(dataDir, filepath.FromSlash(f.Path()))
		dst := final
		if i != primary {
			exported++
			dst = filepath.Join(filepath.Dir(final), t.M.Token+"_"+strconv.Itoa(exported)+".mp4")
		}
		if err := m.exportTorrentFile(ctx, src, dst); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(dataDir)
	return nil
}

func selectTorrentFiles(files []*torrent.File, indexes []int) []*torrent.File {
	if len(indexes) == 0 {
		return files
	}
	want := make(map[int]bool, len(indexes))
	for _, i := range indexes {
		want[i] = true
	}
	out := make([]*torrent.File, 0, len(indexes))
	for i, f := range files {
		if want[i] && f.Length() > 0 {
			out = append(out, f)
		}
	}
	return out
}

func (m *Manager) exportTorrentFile(ctx context.Context, src, dst string) error {
	if strings.EqualFold(filepath.Ext(src), ".mp4") {
		return copyFile(src, dst)
	}
	ff := proc.FindFFmpeg(strings.TrimSpace(m.getSettings().FFmpeg))
	if ff == "" {
		return fmt.Errorf("文件不是 mp4（%s），需要 ffmpeg 转封装", filepath.Base(src))
	}
	cmd := exec.CommandContext(ctx, ff, "-y", "-i", src, "-c", "copy", "-movflags", "+faststart", dst)
	proc.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg 转封装失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}
