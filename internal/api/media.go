package api

import (
	"crypto/subtle"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/scan"
)

// handleMedia 提供浏览器内播放：支持 Range / HEAD，只监听回环地址，
// 并且必须带本次进程生成的 key（防止别的网页通过 localhost 探测）。
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	key := s.mediaKey
	s.mu.RUnlock()
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("k")), []byte(key)) != 1 {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/media/")
	if !strings.HasSuffix(strings.ToLower(name), ".mp4") {
		http.NotFound(w, r)
		return
	}
	token := name[:len(name)-len(".mp4")]
	if !validToken(token) {
		http.NotFound(w, r)
		return
	}
	s.mu.RLock()
	file, ok := resolveFileIn(s.rows, s.index, s.paths.Content, token)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

// resolveFileIn 在给定快照里按 token 找本地文件。
// 顺序与旧版一致：台账里的 file_path → 台账里的 file_name → 扫盘索引。
func resolveFileIn(rows []model.Row, idx scan.Index, contentDir, token string) (string, bool) {
	if row, ok := findRow(rows, token); ok {
		if row.FilePath != "" && inside(contentDir, row.FilePath) {
			if st, err := os.Stat(row.FilePath); err == nil && !st.IsDir() {
				return row.FilePath, true
			}
		}
		if row.FileName != "" {
			p := filepath.Join(contentDir, row.FileName)
			if inside(contentDir, p) {
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p, true
				}
			}
		}
	}
	if e, ok := idx.Files[strings.ToLower(token)]; ok {
		return e.File, true
	}
	return "", false
}
