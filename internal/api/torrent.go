package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source/web"
)

// handleTorrentAdd 把 magnet 入库并立即用勾选的文件入队，保证“确认文件后才产生任务”。
func (s *Server) handleTorrentAdd(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	var body struct {
		URL   string `json:"url"`
		Files []int  `json:"files"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Files) == 0 {
		bad(w, "请至少勾选一个文件")
		return
	}
	row, err := web.MagnetRow(body.URL)
	if err != nil {
		bad(w, err.Error())
		return
	}
	added, updated, skipped, _, err := s.applyScrape([]model.Row{row}, false)
	if err != nil {
		bad(w, "入库失败: "+err.Error())
		return
	}
	if added+updated == 0 && skipped > 0 {
		bad(w, "磁力任务未入库")
		return
	}
	task := model.DownloadTask{
		Token: row.Token, Source: row.Source, SourceID: row.SourceID,
		URL: row.URL, Mode: "torrent", Files: body.Files, State: "queued",
		AddedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if _, err := s.dl.Enqueue(task); err != nil {
		bad(w, "加入下载队列失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": row.Token})
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
	files, err := download.ListTorrentFiles(ctx, raw)
	if err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": files})
}
