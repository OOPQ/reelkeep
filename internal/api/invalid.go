package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
)

// InvalidFile 是一条“数据库里有、磁盘上找不到/不可用”的本地视频。
type InvalidFile struct {
	Token  string `json:"token"`
	Title  string `json:"title"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// handleInvalidFiles 返回无效视频文件列表（文件不存在、空文件、路径是目录）。
func (s *Server) handleInvalidFiles(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	rows := make([]model.Row, len(s.rows))
	copy(rows, s.rows)
	s.mu.RUnlock()

	items := make([]InvalidFile, 0, 16)
	for _, row := range rows {
		name := strings.TrimSpace(row.FileName)
		if name == "" {
			continue
		}
		p := strings.TrimSpace(row.FilePath)
		if p == "" {
			p = filepath.Join(s.paths.Content, name)
		}
		reason := ""
		st, err := os.Stat(p)
		switch {
		case err != nil:
			reason = "文件不存在"
		case st.IsDir():
			reason = "路径是目录"
		case st.Size() == 0:
			reason = "空文件（0 字节）"
		}
		if reason == "" {
			continue
		}
		items = append(items, InvalidFile{Token: row.Token, Title: row.Title, Path: p, Reason: reason})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}
