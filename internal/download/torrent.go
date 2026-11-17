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
	"golang.org/x/time/rate"

	"github.com/OOPQ/reelkeep/internal/model"
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
	if cachePath != "" {
		if st, statErr := os.Stat(cachePath); statErr == nil && st.Size() > 0 {
			if tor, err := client.AddTorrentFromFile(cachePath); err == nil {
				files := tor.Files()
				out := make([]TorrentFile, 0, len(files))
				for i, f := range files {
					out = append(out, TorrentFile{Index: i, Path: f.Path(), Length: f.Length()})
				}
				return out, nil
			}
		}
	}
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
	if limit := m.getSettings().TorrentLimitKB; limit > 0 {
		cfg.DownloadRateLimiter = rate.NewLimiter(rate.Limit(limit*1024), maxInt(limit*1024, 64*1024))
	}
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

// runTorrent 使用共享会话下载一个 Job 下所有勾选文件，完成后才生成媒体条目。
func (m *Manager) runTorrent(ctx context.Context, t *Task) error {
	sess, err := m.acquireTorrent(ctx, t.M.URL)
	if err != nil {
		return err
	}
	defer m.releaseTorrent(sess)
	job, ok, err := m.st.GetJob(sess.hash)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("磁力任务不存在")
	}
	type jobItem struct {
		file *torrent.File
		meta model.TorrentJobFile
	}
	var items []jobItem
	var total int64
	for _, jf := range job.Files {
		if !jf.Selected {
			continue
		}
		f := sess.addFile(jf.Index)
		if f == nil {
			continue
		}
		items = append(items, jobItem{file: f, meta: jf})
		total += f.Length()
	}
	if len(items) == 0 {
		return errors.New("磁力任务没有可下载文件")
	}
	defer func() {
		for _, it := range items {
			sess.releaseFile(it.meta.Index)
		}
	}()
	m.mu.Lock()
	t.M.Total = total
	t.M.Name = job.Title
	m.mu.Unlock()
	_ = m.st.SaveTask(t.M)
	m.st.UpdateJobState(job.ID, "downloading")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var done int64
			all := true
			for _, it := range items {
				n := it.file.BytesCompleted()
				done += n
				if n < it.file.Length() {
					all = false
				}
				m.st.UpdateJobFile(job.ID, it.meta.Index, "downloading", n, "", "")
			}
			m.updateProgress(t, done)
			if all {
				goto complete
			}
		}
	}

complete:
	used := map[string]bool{}
	for _, it := range items {
		if !isVideoFile(it.meta.Path) {
			m.st.UpdateJobFile(job.ID, it.meta.Index, "done", it.file.Length(), "", "")
			continue
		}
		src := filepath.Join(sess.dataDir, filepath.FromSlash(it.meta.Path))
		dst := jobTargetPath(job.SaveDir, it.meta.Path, used)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(dst); err != nil {
			if err := m.exportTorrentFile(ctx, src, dst); err != nil {
				m.st.UpdateJobFile(job.ID, it.meta.Index, "failed", it.file.Length(), err.Error(), "")
				continue
			}
		}
		token := fmt.Sprintf("local@%s_%d", sess.hash, it.meta.Index)
		m.registerJobItem(token, job, it.meta, dst)
		m.st.UpdateJobFile(job.ID, it.meta.Index, "done", it.file.Length(), "", token)
	}
	m.st.UpdateJobState(job.ID, "done")
	return nil
}

func (m *Manager) registerJobItem(token string, job model.TorrentJob, f model.TorrentJobFile, file string) {
	st, err := os.Stat(file)
	if err != nil {
		return
	}
	title := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	now := time.Now().Format("2006-01-02 15:04:05")
	row := model.Row{
		Token: token, Status: "done", Source: "local", Site: "local",
		Title: title, URL: file, PageURL: filepath.Dir(file), DownloadKind: "local",
		FileName: filepath.Base(file), FilePath: file,
		FileSize: fmt.Sprintf("%d", st.Size()), FileDate: st.ModTime().Format("2006-01-02 15:04:05"),
		FirstSeen: now, LastSeen: now, Note: "auto:torrent",
	}
	if err := m.st.UpsertRows([]model.Row{row}); err != nil {
		m.logf("torrent", "写入媒体条目失败: "+err.Error())
		return
	}
	m.autoCover(token, file)
	if m.onRow != nil {
		m.onRow(token)
	}
}

func cleanSegment(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 32 {
			return -1
		}
		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" {
		return "_"
	}
	return s
}

func jobTargetPath(saveDir, rel string, used map[string]bool) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		parts[i] = cleanSegment(parts[i])
	}
	if len(parts) == 0 {
		parts = []string{"video.mp4"}
	}
	last := parts[len(parts)-1]
	if !strings.HasSuffix(strings.ToLower(last), ".mp4") {
		last = strings.TrimSuffix(last, filepath.Ext(last)) + ".mp4"
	}
	parts[len(parts)-1] = last
	relPath := filepath.Join(parts...)
	full := filepath.Join(saveDir, relPath)
	for used[strings.ToLower(full)] {
		ext := filepath.Ext(full)
		full = strings.TrimSuffix(full, ext) + "_2" + ext
	}
	used[strings.ToLower(full)] = true
	return full
}

func (m *Manager) TorrentSessionStats(id string) (peers, seeders int) {
	m.sessionMu.Lock()
	s := m.sessions[id]
	m.sessionMu.Unlock()
	if s == nil || s.tor == nil {
		return 0, 0
	}
	st := s.tor.Stats()
	return st.TotalPeers, st.ConnectedSeeders
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
