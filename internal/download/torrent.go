package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

var publicTrackers = [][]string{
	{"udp://tracker.opentrackr.org:1337/announce", "udp://open.stealth.si:80/announce", "udp://tracker.torrent.eu.org:451/announce"},
}

type torrentSession struct {
	hash    string
	client  *torrent.Client
	tor     *torrent.Torrent
	dataDir string
	ready   chan struct{}
	err     error
	refs    int
	mu      sync.Mutex
	files   map[int]int
}

func saveMetainfo(tor *torrent.Torrent, path string) {
	if tor == nil || path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	mi := tor.Metainfo()
	_ = mi.Write(f)
	_ = f.Close()
}

// ListTorrentFiles 只取元数据，返回磁力任务的文件列表；cachePath 非空时缓存 metainfo。
func ListTorrentFiles(ctx context.Context, magnet, cachePath string) ([]TorrentFile, error) {
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
	tor.AddTrackers(publicTrackers)
	wait, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	select {
	case <-wait.Done():
		return nil, errors.New("等待磁力元数据超时")
	case <-tor.GotInfo():
	}
	saveMetainfo(tor, cachePath)
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

// TorrentHash 从 magnet 提取 btih。
func TorrentHash(magnet string) string {
	u, err := url.Parse(magnet)
	if err == nil {
		for _, xt := range u.Query()["xt"] {
			if strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
				return strings.ToLower(strings.TrimSpace(xt[len("urn:btih:"):]))
			}
		}
	}
	return "unknown"
}

func (m *Manager) acquireTorrent(ctx context.Context, magnet string) (*torrentSession, error) {
	hash := TorrentHash(magnet)
	m.sessionMu.Lock()
	if m.sessions == nil {
		m.sessions = map[string]*torrentSession{}
	}
	s := m.sessions[hash]
	if s == nil {
		s = &torrentSession{
			hash:    hash,
			dataDir: filepath.Join(m.paths.DataDir, "torrents", hash),
			ready:   make(chan struct{}),
			files:   map[int]int{},
		}
		m.sessions[hash] = s
		m.sessionMu.Unlock()
		m.initTorrentSession(s, magnet)
	} else {
		m.sessionMu.Unlock()
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	m.sessionMu.Lock()
	s.refs++
	m.sessionMu.Unlock()
	return s, nil
}

func (m *Manager) initTorrentSession(s *torrentSession, magnet string) {
	defer close(s.ready)
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		s.err = err
		return
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = s.dataDir
	cfg.Seed = false
	cfg.ListenPort = 0
	cfg.DisableIPv6 = false
	client, err := torrent.NewClient(cfg)
	if err != nil {
		s.err = fmt.Errorf("启动 BT 引擎失败: %w", err)
		return
	}
	cache := filepath.Join(s.dataDir, "info.torrent")
	var tor *torrent.Torrent
	if st, statErr := os.Stat(cache); statErr == nil && st.Size() > 0 {
		tor, _ = client.AddTorrentFromFile(cache)
	}
	if tor == nil {
		tor, err = client.AddMagnet(magnet)
		if err != nil {
			client.Close()
			s.err = fmt.Errorf("磁力链接解析失败: %w", err)
			return
		}
		tor.AddTrackers(publicTrackers)
		select {
		case <-time.After(90 * time.Second):
			client.Close()
			s.err = errors.New("等待磁力元数据超时")
			return
		case <-tor.GotInfo():
		}
		saveMetainfo(tor, cache)
	}
	s.client, s.tor = client, tor
}

func (m *Manager) releaseTorrent(s *torrentSession) {
	m.sessionMu.Lock()
	s.refs--
	if s.refs > 0 {
		m.sessionMu.Unlock()
		return
	}
	delete(m.sessions, s.hash)
	m.sessionMu.Unlock()
	if s.client != nil {
		s.client.Close()
	}
}

func (s *torrentSession) addFile(index int) *torrent.File {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[index]++
	files := s.tor.Files()
	if index < 0 || index >= len(files) {
		return nil
	}
	f := files[index]
	f.SetPriority(torrent.PiecePriorityNormal)
	return f
}

func (s *torrentSession) releaseFile(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[index]--
	if s.files[index] <= 0 {
		delete(s.files, index)
		files := s.tor.Files()
		if index >= 0 && index < len(files) {
			files[index].SetPriority(torrent.PiecePriorityNone)
		}
	}
}

// runTorrent 用同一个 infohash 的共享 BT 会话下载当前任务文件。
func (m *Manager) runTorrent(ctx context.Context, t *Task) error {
	final, _ := m.pathsFor(t)
	if st, err := os.Stat(final); err == nil && st.Size() > 0 {
		return nil
	}
	sess, err := m.acquireTorrent(ctx, t.M.URL)
	if err != nil {
		return err
	}
	defer m.releaseTorrent(sess)
	index := 0
	if len(t.M.Files) > 0 {
		index = t.M.Files[0]
	}
	file := sess.addFile(index)
	if file == nil {
		return errors.New("磁力任务没有这个文件")
	}
	defer sess.releaseFile(index)
	if file.Length() <= 0 {
		return errors.New("磁力文件为空")
	}
	m.mu.Lock()
	t.M.Total = file.Length()
	m.mu.Unlock()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	tick := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			tick++
			done := file.BytesCompleted()
			m.updateProgress(t, done)
			if tick%10 == 0 {
				st := sess.tor.Stats()
				m.logf("torrent", fmt.Sprintf("hash=%s bytes=%d total=%d peers=%d seeders=%d", sess.hash, done, file.Length(), st.TotalPeers, st.ConnectedSeeders))
			}
			if done >= file.Length() {
				goto complete
			}
		}
	}

complete:
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	src := filepath.Join(sess.dataDir, filepath.FromSlash(file.Path()))
	if !isVideoFile(file.Path()) {
		return errors.New("选中的文件不是视频")
	}
	m.mu.Lock()
	t.M.Name = filepath.Base(file.Path())
	m.mu.Unlock()
	_ = m.st.SaveTask(t.M)
	final = m.finalPath(t)
	if err := m.exportTorrentFile(ctx, src, final); err != nil {
		return err
	}
	return nil
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
