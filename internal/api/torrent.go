package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/download"
)

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
