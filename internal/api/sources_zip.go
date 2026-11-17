package api

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"strings"
)

// handleSourceZipImport imports every JSON source package inside a zip.
func (s *Server) handleSourceZipImport(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	overwrite := r.URL.Query().Get("overwrite") == "1"
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 20<<20))
	if err != nil {
		bad(w, "读取 ZIP 失败")
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		bad(w, "不是有效的 ZIP 文件")
		return
	}
	type imported struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	var okList []imported
	var errs []string
	for _, f := range zr.File {
		name := strings.ToLower(strings.TrimSpace(f.Name))
		if f.FileInfo().IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.HasSuffix(name, "sources.index.json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			errs = append(errs, f.Name+": "+err.Error())
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, 4<<20))
		_ = rc.Close()
		if err != nil {
			errs = append(errs, f.Name+": "+err.Error())
			continue
		}
		p, err := s.srcMgr.Import(data, overwrite)
		if err != nil {
			errs = append(errs, f.Name+": "+err.Error())
			continue
		}
		okList = append(okList, imported{ID: p.ID, Name: p.Name, Version: p.Version})
	}
	if len(okList) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "ZIP 里没有可导入的源包", "errors": errs})
		return
	}
	s.Bump(false)
	for _, p := range okList {
		s.logbuf.Add("source", "从 ZIP 导入源 "+p.ID+" v"+p.Version)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": okList, "errors": errs})
}
