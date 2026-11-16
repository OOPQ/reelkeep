package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/proc"
	"github.com/OOPQ/reelkeep/internal/source"
)

// downloadInfo 返回下载队列快照；下载器未启用时给一个空壳。
func (s *Server) downloadInfo() download.Info {
	if s.dl == nil {
		return download.Info{
			Phase: "idle", Active: []string{}, Errors: []map[string]string{},
			Items: []download.Item{},
		}
	}
	return s.dl.Info()
}

// ReloadRow 在下载完成后把台账行从数据库刷回内存。
func (s *Server) ReloadRow(token string) {
	row, ok, err := s.st.GetRow(token)
	if err != nil || !ok {
		return
	}
	s.mu.Lock()
	for i := range s.rows {
		if s.rows[i].Token == token {
			s.rows[i] = row
			break
		}
	}
	s.rev++
	s.rowsRev++
	s.mu.Unlock()
}

// handleDownloadAdd 把选中条目加入下载队列。
func (s *Server) handleDownloadAdd(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	var body struct {
		Tokens []string `json:"tokens"`
		Status string   `json:"status"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Tokens) == 0 {
		bad(w, "tokens 为空")
		return
	}
	if len(body.Tokens) > 200 {
		bad(w, "一次最多加入 200 条")
		return
	}

	s.mu.RLock()
	rows := make([]model.Row, len(s.rows))
	copy(rows, s.rows)
	idx := s.index
	s.mu.RUnlock()

	started, skipped := 0, 0
	var errs []string
	for _, tk := range body.Tokens {
		row, ok := findRow(rows, tk)
		if !ok {
			skipped++
			continue
		}
		if row.Status == "recycled" {
			skipped++
			continue
		}
		if row.URL == "" {
			errs = append(errs, tk+": 没有播放地址")
			continue
		}
		if file, ok := resolveFileIn(rows, idx, s.paths.Content, tk); ok && file != "" {
			skipped++ // 已经有文件
			continue
		}
		mode := row.DownloadKind
		if mode == "" {
			mode = "http"
		}
		headers := row.DownloadHeaders
		useAuth, refresh := false, false
		if src, ok := s.reg.Get(row.Source); ok {
			if dr, ok := src.(source.DownloadResolver); ok {
				spec, err := dr.Resolve(context.Background(), row)
				if err != nil {
					errs = append(errs, tk+": "+err.Error())
					continue
				}
				row.URL = spec.URL
				mode = spec.Mode
				useAuth = spec.UseAuth
				refresh = spec.RefreshOn410
				if b, err := json.Marshal(spec.Headers); err == nil {
					headers = string(b)
				}
			}
		}
		if m, ok := s.reg.Manifest(row.Source); ok && m.AuthType != "" {
			useAuth = true
		}
		task := model.DownloadTask{
			Token: row.Token, Source: row.Source, SourceID: row.SourceID,
			URL: row.URL, Mode: mode, Headers: headers,
			UseAuth: useAuth, RefreshOn410: refresh,
			State: "queued", AddedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
		added, err := s.dl.Enqueue(task)
		if err != nil {
			errs = append(errs, tk+": "+err.Error())
			continue
		}
		if added {
			started++
		} else {
			skipped++
		}
	}
	s.logbuf.Add("download", "加入下载队列 "+strconv.Itoa(started)+" 条")
	s.Bump(false)
	if started == 0 {
		msg := "没有可加入队列的条目（可能已有文件或不在台账里）"
		if len(errs) > 0 {
			msg = strings.Join(errs, "; ")
		}
		bad(w, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "started": started, "skipped": skipped, "errors": errs,
	})
}

func (s *Server) handleDownloadPause(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	var body struct {
		On     bool     `json:"on"`
		Tokens []string `json:"tokens"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	n := s.dl.Pause(body.On, body.Tokens)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": n})
}

func (s *Server) handleDownloadCancel(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	var body struct {
		Tokens []string `json:"tokens"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	n := s.dl.Cancel(body.Tokens)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": n})
}

func (s *Server) handleDownloadClearDone(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	n := s.dl.ClearDone()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": n})
}

func (s *Server) handleDownloadReset(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	n := s.dl.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "killed": n, "reverted": 0})
}

func (s *Server) handleDownloadRepair(w http.ResponseWriter, r *http.Request) {
	renamed, deleted, skipped := 0, 0, 0
	probe := s.ffprobePath()
	ffmpeg := s.ffmpegPath()
	validator := ""
	switch {
	case probe != "":
		validator = "ffprobe"
	case ffmpeg != "":
		validator = "ffmpeg"
	}

	_ = filepath.WalkDir(s.paths.Content, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".mp4.part") {
			return nil
		}
		final := strings.TrimSuffix(path, ".part")
		if st, err := os.Stat(final); err == nil {
			// 已有正片：.part 比正片小就删掉，避免重复
			if pst, err := os.Stat(path); err == nil && st.Size() >= pst.Size() {
				if os.Remove(path) == nil {
					deleted++
				}
			} else {
				skipped++
			}
			return nil
		}
		if validator == "" {
			skipped++
			return nil
		}
		ok := false
		if probe != "" {
			ok = validPartProbe(probe, path)
		} else {
			ok = validPartFrame(ffmpeg, path)
		}
		if !ok {
			skipped++
			return nil
		}
		if err := os.Rename(path, final); err != nil {
			skipped++
		} else {
			renamed++
		}
		return nil
	})

	note := ""
	if validator == "" {
		note = "未找到 ffprobe/ffmpeg，未校验 .part"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "renamed": renamed, "deleted": deleted, "skipped": skipped,
		"validator": validator, "note": note,
	})
}

// ffprobePath 优先找 ffmpeg 同目录的 ffprobe，其次 PATH。
func (s *Server) ffprobePath() string {
	s.mu.RLock()
	ff := strings.TrimSpace(s.settings.FFmpegPath)
	s.mu.RUnlock()
	names := []string{"ffprobe", "ffprobe.exe"}
	if ff != "" {
		dir := filepath.Dir(ff)
		for _, n := range names {
			p := filepath.Join(dir, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// validPartProbe 用 ffprobe 读时长，能读出正数才认为文件完整。
func validPartProbe(probe, path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, probe,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", path)
	proc.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return err == nil && d > 0
}

// validPartFrame 没有 ffprobe 时的兜底：让 ffmpeg 解一帧视频，成功即说明容器可读。
func validPartFrame(ffmpeg, path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-v", "error", "-i", path, "-map", "0:v:0", "-frames:v", "1", "-f", "null", "-")
	proc.HideWindow(cmd)
	return cmd.Run() == nil
}
