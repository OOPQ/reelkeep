package api

import (
	"net/http"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/proc"
)

func (s *Server) handleFFmpegInstall(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	proxy := s.settings.Proxy
	s.mu.RUnlock()

	path, err := proc.InstallFFmpeg(r.Context(), proxy)
	if err != nil {
		bad(w, err.Error())
		return
	}

	s.mu.Lock()
	cfg := s.settings
	cfg.FFmpegPath = path
	s.settings = cfg
	s.rev++
	s.mu.Unlock()
	if err := config.SaveSettings(s.paths, cfg); err != nil {
		bad(w, err.Error())
		return
	}
	if s.dl != nil {
		s.dl.SetSettings(download.Settings{
			Concurrent:      cfg.Concurrent,
			FFmpeg:          cfg.FFmpegPath,
			Proxy:           cfg.Proxy,
			RetryCount:      cfg.RetryCount,
			RetryDelay:      time.Duration(cfg.RetryDelay) * time.Second,
			TorrentLimitKB:  cfg.TorrentLimitKB,
			HTTPLimitKB:     cfg.HTTPLimitKB,
			HTTPConnections: cfg.HTTPConnections,
			TorrentBackend:  cfg.TorrentBackend,
			QbtURL:          cfg.QbtURL, QbtUser: cfg.QbtUser, QbtPass: cfg.QbtPass,
		})
	}
	s.logbuf.Add("config", "已安装 FFmpeg: "+path)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}
