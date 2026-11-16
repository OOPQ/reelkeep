package api

import (
	"net/http"

	"github.com/OOPQ/reelkeep/internal/update"
)

// handleUpdate 检查 GitHub Releases；只提示，不下载/安装。
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	current := s.version
	s.mu.RUnlock()
	if current == "" {
		current = "0.0.0-dev"
	}
	rel, err := update.Check(r.Context(), current)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "update": rel})
}
