package api

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
)

// migrateMediaFiles restores file links after legacy tokens were renamed.
// The media files still use the old native-id name; Scan cannot match them to
// source@id rows, so rename them first and restore the file metadata.
func (s *Server) migrateMediaFiles() int {
	s.mu.RLock()
	rows := append([]model.Row(nil), s.rows...)
	s.mu.RUnlock()
	var dirty []model.Row
	for i := range rows {
		r := &rows[i]
		uid := model.MediaUID(r.Source, r.SourceID, r.Token)
		if uid == "" {
			continue
		}
		dir := ""
		if r.FilePath != "" && inside(s.paths.Content, r.FilePath) {
			dir = filepath.Dir(r.FilePath)
		} else if r.FileName != "" {
			p := filepath.Join(s.paths.Content, r.FileName)
			if inside(s.paths.Content, p) {
				dir = filepath.Dir(p)
			}
		}
		if dir == "" {
			dir = filepath.Join(s.paths.Content, strings.ToLower(r.Source))
		}
		if !inside(s.paths.Content, dir) {
			continue
		}
		target := filepath.Join(dir, uid+".mp4")
		old := ""
		if r.FilePath != "" && fileExists(r.FilePath) && inside(s.paths.Content, r.FilePath) {
			old = r.FilePath
		} else if p := filepath.Join(dir, r.Token+".mp4"); fileExists(p) {
			old = p
		}
		if !fileExists(target) && old != "" {
			_ = os.Rename(old, target)
		}
		if !fileExists(target) {
			continue
		}
		st, err := os.Stat(target)
		if err != nil {
			continue
		}
		if r.FileName == uid+".mp4" && r.FilePath == target {
			continue
		}
		r.FileName = uid + ".mp4"
		r.FilePath = target
		r.FileSize = strconv.FormatInt(st.Size(), 10)
		r.FileDate = st.ModTime().Format("2006-01-02 15:04:05")
		if r.Status == "new" || r.Status == "" {
			r.Status = "done"
		}
		dirty = append(dirty, *r)
	}
	if len(dirty) == 0 {
		return 0
	}
	if err := s.st.UpsertRows(dirty); err != nil {
		s.logbuf.Add("repair", "media migration failed: "+err.Error())
		return 0
	}
	for i := range dirty {
		s.ReloadRow(dirty[i].Token)
	}
	return len(dirty)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
