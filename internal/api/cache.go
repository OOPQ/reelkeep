package api

import (
	"net/http"
	"os"
	"path/filepath"
)

// handleCacheClear 清理封面/缩略图缓存，文件丢失后会重新生成或回源。
func (s *Server) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	removed := 0
	for _, dir := range []string{s.paths.Covers, s.paths.Thumbs} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if os.Remove(filepath.Join(dir, e.Name())) == nil {
				removed++
			}
		}
	}
	s.mu.Lock()
	s.rev++
	s.rowsRev++
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed})
}
