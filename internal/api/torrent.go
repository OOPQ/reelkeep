package api

import "net/http"

// 磁力入口暂时关闭。已有 torrent_jobs 仍可查看，但不再接受新的磁力任务。
func (s *Server) handleTorrentAdd(w http.ResponseWriter, r *http.Request) {
	bad(w, "暂不支持磁力链接")
}

func (s *Server) handleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	bad(w, "暂不支持磁力链接")
}

// handleTorrentJobs 返回已有磁力任务，供旧任务查看/清理。
func (s *Server) handleTorrentJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.st.ListJobs()
	if err != nil {
		bad(w, err.Error())
		return
	}
	if s.dl != nil {
		for i := range jobs {
			jobs[i].Peers, jobs[i].Seeders = s.dl.TorrentSessionStats(jobs[i].ID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "jobs": jobs})
}
