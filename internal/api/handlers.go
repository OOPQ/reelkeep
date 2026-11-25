package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/jsonx"
	"github.com/OOPQ/reelkeep/internal/model"
	websource "github.com/OOPQ/reelkeep/internal/source/web"
	"github.com/OOPQ/reelkeep/internal/store"
	"github.com/OOPQ/reelkeep/web"
)

// ---------- 只读接口 ----------

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	known := int64(-1)
	if q := r.URL.Query().Get("rowsRev"); q != "" {
		if v, err := strconv.ParseInt(q, 10, 64); err == nil {
			known = v
		}
	}

	s.mu.RLock()
	includeRows := known < 0 || known != s.rowsRev
	idx := s.index
	st := statsFrom(s.rows, idx, s.coverMissing, s.paths.LedgerCSV, s.paths.Content)
	rev, rowsRev := s.rev, s.rowsRev
	cfg := s.settings
	key := s.mediaKey
	proj := Project{
		Root:       s.paths.Root,
		Data:       s.paths.DataDir,
		Media:      s.paths.Content,
		Covers:     s.paths.Covers,
		Exists:     idx.Exists,
		VideoCount: len(idx.Files),
		Ledger:     s.paths.LedgerCSV,
		Database:   s.paths.Database,
	}
	var gb float64
	for _, f := range idx.Files {
		gb += float64(f.Size)
	}
	proj.DiskGB = round2(gb / (1 << 30))
	var rows []model.Row
	if includeRows {
		rows = make([]model.Row, len(s.rows))
		copy(rows, s.rows)
	}
	s.mu.RUnlock()
	rows = s.attachTags(rows)

	payload := map[string]any{
		"ok":       true,
		"api":      APIVersion,
		"rev":      rev,
		"rowsRev":  rowsRev,
		"stats":    st,
		"log":      s.logbuf.Last(40),
		"job":      s.jobInfo(),
		"download": s.downloadInfo(),
		"project":  proj,
		"settings": map[string]any{
			"proxy":               cfg.Proxy,
			"concurrent":          cfg.Concurrent,
			"ffmpegPath":          cfg.FFmpegPath,
			"retryCount":          cfg.RetryCount,
			"retryDelay":          cfg.RetryDelay,
			"httpLimitKB":         cfg.HTTPLimitKB,
			"httpConnections":     cfg.HTTPConnections,
			"perSourceConcurrent": cfg.PerSourceConcurrent,
			"playerVolume":        cfg.PlayerVolume,
			"playerMuted":         cfg.PlayerMuted,
			"playerPlayMode":      cfg.PlayerPlayMode,
			"sourceIndexUrl":      cfg.SourceIndexURL,
		},
		"sources": s.reg.Enabled(),
		"media": map[string]any{
			"base": fmt.Sprintf("http://127.0.0.1:%d/media", s.port),
			"key":  key,
		},
		"resume":  nil,
		"metrics": s.metrics.snapshot(time.Since(s.started).Minutes()),
	}
	if includeRows {
		payload["rows"] = rows
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "project": s.project()})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	rows := make([]model.Row, len(s.rows))
	copy(rows, s.rows)
	s.mu.RUnlock()

	var b strings.Builder
	b.WriteString("\ufeff") // UTF-8 BOM：Excel 打开中文不乱码，与旧版导出一致
	writeCSVLine(&b, model.Header)
	for i := range rows {
		writeCSVLine(&b, rowStrings(&rows[i]))
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ledger.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, b.String())
}

func writeCSVLine(b *strings.Builder, vals []string) {
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(v, `"`, `""`))
		b.WriteByte('"')
	}
	b.WriteString("\r\n")
}

func rowStrings(r *model.Row) []string {
	vals := r.Values()
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = fmt.Sprint(v)
	}
	return out
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	tk := r.URL.Query().Get("token")
	full := r.URL.Query().Get("full") == "1"
	if !validToken(tk) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	var candidates []string
	if !full {
		candidates = append(candidates, filepath.Join(s.paths.Thumbs, tk+".jpg"))
	}
	candidates = append(candidates, filepath.Join(s.paths.Covers, tk+".jpg"))
	// 用户指定了「用时间轴快照当封面」时优先
	if name := s.readCoverMap()[tk]; name != "" {
		p := filepath.Join(s.paths.Snapshots, name)
		if strings.HasPrefix(name, tk+"_") && inside(s.paths.Snapshots, p) {
			if b, err := os.ReadFile(p); err == nil {
				w.Header().Set("Content-Type", "image/jpeg")
				// 封面地址带 ?c=版本号，可放心让浏览器缓存，减少上一项/下一项的重复加载。
				w.Header().Set("Cache-Control", "private, max-age=600")
				_, _ = w.Write(b)
				return
			}
		}
	}
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "private, max-age=600")
			_, _ = w.Write(b)
			return
		}
	}
	// 抓取只存了 cover_url；第一次打开时按需下载并缓存，默认走系统代理。
	row, ok := s.rowByToken(tk)
	if !ok || row.CoverURL == "" {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	u, err := url.Parse(row.CoverURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	if _, err := os.Stat(s.coverPath(tk) + ".missing"); err == nil {
		http.Redirect(w, r, row.CoverURL, http.StatusFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	b, err := s.fetchImage(ctx, row.CoverURL)
	if err != nil || len(b) <= 1024 {
		http.Redirect(w, r, row.CoverURL, http.StatusFound)
		return
	}
	if err := os.MkdirAll(s.paths.Covers, 0o755); err == nil {
		if tmp, terr := os.CreateTemp(s.paths.Covers, "cover-*.tmp"); terr == nil {
			_, werr := tmp.Write(b)
			_ = tmp.Close()
			if werr == nil {
				_ = os.Rename(tmp.Name(), s.coverPath(tk))
			} else {
				_ = os.Remove(tmp.Name())
			}
		}
	}
	s.makeThumb(tk, s.coverPath(tk))
	if !full {
		if tb, terr := os.ReadFile(s.thumbPath(tk)); terr == nil {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "private, max-age=600")
			_, _ = w.Write(tb)
			return
		}
	}
	w.Header().Set("Content-Type", http.DetectContentType(b))
	w.Header().Set("Cache-Control", "private, max-age=600")
	_, _ = w.Write(b)
}

func (s *Server) rowByToken(tk string) (model.Row, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.rows {
		if s.rows[i].Token == tk {
			return s.rows[i], true
		}
	}
	return model.Row{}, false
}

func (s *Server) handleLogo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(web.Logo())
}

func (s *Server) handleDeleted(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	items := make([]model.DeletedToken, 0, len(s.deleted))
	for _, d := range s.deleted {
		items = append(items, d)
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt > items[j].DeletedAt })
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items), "items": items})
}

// CollectionView 是合集列表项。
type CollectionView struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	CoverToken string `json:"coverToken,omitempty"`
	Count      int    `json:"count,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
	Has        bool   `json:"has,omitempty"`
}

func (s *Server) handleCollections(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.Collections()
	if err != nil {
		bad(w, err.Error())
		return
	}
	tk := r.URL.Query().Get("token")
	if tk != "" {
		has, err := s.st.CollectionsForToken(tk)
		if err != nil {
			bad(w, err.Error())
			return
		}
		items := make([]CollectionView, 0, len(list))
		for _, c := range list {
			items = append(items, CollectionView{ID: c.ID, Name: c.Name, Color: c.Color, CoverToken: c.CoverToken, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Has: has[c.ID]})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	items := make([]CollectionView, 0, len(list))
	for _, c := range list {
		items = append(items, CollectionView{ID: c.ID, Name: c.Name, Color: c.Color, CoverToken: c.CoverToken, Count: c.Count, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCollectionItems(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		bad(w, "合集 id 不合法")
		return
	}
	tokens, err := s.st.CollectionTokens(id)
	if err != nil {
		bad(w, err.Error())
		return
	}
	if tokens == nil {
		tokens = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "tokens": tokens})
}

// Snapshot 是时间轴快照。
type Snapshot struct {
	Name      string  `json:"name"`
	Time      float64 `json:"time"`
	CreatedAt string  `json:"createdAt"`
	URL       string  `json:"url"`
	IsCover   bool    `json:"isCover,omitempty"`
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	tk := r.URL.Query().Get("token")
	cm := s.readCoverMap()
	entries, err := os.ReadDir(s.paths.Snapshots)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []Snapshot{}})
		return
	}
	items := make([]Snapshot, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jpg") {
			continue
		}
		name := e.Name()
		base := strings.TrimSuffix(name, filepath.Ext(name))
		i := strings.LastIndex(base, "_")
		if i <= 0 {
			continue
		}
		rest := base[:i]
		j := strings.LastIndex(rest, "_")
		if j <= 0 {
			continue
		}
		rowToken, timeStr := rest[:j], rest[j+1:]
		if tk != "" && rowToken != tk {
			continue
		}
		t, err := strconv.ParseFloat(timeStr, 64)
		if err != nil {
			continue
		}
		created := ""
		if info, err := e.Info(); err == nil {
			created = info.ModTime().Format("2006-01-02 15:04:05")
		}
		items = append(items, Snapshot{
			Name:      name,
			Time:      t,
			CreatedAt: created,
			IsCover:   cm[rowToken] == name,
			URL: "/api/snapshot/image?token=" + url.QueryEscape(rowToken) +
				"&name=" + url.QueryEscape(name),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Time != items[j].Time {
			return items[i].Time < items[j].Time
		}
		return items[i].Name < items[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleSnapshotJobs(w http.ResponseWriter, r *http.Request) {
	items := s.snapshotJobs(r.URL.Query().Get("token"))
	if items == nil {
		items = []*snapshotJob{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (s *Server) handleSnapshotImage(w http.ResponseWriter, r *http.Request) {
	tk := r.URL.Query().Get("token")
	name := r.URL.Query().Get("name")
	if !validToken(tk) || name == "" || !strings.HasPrefix(name, tk+"_") || !validSnapshotName(name) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	p := filepath.Join(s.paths.Snapshots, name)
	if !inside(s.paths.Snapshots, p) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func (s *Server) handleDownloadInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "download": s.downloadInfo()})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	s.mu.RLock()
	rev := s.rev
	s.mu.RUnlock()
	fmt.Fprintf(w, "retry: 2000\n\nevent: hello\ndata: {\"rev\":%d}\n\n", rev)
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	items := []any{}
	base := filepath.Join(s.paths.Diagnostics, "scrape")
	entries, err := os.ReadDir(base)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(base, e.Name(), "meta.json"))
			if err != nil {
				continue
			}
			var m any
			if jsonx.Unmarshal(b, &m) == nil {
				items = append(items, m)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (s *Server) handleDiagnosticsFile(w http.ResponseWriter, r *http.Request) {
	run := r.URL.Query().Get("run")
	name := r.URL.Query().Get("name")
	if !validRunName(run) || !validDiagName(name) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	base := filepath.Join(s.paths.Diagnostics, "scrape")
	p := filepath.Join(base, run, name)
	if !inside(base, p) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	ct := "text/plain; charset=utf-8"
	switch {
	case strings.HasSuffix(name, ".json"):
		ct = "application/json; charset=utf-8"
	case strings.HasSuffix(name, ".csv"):
		ct = "text/csv; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// ---------- 写接口 ----------

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	return dec.Decode(v)
}

func (s *Server) handleFav(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tokens []string `json:"tokens"`
		On     bool     `json:"on"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Tokens) == 0 {
		bad(w, "tokens 为空")
		return
	}
	if err := s.st.SetFav(body.Tokens, body.On); err != nil {
		bad(w, err.Error())
		return
	}
	val := "0"
	if body.On {
		val = "1"
	}
	s.eachRow(body.Tokens, func(row *model.Row) { row.Fav = val })
	s.Bump(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Tokens)})
}

func (s *Server) handleRate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tokens []string `json:"tokens"`
		Value  int      `json:"value"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Tokens) == 0 {
		bad(w, "tokens 为空")
		return
	}
	if body.Value < 0 {
		body.Value = 0
	}
	if body.Value > 5 {
		body.Value = 5
	}
	if err := s.st.SetRating(body.Tokens, body.Value); err != nil {
		bad(w, err.Error())
		return
	}
	val := strconv.Itoa(body.Value)
	s.eachRow(body.Tokens, func(row *model.Row) { row.Rating = val })
	s.Bump(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Tokens)})
}

func (s *Server) handleMark(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tokens []string `json:"tokens"`
		Status string   `json:"status"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Tokens) == 0 {
		bad(w, "tokens 为空")
		return
	}
	switch body.Status {
	case "new", "done", "failed", "recycled", "sent", "ignored":
	default:
		bad(w, "未知状态: "+body.Status)
		return
	}
	if body.Status == "recycled" {
		if err := s.st.Trash(body.Tokens); err != nil {
			bad(w, err.Error())
			return
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		s.eachRow(body.Tokens, func(row *model.Row) {
			if row.Status != "recycled" {
				row.RecycledFrom = row.Status
			}
			row.Status = "recycled"
			row.RecycledAt = now
		})
	} else {
		if err := s.st.SetStatus(body.Tokens, body.Status); err != nil {
			bad(w, err.Error())
			return
		}
		s.eachRow(body.Tokens, func(row *model.Row) { row.Status = body.Status })
	}
	s.Bump(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Tokens)})
}

func (s *Server) handleWatched(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tokens []string `json:"tokens"`
		On     bool     `json:"on"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Tokens) == 0 {
		bad(w, "tokens 为空")
		return
	}
	if err := s.st.SetWatched(body.Tokens, body.On); err != nil {
		bad(w, err.Error())
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	s.eachRow(body.Tokens, func(row *model.Row) {
		if body.On {
			row.LastPlay = now
		} else {
			row.LastPlay = ""
		}
	})
	s.Bump(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Tokens)})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tokens []string `json:"tokens"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Tokens) == 0 {
		bad(w, "tokens 为空")
		return
	}
	s.mu.RLock()
	prev := map[string]string{}
	for _, row := range s.rows {
		prev[row.Token] = row.RecycledFrom
	}
	s.mu.RUnlock()
	if err := s.st.Restore(body.Tokens); err != nil {
		bad(w, err.Error())
		return
	}
	s.eachRow(body.Tokens, func(row *model.Row) {
		st := prev[row.Token]
		if st == "" {
			st = "new"
		}
		row.Status = st
		row.RecycledAt = ""
		row.RecycledFrom = ""
	})
	s.Bump(true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.Tokens)})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tokens    []string `json:"tokens"`
		Purge     bool     `json:"purge"`
		Permanent bool     `json:"permanent"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.Tokens) == 0 {
		bad(w, "tokens 为空")
		return
	}
	if !body.Purge && !body.Permanent {
		// 未显式要求彻底删除时，只移入回收站，避免误删文件
		if err := s.st.Trash(body.Tokens); err != nil {
			bad(w, err.Error())
			return
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		s.eachRow(body.Tokens, func(row *model.Row) {
			if row.Status != "recycled" {
				row.RecycledFrom = row.Status
			}
			row.Status = "recycled"
			row.RecycledAt = now
		})
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "trashed": len(body.Tokens)})
		return
	}

	s.mu.RLock()
	rows := make([]model.Row, len(s.rows))
	copy(rows, s.rows)
	idx := s.index
	s.mu.RUnlock()

	now := time.Now().Format("2006-01-02 15:04:05")
	var files []string
	var records []model.DeletedToken
	for _, tk := range body.Tokens {
		row, ok := findRow(rows, tk)
		if !ok {
			continue
		}
		if file, ok := resolveFileIn(rows, idx, s.paths.Content, tk); ok {
			files = append(files, file)
		}
		records = append(records, model.DeletedToken{
			Token: row.Token, Source: row.Source, Title: row.Title, DeletedAt: now,
		})
	}
	if len(records) == 0 {
		bad(w, "台账里没有这些条目")
		return
	}
	// 先删台账再删文件：文件删失败只会留下孤立文件（可再扫盘），
	// 反过来则会出现「文件没了、记录还在」的死条目。
	if err := s.st.RemovePermanently(tokensOf(records), records); err != nil {
		bad(w, err.Error())
		return
	}
	var fileErrs []string
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			fileErrs = append(fileErrs, filepath.Base(f)+": "+err.Error())
		}
	}

	set := map[string]bool{}
	for _, d := range records {
		set[d.Token] = true
	}
	s.mu.Lock()
	kept := make([]model.Row, 0, len(s.rows))
	for _, row := range s.rows {
		if !set[row.Token] {
			kept = append(kept, row)
		}
	}
	s.rows = kept
	for _, d := range records {
		s.deleted[strings.ToLower(d.Token)] = d
	}
	deletedCopy := make(map[string]model.DeletedToken, len(s.deleted))
	for k, v := range s.deleted {
		deletedCopy[k] = v
	}
	s.rev++
	s.rowsRev++
	s.mu.Unlock()

	_ = store.SaveDeletedJSON(s.paths.DataDir, deletedCopy)
	s.logbuf.Add("delete", fmt.Sprintf("彻底删除 %d 条", len(records)))
	resp := map[string]any{"ok": true, "deleted": len(records)}
	if len(fileErrs) > 0 {
		s.logbuf.Add("delete", fmt.Sprintf("文件删除失败 %d 个", len(fileErrs)))
		resp["fileErrors"] = fileErrs
	}
	writeJSON(w, http.StatusOK, resp)
}

func tokensOf(list []model.DeletedToken) []string {
	out := make([]string, 0, len(list))
	for _, d := range list {
		out = append(out, d.Token)
	}
	return out
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	changed, err := s.Scan(true)
	if err != nil {
		bad(w, err.Error())
		return
	}
	s.mu.RLock()
	idx := s.index
	downloaded := 0
	for i := range s.rows {
		if s.rows[i].FileName != "" {
			downloaded++
		}
	}
	total := len(s.rows)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"files":      len(idx.Files),
		"downloaded": downloaded,
		"missing":    total - downloaded,
		"changed":    changed,
		"partial":    len(idx.Partial),
		"dir":        s.paths.Content,
		"exists":     idx.Exists,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	var body struct {
		Proxy               *string  `json:"proxy"`
		Concurrent          *int     `json:"concurrent"`
		FFmpegPath          *string  `json:"ffmpegPath"`
		RetryCount          *int     `json:"retryCount"`
		RetryDelay          *int     `json:"retryDelay"`
		HTTPLimitKB         *int     `json:"httpLimitKB"`
		HTTPConnections     *int     `json:"httpConnections"`
		PerSourceConcurrent *int     `json:"perSourceConcurrent"`
		PlayerVolume        *float64 `json:"playerVolume"`
		PlayerMuted         *bool    `json:"playerMuted"`
		PlayerPlayMode      *string  `json:"playerPlayMode"`
		TorrentLimitKB      *int     `json:"torrentLimitKB"`
		TorrentBackend      *string  `json:"torrentBackend"`
		QbtURL              *string  `json:"qbtUrl"`
		QbtUser             *string  `json:"qbtUser"`
		QbtPass             *string  `json:"qbtPass"`
		SourceIndexURL      *string  `json:"sourceIndexUrl"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	s.mu.Lock()
	cfg := s.settings
	s.mu.Unlock()

	if body.Proxy != nil {
		p := strings.TrimSpace(*body.Proxy)
		if p != "" {
			u, err := url.Parse(p)
			if err != nil || u.Scheme == "" || u.Host == "" {
				bad(w, "代理格式不对，例如 http://127.0.0.1:7890")
				return
			}
			switch strings.ToLower(u.Scheme) {
			case "http", "https", "socks5", "socks5h":
			default:
				bad(w, "代理只支持 http/https/socks5")
				return
			}
		}
		cfg.Proxy = p
	}
	if body.Concurrent != nil {
		if *body.Concurrent < 1 || *body.Concurrent > 16 {
			bad(w, "并发数必须在 1–16 之间")
			return
		}
		cfg.Concurrent = *body.Concurrent
	}
	if body.FFmpegPath != nil {
		p := strings.TrimSpace(*body.FFmpegPath)
		if p != "" {
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				bad(w, "FFmpeg 路径不存在："+p)
				return
			}
		}
		cfg.FFmpegPath = p
	}
	if body.RetryCount != nil {
		if *body.RetryCount < 1 || *body.RetryCount > 8 {
			bad(w, "重试次数必须在 1–8 之间")
			return
		}
		cfg.RetryCount = *body.RetryCount
	}
	if body.RetryDelay != nil {
		if *body.RetryDelay < 1 || *body.RetryDelay > 30 {
			bad(w, "重试间隔必须在 1–30 秒之间")
			return
		}
		cfg.RetryDelay = *body.RetryDelay
	}
	if body.TorrentLimitKB != nil {
		if *body.TorrentLimitKB < 0 || *body.TorrentLimitKB > 102400 {
			bad(w, "磁力限速需在 0–102400 KB/s")
			return
		}
		cfg.TorrentLimitKB = *body.TorrentLimitKB
	}
	if body.HTTPLimitKB != nil {
		if *body.HTTPLimitKB < 0 || *body.HTTPLimitKB > 102400 {
			bad(w, "下载限速需在 0–102400 KB/s")
			return
		}
		cfg.HTTPLimitKB = *body.HTTPLimitKB
	}
	if body.HTTPConnections != nil {
		if *body.HTTPConnections < 1 || *body.HTTPConnections > 16 {
			bad(w, "单文件连接数需在 1–16 之间")
			return
		}
		cfg.HTTPConnections = *body.HTTPConnections
	}
	if body.PerSourceConcurrent != nil {
		if *body.PerSourceConcurrent < 1 || *body.PerSourceConcurrent > 8 {
			bad(w, "每来源并发需在 1–8 之间")
			return
		}
		cfg.PerSourceConcurrent = *body.PerSourceConcurrent
	}
	if body.PlayerVolume != nil {
		if *body.PlayerVolume < 0 || *body.PlayerVolume > 1 {
			bad(w, "音量需在 0–1 之间")
			return
		}
		cfg.PlayerVolume = *body.PlayerVolume
		cfg.PlayerVolumeSet = true
	}
	if body.PlayerMuted != nil {
		cfg.PlayerMuted = *body.PlayerMuted
		cfg.PlayerVolumeSet = true
	}
	if body.PlayerPlayMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*body.PlayerPlayMode))
		switch mode {
		case "off", "one", "next", "all":
			cfg.PlayerPlayMode = mode
		default:
			bad(w, "播放模式只能是 off/one/next/all")
			return
		}
	}
	if body.TorrentBackend != nil {
		v := strings.ToLower(strings.TrimSpace(*body.TorrentBackend))
		if v != "builtin" && v != "qbittorrent" {
			bad(w, "磁力后端需为 builtin 或 qbittorrent")
			return
		}
		cfg.TorrentBackend = v
	}
	if body.QbtURL != nil {
		cfg.QbtURL = strings.TrimSpace(*body.QbtURL)
	}
	if body.QbtUser != nil {
		cfg.QbtUser = strings.TrimSpace(*body.QbtUser)
	}
	if body.QbtPass != nil {
		cfg.QbtPass = *body.QbtPass
	}
	if body.SourceIndexURL != nil {
		v := strings.TrimSpace(*body.SourceIndexURL)
		if v != "" {
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				bad(w, "源索引地址必须是 http(s) 绝对地址")
				return
			}
		}
		cfg.SourceIndexURL = v
	}
	if err := config.SaveSettings(s.paths, cfg); err != nil {
		bad(w, err.Error())
		return
	}
	s.mu.Lock()
	s.settings = cfg
	s.rev++
	s.mu.Unlock()
	if s.srcMgr != nil {
		s.srcMgr.SetProxy(cfg.Proxy)
	}
	websource.Configure(cfg.Proxy)
	if s.dl != nil {
		s.dl.SetSettings(download.Settings{
			Concurrent:          cfg.Concurrent,
			FFmpeg:              cfg.FFmpegPath,
			Proxy:               cfg.Proxy,
			RetryCount:          cfg.RetryCount,
			RetryDelay:          time.Duration(cfg.RetryDelay) * time.Second,
			TorrentLimitKB:      cfg.TorrentLimitKB,
			HTTPLimitKB:         cfg.HTTPLimitKB,
			HTTPConnections:     cfg.HTTPConnections,
			PerSourceConcurrent: cfg.PerSourceConcurrent,
			TorrentBackend:      cfg.TorrentBackend,
			QbtURL:              cfg.QbtURL, QbtUser: cfg.QbtUser, QbtPass: cfg.QbtPass,
		})
	}
	s.logbuf.Add("config", fmt.Sprintf("代理=%s 并发=%d", cfg.Proxy, cfg.Concurrent))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "proxy": cfg.Proxy, "concurrent": cfg.Concurrent,
	})
}

var palette = []string{"#4f8cff", "#f2a33c", "#3fb950", "#d2a8ff", "#f778ba", "#39c5cf", "#e3b341", "#ff7b72"}

func (s *Server) handleCollectionsWrite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action     string   `json:"action"`
		ID         int64    `json:"id"`
		TargetID   int64    `json:"targetId"`
		Name       string   `json:"name"`
		Color      string   `json:"color"`
		CoverToken string   `json:"coverToken"`
		Tokens     []string `json:"tokens"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	switch body.Action {
	case "create":
		name := strings.TrimSpace(body.Name)
		if name == "" {
			bad(w, "合集名不能为空")
			return
		}
		list, _ := s.st.Collections()
		color := body.Color
		if color == "" {
			color = palette[len(list)%len(palette)]
		}
		id, err := s.st.CreateCollection(name, color)
		if err != nil {
			bad(w, "创建合集失败（可能重名）："+err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	case "rename", "color":
		if body.ID <= 0 {
			bad(w, "合集 id 不合法")
			return
		}
		if err := s.st.UpdateCollection(body.ID, strings.TrimSpace(body.Name), body.Color); err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "merge":
		if body.ID <= 0 || body.TargetID <= 0 || body.ID == body.TargetID {
			bad(w, "合并标签参数不正确")
			return
		}
		if err := s.st.MergeCollection(body.ID, body.TargetID); err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "delete":
		if body.ID <= 0 {
			bad(w, "合集 id 不合法")
			return
		}
		if err := s.st.DeleteCollection(body.ID); err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "cover":
		if body.ID <= 0 {
			bad(w, "合集 id 不合法")
			return
		}
		token := strings.TrimSpace(body.CoverToken)
		if token != "" {
			tokens, err := s.st.CollectionTokens(body.ID)
			if err != nil {
				bad(w, err.Error())
				return
			}
			found := false
			for _, t := range tokens {
				if t == token {
					found = true
					break
				}
			}
			if !found {
				bad(w, "封面必须来自该合集中的条目")
				return
			}
		}
		if err := s.st.SetCollectionCover(body.ID, token); err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "add", "remove":
		if body.ID <= 0 || len(body.Tokens) == 0 {
			bad(w, "参数不完整")
			return
		}
		var err error
		if body.Action == "add" {
			err = s.st.AddToCollection(body.ID, body.Tokens)
		} else {
			err = s.st.RemoveFromCollection(body.ID, body.Tokens)
		}
		if err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		bad(w, "未知动作: "+body.Action)
	}
}

func (s *Server) handleClientLog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		URL     string `json:"url"`
		Ms      any    `json:"ms"`
		Stack   string `json:"stack"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	rec := map[string]any{
		"t":     time.Now().Format("2006-01-02 15:04:05.000"),
		"level": body.Level,
		"msg":   body.Message,
		"url":   body.URL,
		"ms":    body.Ms,
		"stack": body.Stack,
	}
	b, _ := json.Marshal(rec)
	if err := os.MkdirAll(s.paths.Logs, 0o755); err == nil {
		p := filepath.Join(s.paths.Logs, "client.log")
		if info, err := os.Stat(p); err == nil && info.Size() > 1<<20 {
			_ = os.Rename(p, p+".1")
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.Write(append(b, '\n'))
			_ = f.Close()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRevealContent(w http.ResponseWriter, r *http.Request) {
	if err := openPath(s.paths.Content); err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": s.paths.Content})
}

// ---------- 静态资源 ----------

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	f, err := s.assets.Open(name)
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
	ct := mime.TypeByExtension(filepath.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	if strings.HasSuffix(name, ".js") {
		ct = "application/javascript; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	// 未注册的 /api/* 返回 JSON 404，别落到前端页面
	if strings.HasPrefix(p, "api/") {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "unknown api: " + p})
		return
	}
	if p != "" && !strings.Contains(p, "..") {
		if f, err := s.assets.Open(p); err == nil {
			stat, err := f.Stat()
			if err == nil && !stat.IsDir() {
				ct := mime.TypeByExtension(filepath.Ext(p))
				if ct == "" {
					ct = "application/octet-stream"
				}
				if strings.HasSuffix(p, ".js") {
					ct = "application/javascript; charset=utf-8"
				}
				w.Header().Set("Content-Type", ct)
				w.Header().Set("Cache-Control", "no-store")
				_, _ = io.Copy(w, f)
				_ = f.Close()
				return
			}
			_ = f.Close()
		}
	}
	// React Router 深路由回退到 index.html；有扩展名的一律 404
	if filepath.Ext(p) != "" {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		http.Error(w, "web/dist/index.html 缺失，请先构建前端", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// ---------- 工具 ----------

func (s *Server) eachRow(tokens []string, fn func(*model.Row)) {
	set := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		set[t] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		if set[s.rows[i].Token] {
			fn(&s.rows[i])
		}
	}
}

func findRow(rows []model.Row, token string) (model.Row, bool) {
	for i := range rows {
		if rows[i].Token == token {
			return rows[i], true
		}
	}
	lower := strings.ToLower(token)
	for i := range rows {
		if strings.ToLower(rows[i].Token) == lower {
			return rows[i], true
		}
	}
	return model.Row{}, false
}

func validToken(tk string) bool {
	if tk == "" || len(tk) > 200 || strings.Contains(tk, "..") {
		return false
	}
	for _, r := range tk {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '@', r == '.':
		default:
			return false
		}
	}
	return true
}

func validSnapshotName(name string) bool {
	if name == "" || len(name) > 260 || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '@', r == '.':
		default:
			return false
		}
	}
	return true
}

func validRunName(s string) bool {
	if s == "" || len(s) > 200 || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '@', r == '.', r == ' ':
		default:
			return false
		}
	}
	return true
}

func validDiagName(s string) bool {
	switch s {
	case "result.csv", "meta.json", "progress.json", "scrape.log":
		return true
	}
	return false
}

// inside 判断 p 是否在 base 目录内（防路径穿越）。
func inside(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func openPath(p string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", p).Start()
	case "darwin":
		return exec.Command("open", p).Start()
	default:
		return exec.Command("xdg-open", p).Start()
	}
}
