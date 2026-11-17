package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source/web"
)

// torrentPick 是前端勾选的一个磁力文件。
type torrentPick struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	Length int64  `json:"length"`
}

// handleTorrentAdd 把每个勾选文件拆成独立台账条目和独立下载任务。
func (s *Server) handleTorrentAdd(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	var body struct {
		URL   string        `json:"url"`
		Files []torrentPick `json:"files"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Files) == 0 {
		bad(w, "请至少勾选一个文件")
		return
	}
	base, err := web.MagnetRow(body.URL)
	if err != nil {
		bad(w, err.Error())
		return
	}
	hash := strings.ToLower(base.SourceID)
	now := time.Now().Format("2006-01-02 15:04:05")
	rows := make([]model.Row, 0, len(body.Files))
	tasks := make([]model.DownloadTask, 0, len(body.Files))
	seen := map[int]bool{}
	for _, f := range body.Files {
		if f.Index < 0 || seen[f.Index] || strings.TrimSpace(f.Path) == "" {
			continue
		}
		seen[f.Index] = true
		name := filepath.Base(f.Path)
		token := fmt.Sprintf("magnet@%s_%d", hash, f.Index)
		row := base
		row.Token = token
		row.Title = strings.TrimSuffix(name, filepath.Ext(name))
		rows = append(rows, row)
		tasks = append(tasks, model.DownloadTask{
			Token: token, Source: row.Source, SourceID: row.SourceID,
			URL: row.URL, Mode: "torrent", Files: []int{f.Index}, Name: name,
			State: "queued", AddedAt: now,
		})
	}
	if len(rows) == 0 {
		bad(w, "没有有效的勾选文件")
		return
	}
	if _, _, _, _, err := s.applyScrape(rows, false); err != nil {
		bad(w, "入库失败: "+err.Error())
		return
	}
	tokens := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if _, err := s.dl.Enqueue(task); err != nil {
			bad(w, "加入下载队列失败: "+err.Error())
			return
		}
		tokens = append(tokens, task.Token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tokens": tokens})
}

// handleTorrentFiles 返回 magnet 的文件列表，供下载前勾选。
func (s *Server) handleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	raw := strings.TrimSpace(body.URL)
	if !strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		bad(w, "只支持 magnet 链接")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Second)
	defer cancel()
	files, err := download.ListTorrentFiles(ctx, raw, filepath.Join(s.paths.DataDir, "torrents", download.TorrentHash(raw), "info.torrent"))
	if err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": files})
}
