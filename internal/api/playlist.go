package api

import (
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
)

// attachPlaylistCollection adds playlist rows to a collection named after the playlist.
func (s *Server) attachPlaylistCollection(name string, rows []model.Row) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "播放清单"
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	var id int64
	cols, err := s.st.Collections()
	if err == nil {
		for _, c := range cols {
			if strings.EqualFold(strings.TrimSpace(c.Name), name) {
				id = c.ID
				break
			}
		}
	}
	if id == 0 {
		id, err = s.st.CreateCollection(name, palette[len(cols)%len(palette)])
		if err != nil {
			return
		}
	}
	tokens := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Token != "" {
			tokens = append(tokens, r.Token)
		}
	}
	if len(tokens) == 0 {
		return
	}
	if err := s.st.AddToCollection(id, tokens); err == nil {
		s.Bump(true)
		s.logbuf.Add("scrape", "已加入合集：「"+name+"」")
	}
}
