package api

import "net/http"

func (s *Server) handleHistoryClear(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ClearHistory(); err != nil {
		bad(w, err.Error())
		return
	}
	rows, err := s.st.LoadRows()
	if err != nil {
		bad(w, err.Error())
		return
	}
	s.mu.Lock()
	s.rows = rows
	s.rev++
	s.rowsRev++
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
