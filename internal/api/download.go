package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/model"
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "renamed": 0, "deleted": 0})
}
