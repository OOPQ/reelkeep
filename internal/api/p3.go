package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/cover"
	"github.com/OOPQ/reelkeep/internal/jsonx"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/netproxy"
	"github.com/OOPQ/reelkeep/internal/proc"
	"github.com/OOPQ/reelkeep/internal/source"
)

var dupSuffix = regexp.MustCompile(`\s*\(\d+\)\s*$`)

// ---------- 项目 / 本地导入 ----------

func (s *Server) handleProjectOpen(w http.ResponseWriter, r *http.Request) {
	if err := openPath(s.paths.Root); err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": s.paths.Root})
}

func (s *Server) handleProjectImport(w http.ResponseWriter, r *http.Request) {
	added, skipped, err := s.importProjectFiles()
	if err != nil {
		bad(w, err.Error())
		return
	}
	// 识别已有文件不只是「补新行」：台账里已有、但还没绑文件的条目也要同步绑定。
	changed, err := s.Scan(false)
	if err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("project", fmt.Sprintf("识别已有媒体：新增 %d，跳过 %d，绑定 %d", added, skipped, changed))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": added, "skipped": skipped, "changed": changed})
}

// importProjectFiles 扫描 media/ 里台账没有的 mp4，补成 done 记录。
func (s *Server) importProjectFiles() (int, int, error) {
	files := walkMP4(s.paths.Content, 4)
	if len(files) == 0 {
		return 0, 0, nil
	}
	s.mu.RLock()
	known := make(map[string]bool, len(s.rows)*2)
	for i := range s.rows {
		known[strings.ToLower(s.rows[i].Token)] = true
		// 已下载文件按稳定 uid 命名，扫盘时也要能认出来，避免重复登记
		if uid := model.MediaUID(s.rows[i].Source, s.rows[i].SourceID, s.rows[i].Token); uid != "" {
			known[strings.ToLower(uid)] = true
		}
	}
	s.mu.RUnlock()

	now := time.Now().Format("2006-01-02 15:04:05")
	var addedRows []model.Row
	skipped := 0
	for _, f := range files {
		stem := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		stem = dupSuffix.ReplaceAllString(stem, "")
		if stem == "" {
			continue
		}
		key := strings.ToLower(stem)
		if known[key] {
			skipped++
			continue
		}
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		srcKey, native := s.tokenSource(stem)
		addedRows = append(addedRows, model.Row{
			Token: stem, Status: "done", Source: srcKey, SourceID: native,
			Site: "local", Title: stem, Note: "scan:init",
			FileName: filepath.Base(f), FilePath: f,
			FileSize:  strconv.FormatInt(st.Size(), 10),
			FileDate:  st.ModTime().Format("2006-01-02 15:04:05"),
			FirstSeen: now, LastSeen: now, DownloadKind: "http",
		})
		known[key] = true
	}
	if len(addedRows) == 0 {
		return 0, skipped, nil
	}
	if err := s.st.UpsertRows(addedRows); err != nil {
		return 0, skipped, err
	}
	s.mu.Lock()
	s.rows = append(s.rows, addedRows...)
	s.rev++
	s.rowsRev++
	s.mu.Unlock()
	return len(addedRows), skipped, nil
}

func (s *Server) tokenSource(token string) (string, string) {
	if i := strings.Index(token, "@"); i > 0 {
		prefix := strings.ToLower(token[:i])
		if _, ok := s.reg.Manifest(prefix); ok {
			return prefix, token[i+1:]
		}
	}
	return "local", token
}

func walkMP4(root string, maxDepth int) []string {
	var out []string
	if root == "" {
		return out
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if d.IsDir() {
			if rel != "." && strings.Count(rel, string(os.PathSeparator)) >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".mp4") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func (s *Server) handleImportLocal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	src := strings.TrimSpace(body.Path)
	if src == "" {
		bad(w, "请填写本地视频文件夹")
		return
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		bad(w, "路径不合法: "+src)
		return
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		bad(w, "文件夹不存在: "+abs)
		return
	}
	content := filepath.Clean(s.paths.Content)
	if strings.EqualFold(abs, content) || strings.HasPrefix(strings.ToLower(abs)+string(os.PathSeparator), strings.ToLower(content)+string(os.PathSeparator)) {
		bad(w, "源文件夹在媒体目录内，不能导入自身")
		return
	}
	files := walkMP4(abs, 4)
	if len(files) == 0 {
		bad(w, "该文件夹里没有 mp4: "+abs)
		return
	}
	dest := filepath.Join(s.paths.Content, "local")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		bad(w, err.Error())
		return
	}
	moved, skipped, failed := 0, 0, 0
	for _, f := range files {
		target := filepath.Join(dest, filepath.Base(f))
		if old, err := os.Stat(target); err == nil {
			if fi, err := os.Stat(f); err == nil && fi.Size() == old.Size() {
				_ = os.Remove(f)
				moved++
			} else {
				skipped++
			}
			continue
		}
		if err := moveFile(f, target); err != nil {
			failed++
		} else {
			moved++
		}
	}
	added, _, err := s.importProjectFiles()
	if err != nil {
		bad(w, err.Error())
		return
	}
	changed, err := s.Scan(false)
	if err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("import", fmt.Sprintf("本地导入：移动 %d 个，新增 %d 条，绑定 %d 条", moved, added, changed))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "moved": moved, "skipped": skipped, "failed": failed,
		"added": added, "changed": changed, "covers": 0, "dir": dest,
	})
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// ---------- 封面 / 缩略图 ----------

func (s *Server) coverPath(token string) string {
	return filepath.Join(s.paths.Covers, token+".jpg")
}

func (s *Server) thumbPath(token string) string {
	return filepath.Join(s.paths.Thumbs, token+".jpg")
}

func (s *Server) hasCover(token string) bool {
	st, err := os.Stat(s.coverPath(token))
	return err == nil && st.Size() > 1024
}

func (s *Server) makeThumb(token, coverPath string) {
	_ = cover.MakeThumb(coverPath, s.thumbPath(token), 120)
}

func (s *Server) ffmpegPath() string {
	s.mu.RLock()
	ff := strings.TrimSpace(s.settings.FFmpegPath)
	s.mu.RUnlock()
	return proc.FindFFmpeg(ff)
}

func (s *Server) handleCoversRepair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Limit     int  `json:"limit"`
		Local     bool `json:"local"`
		LocalOnly bool `json:"localOnly"`
	}
	_ = decodeJSON(r, &body)
	limit := body.Limit
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}

	s.mu.RLock()
	rows := make([]model.Row, len(s.rows))
	copy(rows, s.rows)
	s.mu.RUnlock()

	fixed, localFixed, failed, dead, scanned := 0, 0, 0, 0, 0

	// 1) 本地有视频、没封面：抽一帧（最多 3 个，避免占满请求）
	localCap := 3
	if body.Limit > 0 && body.Limit < localCap {
		localCap = body.Limit
	}
	for i := range rows {
		if localCap <= 0 {
			break
		}
		row := &rows[i]
		if row.FileName == "" || row.Token == "" || s.hasCover(row.Token) {
			continue
		}
		if _, err := os.Stat(row.FilePath); err != nil {
			continue
		}
		localCap--
		scanned++
		if s.makeLocalCover(row.FilePath, s.coverPath(row.Token)) {
			s.makeThumb(row.Token, s.coverPath(row.Token))
			fixed++
			localFixed++
		} else {
			failed++
		}
	}

	// 2) 远程补封面
	if !body.LocalOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		for i := range rows {
			if scanned >= limit {
				break
			}
			row := &rows[i]
			if row.Token == "" || s.hasCover(row.Token) {
				continue
			}
			if row.CoverURL == "" && row.FileName == "" {
				continue
			}
			scanned++
			coverURL := row.CoverURL
			if coverURL == "" {
				if src, ok := s.reg.Get(row.Source); ok {
					if c, ok := src.(source.Coverer); ok {
						u, err := c.Cover(ctx, *row)
						if err == nil {
							coverURL = u
						}
					}
				}
			}
			if coverURL == "" {
				_ = os.WriteFile(s.coverPath(row.Token)+".missing", nil, 0o644)
				dead++
				continue
			}
			b, err := s.fetchImage(ctx, coverURL)
			if err != nil {
				if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "410") {
					_ = os.WriteFile(s.coverPath(row.Token)+".missing", nil, 0o644)
					dead++
				} else {
					failed++
				}
				continue
			}
			if len(b) <= 1024 {
				_ = os.WriteFile(s.coverPath(row.Token)+".missing", nil, 0o644)
				dead++
				continue
			}
			if err := os.MkdirAll(s.paths.Covers, 0o755); err != nil {
				failed++
				continue
			}
			tmp := s.coverPath(row.Token) + ".tmp"
			if err := os.WriteFile(tmp, b, 0o644); err != nil {
				failed++
				continue
			}
			if err := os.Rename(tmp, s.coverPath(row.Token)); err != nil {
				failed++
				continue
			}
			s.makeThumb(row.Token, s.coverPath(row.Token))
			fixed++
		}
	}
	if fixed > 0 {
		s.Bump(false)
	}
	s.logbuf.Add("cover", fmt.Sprintf("封面修复：新增 %d（本地 %d），失败 %d，标记缺失 %d", fixed, localFixed, failed, dead))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "fixed": fixed, "local": localFixed, "failed": failed,
		"missing": dead, "scanned": scanned,
	})
}

func (s *Server) makeLocalCover(video, dest string) bool {
	ff := s.ffmpegPath()
	if ff == "" {
		return false
	}
	if st, err := os.Stat(video); err != nil || st.Size() == 0 {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	args := []string{"-y", "-nostdin", "-loglevel", "error", "-ss", "1", "-i", video,
		"-frames:v", "1", "-q:v", "3", dest}
	cmd := exec.CommandContext(ctx, ff, args...)
	proc.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return false
	}
	st, err := os.Stat(dest)
	return err == nil && st.Size() > 1024
}

func (s *Server) fetchImage(ctx context.Context, rawURL string) ([]byte, error) {
	if s.imageFetch != nil {
		return s.imageFetch(rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("封面地址不合法")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36")
	s.mu.RLock()
	proxy := strings.TrimSpace(s.settings.Proxy)
	s.mu.RUnlock()
	tr := &http.Transport{
		DialContext:           imageDial,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
	if pu, perr := netproxy.ProxyURL(proxy); perr == nil && pu != nil {
		tr.Proxy = http.ProxyURL(pu)
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if netproxy.IsProxyAddr(addr, pu) {
				return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
			}
			return imageDial(ctx, network, addr)
		}
	}
	client := &http.Client{Timeout: 25 * time.Second, Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// imageDial 拒绝内网地址，并优先 IPv4（封面 CDN 常见 IPv6 不通）。
func imageDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("解析失败: %s", host)
	}
	d := &net.Dialer{Timeout: 8 * time.Second}
	var lastErr error
	for pass := 0; pass < 2; pass++ {
		for _, ip := range ips {
			if isPrivate(ip.IP) {
				continue
			}
			if pass == 0 && ip.IP.To4() == nil {
				continue
			}
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("拒绝访问内网地址: %s", host)
	}
	return nil, lastErr
}

func isPrivate(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// ---------- 时间轴快照 ----------

type snapshotJob struct {
	ID         string  `json:"id"`
	Token      string  `json:"token"`
	Time       float64 `json:"time"`
	Name       string  `json:"name"`
	Path       string  `json:"-"`
	Status     string  `json:"status"`
	Error      string  `json:"error,omitempty"`
	StartedAt  string  `json:"startedAt"`
	FinishedAt string  `json:"finishedAt,omitempty"`
}

func (s *Server) coverMapPath() string { return filepath.Join(s.paths.DataDir, "cover-map.json") }

func (s *Server) readCoverMap() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(s.coverMapPath())
	if err != nil {
		return out
	}
	var m map[string]string
	if jsonx.Unmarshal(b, &m) == nil {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func (s *Server) writeCoverMap(m map[string]string) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := s.coverMapPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.coverMapPath())
}

func (s *Server) handleSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string  `json:"token"`
		Time  float64 `json:"time"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	token := strings.TrimSpace(body.Token)
	if !validToken(token) {
		bad(w, "token 不合法")
		return
	}
	s.mu.RLock()
	rows := s.rows
	idx := s.index
	file, ok := resolveFileIn(rows, idx, s.paths.Content, token)
	s.mu.RUnlock()
	if !ok {
		bad(w, "本地没有这个文件，无法快照")
		return
	}
	ff := s.ffmpegPath()
	if ff == "" {
		bad(w, "没有找到 FFmpeg，请在设置里填写路径")
		return
	}
	if body.Time < 0 {
		body.Time = 0
	}
	if err := os.MkdirAll(s.paths.Snapshots, 0o755); err != nil {
		bad(w, err.Error())
		return
	}
	now := time.Now()
	name := fmt.Sprintf("%s_%.3f_%s%03d.jpg", token, body.Time, now.Format("20060102150405"), now.Nanosecond()/1e6)
	out := filepath.Join(s.paths.Snapshots, name)
	id := strconv.FormatInt(now.UnixNano(), 36)
	if len(id) > 10 {
		id = id[:10]
	}

	s.snapMu.Lock()
	s.cleanupSnapJobsLocked()
	running := 0
	for _, j := range s.snapJobs {
		if j.Status == "running" {
			running++
		}
	}
	if running >= 2 {
		s.snapMu.Unlock()
		bad(w, "已有 2 个快照任务在生成，请稍后再试")
		return
	}
	job := &snapshotJob{
		ID: id, Token: token, Time: body.Time, Name: name, Path: out,
		Status: "running", StartedAt: now.Format("2006-01-02 15:04:05"),
	}
	s.snapJobs[id] = job
	s.snapMu.Unlock()

	go s.runSnapshot(ff, job, file, out)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "pending": true, "id": id, "name": name, "time": body.Time,
	})
}

func (s *Server) runSnapshot(ff string, job *snapshotJob, input, out string) {
	timeText := strconv.FormatFloat(job.Time, 'f', 3, 64)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-ss", timeText, "-i", input,
		"-frames:v", "1", "-q:v", "3", "-y", out}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ff, args...)
	proc.HideWindow(cmd)
	cmd.Stderr = &stderr
	err := cmd.Run()
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	j := s.snapJobs[job.ID]
	if j == nil {
		return
	}
	j.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	if st, serr := os.Stat(out); err == nil && serr == nil && st.Size() > 0 {
		j.Status = "done"
		return
	}
	j.Status = "failed"
	tail := strings.TrimSpace(stderr.String())
	if len(tail) > 300 {
		tail = tail[len(tail)-300:]
	}
	if tail == "" {
		tail = "FFmpeg 没有产出文件"
	}
	j.Error = tail
}

func (s *Server) cleanupSnapJobsLocked() {
	for id, j := range s.snapJobs {
		if j.Status == "running" || j.FinishedAt == "" {
			continue
		}
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", j.FinishedAt, time.Local); err == nil {
			if time.Since(t) > 10*time.Minute {
				delete(s.snapJobs, id)
			}
		}
	}
}

func (s *Server) snapshotJobs(token string) []*snapshotJob {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	s.cleanupSnapJobsLocked()
	out := make([]*snapshotJob, 0, len(s.snapJobs))
	for _, j := range s.snapJobs {
		if token == "" || j.Token == token {
			out = append(out, j)
		}
	}
	return out
}

func (s *Server) handleSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if !validToken(body.Token) || !validSnapshotName(body.Name) || !strings.HasPrefix(body.Name, body.Token+"_") {
		bad(w, "快照名不合法")
		return
	}
	p := filepath.Join(s.paths.Snapshots, body.Name)
	if !inside(s.paths.Snapshots, p) {
		bad(w, "快照名不合法")
		return
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		bad(w, err.Error())
		return
	}
	cm := s.readCoverMap()
	if cm[body.Token] == body.Name {
		delete(cm, body.Token)
		_ = s.writeCoverMap(cm)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSnapshotCover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if !validToken(body.Token) || !validSnapshotName(body.Name) || !strings.HasPrefix(body.Name, body.Token+"_") {
		bad(w, "快照名不合法")
		return
	}
	if _, err := os.Stat(filepath.Join(s.paths.Snapshots, body.Name)); err != nil {
		bad(w, "快照不存在")
		return
	}
	cm := s.readCoverMap()
	cm[body.Token] = body.Name
	if err := s.writeCoverMap(cm); err != nil {
		bad(w, err.Error())
		return
	}
	s.Bump(false)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 播放 / 定位 ----------

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token     string `json:"token"`
		Reveal    bool   `json:"reveal"`
		CountOnly bool   `json:"countOnly"`
		NoRecord  bool   `json:"noRecord"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	s.mu.RLock()
	file, ok := resolveFileIn(s.rows, s.index, s.paths.Content, body.Token)
	s.mu.RUnlock()
	if !ok {
		bad(w, "本地还没有这个文件（先下载）")
		return
	}
	name := filepath.Base(file)
	if body.Reveal {
		if err := revealFile(file); err != nil {
			bad(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "file": file, "name": name})
		return
	}
	if !body.CountOnly {
		if err := openFile(file); err != nil {
			bad(w, err.Error())
			return
		}
	}
	var plays, lastPlay string
	if !body.NoRecord {
		plays, lastPlay = s.recordPlay(body.Token)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "file": file, "name": name, "plays": plays, "last_play": lastPlay,
	})
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	s.mu.RLock()
	file, ok := resolveFileIn(s.rows, s.index, s.paths.Content, body.Token)
	s.mu.RUnlock()
	if !ok {
		// 文件已经丢失（无效条目）：退而打开它所在的目录，方便用户排查。
		if row, found := s.rowByToken(body.Token); found {
			p := strings.TrimSpace(row.FilePath)
			if p == "" && strings.TrimSpace(row.FileName) != "" {
				p = filepath.Join(s.paths.Content, row.FileName)
			}
			if p != "" {
				dir := filepath.Dir(p)
				if inside(s.paths.Content, dir) {
					if err := openPath(dir); err == nil {
						writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": dir})
						return
					}
				}
			}
		}
		bad(w, "本地还没有这个文件（先下载）")
		return
	}
	if err := revealFile(file); err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": file})
}

func (s *Server) recordPlay(token string) (string, string) {
	now := time.Now().Format("2006-01-02 15:04:05")
	n, err := s.st.BumpPlayCount(token, now)
	if err != nil {
		return "", ""
	}
	s.ReloadRow(token)
	return n, now
}

func openFile(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func revealFile(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,"+path).Start()
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}
