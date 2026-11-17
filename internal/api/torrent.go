package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source/web"
)

// torrentPick 是前端勾选的一个磁力文件。
type torrentPick struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	Length int64  `json:"length"`
}

func safeDirName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 32 {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "torrent"
	}
	return name
}

func jobTitleFrom(base model.Row, picks []torrentPick) string {
	if first := picks[0].Path; first != "" {
		dir := filepath.Base(filepath.ToSlash(filepath.Dir(first)))
		if dir != "." && dir != "/" && dir != "" {
			return dir
		}
	}
	title := strings.TrimSpace(base.Title)
	if title != "" && !strings.HasPrefix(title, "磁力任务") {
		return title
	}
	return "磁力任务 " + strings.ToUpper(base.SourceID[:min(8, len(base.SourceID))])
}

// handleTorrentAdd 创建一个磁力 Job（一个 infohash 一个 Job），不预创建媒体条目。
func (s *Server) handleTorrentAdd(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		bad(w, "下载器未启用")
		return
	}
	var body struct {
		URL   string        `json:"url"`
		Files []torrentPick `json:"files"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Files) == 0 {
		bad(w, "请至少勾选一个文件")
		return
	}
	base, err := web.MagnetRow(body.URL)
	if err != nil {
		bad(w, err.Error())
		return
	}
	hash := strings.ToLower(base.SourceID)
	title := jobTitleFrom(base, body.Files)
	saveDir := filepath.Join(s.paths.Content, "local")
	now := time.Now().Format("2006-01-02 15:04:05")
	job := model.TorrentJob{
		ID: hash, Infohash: hash, Magnet: body.URL, Title: title,
		SaveDir: saveDir, State: "queued", CreatedAt: now, UpdatedAt: now,
	}
	var indexes []int
	seen := map[int]bool{}
	for _, f := range body.Files {
		if f.Index < 0 || seen[f.Index] || strings.TrimSpace(f.Path) == "" {
			continue
		}
		seen[f.Index] = true
		indexes = append(indexes, f.Index)
		job.Files = append(job.Files, model.TorrentJobFile{
			JobID: hash, Index: f.Index, Path: f.Path, Length: f.Length,
			Selected: true, State: "queued",
		})
	}
	if len(indexes) == 0 {
		bad(w, "没有有效的勾选文件")
		return
	}
	if err := s.st.SaveJob(job); err != nil {
		bad(w, "保存磁力任务失败: "+err.Error())
		return
	}
	if saved, ok, err := s.st.GetJob(hash); err == nil && ok {
		job = saved
	}
	task := model.DownloadTask{
		Token: "torrent@" + hash, Source: "local", SourceID: hash,
		URL: body.URL, Mode: "torrent", Files: indexes, Name: title,
		State: "queued", AddedAt: now,
	}
	if _, err := s.dl.Enqueue(task); err != nil {
		bad(w, "加入下载队列失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
}

// handleTorrentJobs 返回磁力任务列表及文件状态。
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

// handleTorrentFiles 返回 magnet 的文件列表，供下载前勾选。
func (s *Server) handleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	raw := strings.TrimSpace(body.URL)
	if !strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		bad(w, "只支持 magnet 链接")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Second)
	defer cancel()
	files, err := download.ListTorrentFiles(ctx, raw, filepath.Join(s.paths.DataDir, "torrents", download.TorrentHash(raw), "info.torrent"))
	if err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": files})
}
