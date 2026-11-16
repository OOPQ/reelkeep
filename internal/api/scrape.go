package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// scrapeJob 是一次抓取任务（同一时间只允许一个）。
type scrapeJob struct {
	mu       sync.Mutex
	Source   string
	Rank     string
	Pages    []int
	Limit    int
	Running  bool
	Started  time.Time
	Finished time.Time
	Phase    string
	Done     int
	Total    int
	Added    int
	Updated  int
	Skipped  int
	Err      string
	Logs     []string
	Cancel   context.CancelFunc
}

func (j *scrapeJob) logf(format string, args ...any) {
	line := fmt.Sprintf("%s %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	j.mu.Lock()
	j.Logs = append(j.Logs, line)
	if len(j.Logs) > 200 {
		j.Logs = j.Logs[len(j.Logs)-200:]
	}
	j.mu.Unlock()
}

func (j *scrapeJob) setPhase(phase string) {
	j.mu.Lock()
	j.Phase = phase
	j.mu.Unlock()
}

func (j *scrapeJob) progress(done, total int) {
	j.mu.Lock()
	j.Done = done
	if total > 0 {
		j.Total = total
	}
	j.mu.Unlock()
}

func (j *scrapeJob) finish(err error) {
	j.mu.Lock()
	j.Running = false
	j.Finished = time.Now()
	j.Phase = "done"
	if err != nil {
		j.Err = err.Error()
	}
	j.mu.Unlock()
}

func (j *scrapeJob) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Running
}

func (j *scrapeJob) info() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	elapsed := 0.0
	if j.Running {
		elapsed = time.Since(j.Started).Seconds()
	} else if !j.Finished.IsZero() {
		elapsed = j.Finished.Sub(j.Started).Seconds()
	}
	speed, eta := 0.0, 0.0
	if elapsed > 0 && j.Done > 0 {
		speed = float64(j.Done) / elapsed
		if j.Total > j.Done {
			eta = float64(j.Total-j.Done) / speed
		}
	}
	progress := map[string]any{
		"phase": j.Phase, "done": j.Done, "total": j.Total,
		"elapsed": round1(elapsed), "speed": round1(speed), "eta": round1(eta),
		"at":    time.Now().Format("15:04:05"),
		"added": j.Added, "updated": j.Updated, "count": j.Added + j.Updated,
		"kind": "scrape", "error": j.Err,
	}
	if !j.Finished.IsZero() {
		progress["finishedAt"] = j.Finished.UnixMilli()
	}
	return map[string]any{
		"running":  j.Running,
		"elapsed":  round1(elapsed),
		"rank":     j.Rank,
		"progress": progress,
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// jobInfo 返回给 /api/state 的 job 字段；结束 60 秒后自动清理。
func (s *Server) jobInfo() any {
	s.jobMu.Lock()
	j := s.job
	if j == nil {
		s.jobMu.Unlock()
		return nil
	}
	if !j.isRunning() {
		j.mu.Lock()
		finished := j.Finished
		j.mu.Unlock()
		if !finished.IsZero() && time.Since(finished) > 60*time.Second {
			s.job = nil
			s.jobMu.Unlock()
			return nil
		}
	}
	s.jobMu.Unlock()
	return j.info()
}

// handleScrape 启动一次抓取任务。
func (s *Server) handleScrape(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source      string `json:"source"`
		Rank        string `json:"rank"`
		URL         string `json:"url"`
		Pages       []int  `json:"pages"`
		Limit       int    `json:"limit"`
		SkipDeleted *bool  `json:"skipDeleted"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if body.Source == "" {
		bad(w, "请选择源")
		return
	}
	if body.URL != "" {
		bad(w, "当前版本还不支持「链接抓取」，请先用「获取数据」按榜单抓取")
		return
	}
	m, ok := s.reg.Manifest(body.Source)
	if !ok {
		bad(w, "未知源："+body.Source)
		return
	}
	if !hasCap(m.Capabilities, "scrape") {
		bad(w, "源 "+body.Source+" 不支持抓取")
		return
	}
	if body.Rank != "" && len(m.Ranks) > 0 && !hasRank(m.Ranks, body.Rank) {
		bad(w, "源 "+body.Source+" 没有榜单："+body.Rank)
		return
	}
	src, ok := s.reg.Get(body.Source)
	if !ok {
		bad(w, "源 "+body.Source+" 没有实现")
		return
	}
	pages := body.Pages
	if len(pages) == 0 {
		pages = []int{1}
	}
	if len(pages) > 20 {
		bad(w, "一次最多 20 页")
		return
	}
	if body.Limit < 0 || body.Limit > 500 {
		bad(w, "limit 必须在 0–500 之间")
		return
	}
	skipDeleted := true
	if body.SkipDeleted != nil {
		skipDeleted = *body.SkipDeleted
	}

	ctx, cancel := context.WithCancel(context.Background())
	j := &scrapeJob{
		Source: body.Source, Rank: body.Rank, Pages: pages, Limit: body.Limit,
		Running: true, Started: time.Now(), Phase: "list", Cancel: cancel,
	}
	s.jobMu.Lock()
	if s.job != nil && s.job.isRunning() {
		s.jobMu.Unlock()
		cancel()
		bad(w, "已有抓取任务在跑，等它结束")
		return
	}
	s.job = j
	s.jobMu.Unlock()

	go s.runScrape(ctx, j, src, skipDeleted)
	s.logbuf.Add("scrape", fmt.Sprintf("开始抓取 %s / %s（%d 页）", body.Source, body.Rank, len(pages)))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": j.info()})
}

func (s *Server) runScrape(ctx context.Context, j *scrapeJob, src source.Source, skipDeleted bool) {
	runID := time.Now().Format("20060102-150405") + "-" + sanitizeName(j.Source) + "-" + sanitizeName(j.Rank)
	j.logf("run=%s source=%s rank=%s pages=%v limit=%d", runID, j.Source, j.Rank, j.Pages, j.Limit)

	var rows []model.Row
	err := src.Scrape(ctx, source.ScrapeRequest{
		Rank:     j.Rank,
		Pages:    j.Pages,
		Limit:    j.Limit,
		PageSize: 50,
		Progress: func(phase string, done, total int) {
			j.setPhase(phase)
			j.progress(done, total)
		},
	}, func(r model.Row) error {
		rows = append(rows, r)
		j.mu.Lock()
		j.Done = len(rows)
		j.mu.Unlock()
		return nil
	})
	if err != nil {
		j.logf("抓取失败: %v", err)
		s.writeScrapeDiag(runID, j, rows, "failed", err.Error(), 0, 0, 0)
		j.finish(err)
		if s.srcMgr != nil {
			s.srcMgr.RecordResult(j.Source, err)
		}
		s.logbuf.Add("scrape", "抓取失败："+err.Error())
		return
	}

	added, updated, skipped, mergeErr := s.applyScrape(rows, skipDeleted)
	if mergeErr != nil {
		j.logf("入库失败: %v", mergeErr)
		s.writeScrapeDiag(runID, j, rows, "failed", mergeErr.Error(), added, updated, skipped)
		j.finish(mergeErr)
		if s.srcMgr != nil {
			s.srcMgr.RecordResult(j.Source, mergeErr)
		}
		return
	}

	j.mu.Lock()
	j.Added, j.Updated, j.Skipped = added, updated, skipped
	j.Total = len(rows)
	j.mu.Unlock()
	j.logf("完成: 抓取 %d，新增 %d，更新 %d，跳过 %d", len(rows), added, updated, skipped)
	s.writeScrapeDiag(runID, j, rows, "ok", "", added, updated, skipped)
	j.finish(nil)
	if s.srcMgr != nil {
		s.srcMgr.RecordResult(j.Source, nil)
	}
	s.logbuf.Add("scrape", fmt.Sprintf("%s 抓取完成：新增 %d，更新 %d", j.Source, added, updated))
}

// applyScrape 把抓取结果合并进台账：新条目入库，旧条目只覆盖抓取字段，用户字段保持。
func (s *Server) applyScrape(in []model.Row, skipDeleted bool) (added, updated, skipped int, err error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	s.mu.Lock()
	idx := make(map[string]int, len(s.rows))
	for i := range s.rows {
		idx[strings.ToLower(s.rows[i].Token)] = i
	}
	dirty := make([]model.Row, 0, len(in))
	for _, r := range in {
		if r.Token == "" || r.URL == "" {
			skipped++
			continue
		}
		key := strings.ToLower(r.Token)
		if skipDeleted {
			if _, ok := s.deleted[key]; ok {
				skipped++
				continue
			}
		}
		if i, ok := idx[key]; ok {
			cur := &s.rows[i]
			if cur.Status == "recycled" {
				skipped++
				continue
			}
			mergeScraped(cur, r, now)
			dirty = append(dirty, *cur)
			updated++
			continue
		}
		r.Status = "new"
		if r.FirstSeen == "" {
			r.FirstSeen = now
		}
		r.LastSeen = now
		s.rows = append(s.rows, r)
		idx[key] = len(s.rows) - 1
		dirty = append(dirty, r)
		added++
	}
	if added+updated > 0 {
		s.rev++
		s.rowsRev++
	}
	s.mu.Unlock()

	if len(dirty) > 0 {
		if err := s.st.UpsertRows(dirty); err != nil {
			return 0, 0, 0, err
		}
	}
	return added, updated, skipped, nil
}

func mergeScraped(dst *model.Row, src model.Row, now string) {
	setIf := func(d *string, v string) {
		if strings.TrimSpace(v) != "" {
			*d = v
		}
	}
	setIf(&dst.Title, src.Title)
	setIf(&dst.URL, src.URL)
	setIf(&dst.CoverURL, src.CoverURL)
	setIf(&dst.PageURL, src.PageURL)
	setIf(&dst.Duration, src.Duration)
	setIf(&dst.Seconds, src.Seconds)
	setIf(&dst.Res, src.Res)
	setIf(&dst.Views, src.Views)
	setIf(&dst.Likes, src.Likes)
	setIf(&dst.Uploaded, src.Uploaded)
	setIf(&dst.Extra, src.Extra)
	setIf(&dst.Rank, src.Rank)
	setIf(&dst.Page, src.Page)
	setIf(&dst.DownloadKind, src.DownloadKind)
	dst.LastSeen = now
	// Status / Fav / Rating / Note / PlayCount / LastPlay / File* / Recycled* 一律不动
}

// writeScrapeDiag 落一轮诊断：result.csv / meta.json / progress.json / scrape.log。
func (s *Server) writeScrapeDiag(runID string, j *scrapeJob, rows []model.Row, status, errText string, added, updated, skipped int) {
	dir := filepath.Join(s.paths.Diagnostics, "scrape", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	var b strings.Builder
	b.WriteString("\ufeff")
	writeCSVLine(&b, model.Header)
	for i := range rows {
		writeCSVLine(&b, rowStrings(&rows[i]))
	}
	if err := os.WriteFile(filepath.Join(dir, "result.csv"), []byte(b.String()), 0o644); err != nil {
		return
	}

	meta := map[string]any{
		"run": runID, "status": status, "source": j.Source, "rank": j.Rank,
		"count": len(rows), "added": added, "updated": updated, "skipped": skipped,
		"error": errText, "finishedAt": time.Now().Format("2006-01-02 15:04:05"),
		"files": []string{"result.csv", "progress.json", "scrape.log"},
		"url":   "/api/diagnostics/scrape/file?run=" + runID,
	}
	mb, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), mb, 0o644)

	j.mu.Lock()
	done, total := j.Done, j.Total
	logs := append([]string{}, j.Logs...)
	j.mu.Unlock()
	prog := map[string]any{
		"phase": "done", "done": done, "total": total,
		"elapsed": 0, "speed": 0, "eta": 0,
		"at": time.Now().Format("15:04:05"), "added": added, "updated": updated,
		"count": len(rows), "kind": "scrape", "error": errText,
	}
	pb, _ := json.MarshalIndent(prog, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "progress.json"), pb, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "scrape.log"), []byte(strings.Join(logs, "\n")), 0o644)
}

// handleJobCancel 取消当前抓取。
func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	s.jobMu.Lock()
	j := s.job
	s.jobMu.Unlock()
	if j == nil || !j.isRunning() {
		bad(w, "没有正在运行的抓取任务")
		return
	}
	j.mu.Lock()
	cancel := j.Cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.logbuf.Add("scrape", "已请求取消抓取")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func sanitizeName(s string) string {
	if s == "" {
		return "all"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func hasCap(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

func hasRank(ranks []source.Rank, key string) bool {
	for _, r := range ranks {
		if r.Key == key {
			return true
		}
	}
	return false
}
