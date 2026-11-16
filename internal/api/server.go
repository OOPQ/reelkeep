package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/download"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/scan"
	"github.com/OOPQ/reelkeep/internal/source"
	"github.com/OOPQ/reelkeep/internal/store"
)

// APIVersion 与旧版 /api/state 的 api 字段保持一致：前端靠它识别「后台还是旧进程」。
const APIVersion = 4

// LogEntry 是一条界面日志。
type LogEntry struct {
	Time    string `json:"time"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// Log 是最近 200 条内存日志。
type Log struct {
	mu      sync.Mutex
	entries []LogEntry
}

// Add 追加一条日志。
func (l *Log) Add(kind, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, LogEntry{Time: time.Now().Format("15:04:05"), Kind: kind, Message: msg})
	if len(l.entries) > 200 {
		l.entries = append([]LogEntry(nil), l.entries[len(l.entries)-200:]...)
	}
}

// Last 取最近 n 条。
func (l *Log) Last(n int) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.entries) {
		n = len(l.entries)
	}
	out := make([]LogEntry, n)
	copy(out, l.entries[len(l.entries)-n:])
	return out
}

// SlowReq 是慢请求记录。
type SlowReq struct {
	T    string `json:"t"`
	Path string `json:"path"`
	Ms   int64  `json:"ms"`
}

// Metrics 是接口耗时指标（单机场景，内存里够用）。
type Metrics struct {
	mu           sync.Mutex
	total        int64
	sumMs        float64
	maxMs        int64
	maxPath      string
	slow         []SlowReq
	clientErrors []any
	clientWarns  int64
}

func (m *Metrics) record(path string, ms int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total++
	m.sumMs += float64(ms)
	if ms > m.maxMs {
		m.maxMs, m.maxPath = ms, path
	}
	if ms >= 800 {
		m.slow = append([]SlowReq{{T: time.Now().Format("15:04:05"), Path: path, Ms: ms}}, m.slow...)
		if len(m.slow) > 20 {
			m.slow = m.slow[:20]
		}
	}
}

func (m *Metrics) snapshot(uptimeMin float64) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	avg := 0.0
	if m.total > 0 {
		avg = m.sumMs / float64(m.total)
	}
	slow := m.slow
	if slow == nil {
		slow = []SlowReq{}
	}
	clientErrors := m.clientErrors
	if clientErrors == nil {
		clientErrors = []any{}
	}
	return map[string]any{
		"total":        m.total,
		"avgMs":        avg,
		"maxMs":        m.maxMs,
		"maxPath":      m.maxPath,
		"slow":         slow,
		"clientErrors": clientErrors,
		"clientWarns":  m.clientWarns,
		"uptime":       uptimeMin,
	}
}

// Server 是 HTTP 层的全部状态。
type Server struct {
	paths    config.Paths
	st       *store.Store
	reg      *source.Registry
	srcMgr   SourceManager
	dl       *download.Manager
	settings config.Settings
	version  string
	assets   fs.FS // dist 目录
	started  time.Time

	mu           sync.RWMutex
	rows         []model.Row
	index        scan.Index
	deleted      map[string]model.DeletedToken
	rev          int64
	rowsRev      int64
	coverMissing int

	scanMu   sync.Mutex
	logbuf   *Log
	metrics  *Metrics
	mediaKey string
	port     int

	jobMu sync.Mutex
	job   *scrapeJob

	snapMu   sync.Mutex
	snapJobs map[string]*snapshotJob

	// 测试用：替换封面下载（默认走 fetchImage 的网络实现）
	imageFetch func(string) ([]byte, error)

	desktop      bool
	win          WindowController
	picker       func() (string, error)
	loginWin     func(id, loginURL string) error
	loginCapture func(id string) (string, error)
}

// SetVersion 记录当前版本（更新检查用）。
func (s *Server) SetVersion(v string) {
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// New 创建 Server。assets 应为 dist 的 fs.FS。
func New(paths config.Paths, st *store.Store, reg *source.Registry, mgr SourceManager,
	dl *download.Manager, settings config.Settings, assets fs.FS) *Server {
	return &Server{
		paths:    paths,
		st:       st,
		reg:      reg,
		srcMgr:   mgr,
		dl:       dl,
		settings: settings,
		assets:   assets,
		started:  time.Now(),
		logbuf:   &Log{},
		metrics:  &Metrics{},
		deleted:  map[string]model.DeletedToken{},
		snapJobs: map[string]*snapshotJob{},
	}
}

// SetPort 让媒体地址能拼出正确端口。
func (s *Server) SetPort(p int) { s.port = p }

// Port 返回监听端口。
func (s *Server) Port() int { return s.port }

// Log 暴露日志缓冲区。
func (s *Server) LogBuffer() *Log { return s.logbuf }

func randomKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Boot 加载台账、删除记录，并做一次启动扫盘。
func (s *Server) Boot() error {
	rows, err := s.st.LoadRows()
	if err != nil {
		return fmt.Errorf("读取台账: %w", err)
	}
	s.mu.Lock()
	s.rows = rows
	s.deleted = s.st.LoadDeleted(s.paths.DataDir)
	s.mediaKey = randomKey()
	s.rev++
	s.rowsRev++
	s.mu.Unlock()

	if _, err := s.Scan(false); err != nil {
		return err
	}
	s.logbuf.Add("boot", fmt.Sprintf("已加载 %d 条台账，媒体目录 %s", len(rows), s.paths.Content))
	return nil
}

// Scan 扫盘并回填（manual=true 时由「扫库」按钮触发），返回落库行数。
func (s *Server) Scan(manual bool) (int, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	idx := scan.Build(s.paths.Content)
	s.mu.Lock()
	rows := s.rows
	dirty, _, _ := idx.Apply(rows, false)
	s.rows = rows
	s.index = idx
	s.mu.Unlock()

	if len(dirty) > 0 {
		if err := s.st.UpsertRows(dirty); err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	s.rev++
	s.rowsRev++
	s.mu.Unlock()
	s.refreshCoverMissing()
	if manual {
		s.logbuf.Add("scan", fmt.Sprintf("扫描 %s：文件 %d / 台账 %d，变更 %d",
			s.paths.Content, len(idx.Files), len(rows), len(dirty)))
	}
	return len(dirty), nil
}

func (s *Server) refreshCoverMissing() {
	s.mu.RLock()
	rows := s.rows
	s.mu.RUnlock()
	missing := 0
	for i := range rows {
		r := &rows[i]
		if r.CoverURL == "" && r.FileName == "" {
			continue
		}
		tk := r.Token
		if _, err := os.Stat(filepath.Join(s.paths.Thumbs, tk+".jpg")); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.paths.Covers, tk+".jpg")); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.paths.Covers, tk+".missing")); err == nil {
			continue
		}
		missing++
	}
	s.mu.Lock()
	s.coverMissing = missing
	s.mu.Unlock()
}

// ---------- 统计 / 派生数据 ----------

// ResCount 是分辨率分布。
type ResCount struct {
	Res   string `json:"res"`
	Count int    `json:"count"`
}

// Stats 与旧版 /api/state.stats 字段一一对应。
type Stats struct {
	Total        int          `json:"total"`
	New          int          `json:"new"`
	Done         int          `json:"done"`
	Recycled     int          `json:"recycled"`
	Failed       int          `json:"failed"`
	Hours        float64      `json:"hours"`
	TodayAdded   int          `json:"todayAdded"`
	ByRes        []ResCount   `json:"byRes"`
	LedgerPath   string       `json:"ledgerPath"`
	LastSeen     string       `json:"lastSeen"`
	Busy         bool         `json:"busy"`
	Downloaded   int          `json:"downloaded"`
	Missing      int          `json:"missing"`
	Favorites    int          `json:"favorites"`
	CoverMissing int          `json:"coverMissing"`
	Watched      int          `json:"watched"`
	ToWatch      int          `json:"toWatch"`
	DiskGB       float64      `json:"diskGB"`
	ContentDir   string       `json:"contentDir"`
	DirExists    bool         `json:"dirExists"`
	FileCount    int          `json:"fileCount"`
	PartialCount int          `json:"partialCount"`
	DirGB        float64      `json:"dirGB"`
	Orphans      []scan.Entry `json:"orphans"`
	OrphanCount  int          `json:"orphanCount"`
	LastScan     string       `json:"lastScan"`
}

func statsFrom(rows []model.Row, idx scan.Index, coverMissing int, ledgerPath, contentDir string) Stats {
	st := Stats{LedgerPath: ledgerPath, ContentDir: contentDir, DirExists: idx.Exists}
	counts := map[string]int{}
	resCount := map[string]int{}
	var secs int64
	var bytesSum float64
	today := time.Now().Format("2006-01-02")
	var lastSeen string
	for i := range rows {
		r := &rows[i]
		if _, ok := counts[r.Status]; !ok {
			counts[r.Status] = 0
		}
		counts[r.Status]++
		if v, err := strconv.ParseInt(r.Seconds, 10, 64); err == nil && v > 0 {
			secs += v
		}
		if v, err := strconv.ParseFloat(r.FileSize, 64); err == nil && v > 0 {
			bytesSum += v
		}
		if r.FileName != "" {
			st.Downloaded++
			if r.LastPlay == "" {
				st.ToWatch++
			}
		}
		if r.LastPlay != "" {
			st.Watched++
		}
		if r.Fav == "1" {
			st.Favorites++
		}
		if strings.HasPrefix(r.FirstSeen, today) {
			st.TodayAdded++
		}
		if r.Res != "" {
			resCount[r.Res]++
		}
		if r.LastSeen > lastSeen {
			lastSeen = r.LastSeen
		}
	}
	st.Total = len(rows) - counts["recycled"]
	st.New = counts["new"]
	st.Done = counts["done"]
	st.Recycled = counts["recycled"]
	st.Failed = counts["failed"]
	st.Hours = round2(float64(secs) / 3600)
	st.Missing = len(rows) - st.Downloaded
	st.CoverMissing = coverMissing
	st.DiskGB = round2(bytesSum / (1 << 30))
	st.LastSeen = lastSeen
	st.FileCount = len(idx.Files)
	st.PartialCount = len(idx.Partial)
	for _, f := range idx.Files {
		st.DirGB += float64(f.Size)
	}
	st.DirGB = round2(st.DirGB / (1 << 30))
	for res, n := range resCount {
		st.ByRes = append(st.ByRes, ResCount{Res: res, Count: n})
	}
	sort.Slice(st.ByRes, func(i, j int) bool {
		if st.ByRes[i].Count != st.ByRes[j].Count {
			return st.ByRes[i].Count > st.ByRes[j].Count
		}
		return st.ByRes[i].Res < st.ByRes[j].Res
	})
	if len(st.ByRes) > 6 {
		st.ByRes = st.ByRes[:6]
	}
	if st.ByRes == nil {
		st.ByRes = []ResCount{}
	}
	orphans := make([]scan.Entry, 0, len(idx.Files))
	ledger := make(map[string]bool, len(rows))
	for i := range rows {
		ledger[strings.ToLower(rows[i].Token)] = true
	}
	for k, f := range idx.Files {
		if !ledger[k] {
			orphans = append(orphans, f)
		}
	}
	st.OrphanCount = len(orphans)
	if len(orphans) > 30 {
		orphans = orphans[:30]
	}
	if orphans == nil {
		orphans = []scan.Entry{}
	}
	st.Orphans = orphans
	st.LastScan = time.Now().Format("2006-01-02 15:04:05")
	return st
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// countCovers 只扫封面目录（O(文件数)），避免 /api/state 轮询时逐行 stat。
func countCovers(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.HasSuffix(name, ".jpg") {
			continue
		}
		if st, err := e.Info(); err == nil && st.Size() > 1024 {
			n++
		}
	}
	return n
}

// Project 是 /api/state 和 /api/project 的项目快照。
type Project struct {
	Root       string  `json:"root"`
	Data       string  `json:"data"`
	Media      string  `json:"media"`
	Covers     string  `json:"covers"`
	Exists     bool    `json:"exists"`
	VideoCount int     `json:"videoCount"`
	VideoGB    float64 `json:"videoGB"`
	CoverCount int     `json:"coverCount"`
	DiskGB     float64 `json:"diskGB"`
	Ledger     string  `json:"ledger"`
	Database   string  `json:"database"`
}

func (s *Server) project() Project {
	s.mu.RLock()
	idx := s.index
	rows := append([]model.Row(nil), s.rows...)
	s.mu.RUnlock()
	var gb float64
	for _, f := range idx.Files {
		gb += float64(f.Size)
	}
	videoCount := len(idx.Files)
	var rowBytes float64
	rowVideos := 0
	for i := range rows {
		if rows[i].FileName == "" {
			continue
		}
		rowVideos++
		if v, err := strconv.ParseFloat(rows[i].FileSize, 64); err == nil && v > 0 {
			rowBytes += v
		}
	}
	if rowVideos > videoCount {
		videoCount = rowVideos
	}
	if rowBytes > gb {
		gb = rowBytes
	}
	return Project{
		Root:       s.paths.Root,
		Data:       s.paths.DataDir,
		Media:      s.paths.Content,
		Covers:     s.paths.Covers,
		Exists:     idx.Exists,
		VideoCount: videoCount,
		VideoGB:    round2(gb / (1 << 30)),
		CoverCount: countCovers(s.paths.Covers),
		DiskGB:     round2(gb / (1 << 30)),
		Ledger:     s.paths.LedgerCSV,
		Database:   s.paths.Database,
	}
}

// SettingsPayload 是前端 settings 的形状。
func (s *Server) settingsPayload() map[string]any {
	s.mu.RLock()
	cfg := s.settings
	s.mu.RUnlock()
	return map[string]any{
		"proxy":      cfg.Proxy,
		"concurrent": cfg.Concurrent,
		"ffmpegPath": cfg.FFmpegPath,
		"qiseCookie": cfg.QiseCookie,
		"retryCount": cfg.RetryCount,
		"retryDelay": cfg.RetryDelay,
	}
}

// ---------- 路由 ----------

// Handler 返回完整的 HTTP 处理器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 只读
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/update", s.handleUpdate)
	mux.HandleFunc("POST /api/update/apply", s.handleUpdateApply)
	mux.HandleFunc("GET /api/desktop", s.handleDesktop)
	mux.HandleFunc("POST /api/desktop/window", s.handleDesktopWindow)
	mux.HandleFunc("POST /api/desktop/pick-folder", s.handlePickFolder)
	mux.HandleFunc("POST /api/desktop/login", s.handleDesktopLogin)
	mux.HandleFunc("POST /api/desktop/login/capture", s.handleDesktopLoginCapture)
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("POST /api/projects/open", s.handleProjectsOpen)
	mux.HandleFunc("GET /api/project", s.handleProject)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("GET /api/cover", s.handleCover)
	mux.HandleFunc("GET /api/logo", s.handleLogo)
	mux.HandleFunc("GET /api/deleted", s.handleDeleted)
	mux.HandleFunc("GET /api/collections", s.handleCollections)
	mux.HandleFunc("GET /api/collections/items", s.handleCollectionItems)
	mux.HandleFunc("GET /api/snapshots", s.handleSnapshots)
	mux.HandleFunc("GET /api/snapshot/jobs", s.handleSnapshotJobs)
	mux.HandleFunc("GET /api/snapshot/image", s.handleSnapshotImage)
	mux.HandleFunc("POST /api/snapshot", s.handleSnapshotCreate)
	mux.HandleFunc("POST /api/snapshot/delete", s.handleSnapshotDelete)
	mux.HandleFunc("POST /api/snapshot/cover", s.handleSnapshotCover)
	mux.HandleFunc("POST /api/play", s.handlePlay)
	mux.HandleFunc("POST /api/reveal", s.handleReveal)
	mux.HandleFunc("POST /api/covers/repair", s.handleCoversRepair)
	mux.HandleFunc("POST /api/import/local", s.handleImportLocal)
	mux.HandleFunc("POST /api/project/import", s.handleProjectImport)
	mux.HandleFunc("POST /api/project/open", s.handleProjectOpen)
	mux.HandleFunc("GET /api/download", s.handleDownloadInfo)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/diagnostics/scrape", s.handleDiagnostics)
	mux.HandleFunc("GET /api/diagnostics/scrape/file", s.handleDiagnosticsFile)

	// 写操作
	mux.HandleFunc("POST /api/fav", s.handleFav)
	mux.HandleFunc("POST /api/rate", s.handleRate)
	mux.HandleFunc("POST /api/mark", s.handleMark)
	mux.HandleFunc("POST /api/watched", s.handleWatched)
	mux.HandleFunc("POST /api/restore", s.handleRestore)
	mux.HandleFunc("POST /api/delete", s.handleDelete)
	mux.HandleFunc("POST /api/scan", s.handleScan)
	mux.HandleFunc("POST /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/collections", s.handleCollectionsWrite)
	mux.HandleFunc("POST /api/log", s.handleClientLog)

	// 源管理与抓取
	mux.HandleFunc("GET /api/sources", s.handleSources)
	mux.HandleFunc("GET /api/sources/package", s.handleSourcePackage)
	mux.HandleFunc("POST /api/sources/import", s.handleSourceImport)
	mux.HandleFunc("POST /api/sources/enable", s.handleSourceEnable)
	mux.HandleFunc("POST /api/sources/remove", s.handleSourceRemove)
	mux.HandleFunc("GET /api/sources/export", s.handleSourceExport)
	mux.HandleFunc("POST /api/sources/test", s.handleSourceTest)
	mux.HandleFunc("POST /api/sources/test-all", s.handleSourceTestAll)
	mux.HandleFunc("GET /api/sources/auth", s.handleSourceAuth)
	mux.HandleFunc("POST /api/sources/auth", s.handleSourceAuthSave)
	mux.HandleFunc("POST /api/sources/auth/clear", s.handleSourceAuthClear)
	mux.HandleFunc("POST /api/sources/auth/test", s.handleSourceAuthTest)
	mux.HandleFunc("POST /api/scrape", s.handleScrape)
	mux.HandleFunc("POST /api/job/cancel", s.handleJobCancel)

	// 下载队列
	mux.HandleFunc("POST /api/download", s.handleDownloadAdd)
	mux.HandleFunc("POST /api/download/pause", s.handleDownloadPause)
	mux.HandleFunc("POST /api/download/cancel", s.handleDownloadCancel)
	mux.HandleFunc("POST /api/download/clear-done", s.handleDownloadClearDone)
	mux.HandleFunc("POST /api/download/reset", s.handleDownloadReset)
	mux.HandleFunc("POST /api/download/repair", s.handleDownloadRepair)

	// 尚未迁移到 Go 的能力：明确报错，不伪装成功
	mux.HandleFunc("POST /api/refresh-url", s.handleRefreshURL)
	mux.HandleFunc("POST /api/import", s.handleNotYet("链接抓取请使用 /api/scrape 的 url 参数"))
	mux.HandleFunc("POST /api/content/open", s.handleRevealContent)

	// 静态资源与媒体
	mux.HandleFunc("GET /assets/", s.handleAssets)
	mux.HandleFunc("GET /media/", s.handleMedia)
	mux.HandleFunc("GET /", s.handleSPA)

	return s.withMetrics(mux)
}

func (s *Server) withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				s.logbuf.Add("error", fmt.Sprintf("%s %s panic: %v", r.Method, r.URL.Path, rec))
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "internal"})
			}
			s.metrics.record(r.Method+" "+r.URL.Path, time.Since(start).Milliseconds())
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// ---------- 通用响应 ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func bad(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg})
}

func (s *Server) handleNotYet(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bad(w, msg)
	}
}

// Bump 在写操作后推进版本号。
func (s *Server) Bump(rowsChanged bool) {
	s.mu.Lock()
	s.rev++
	if rowsChanged {
		s.rowsRev++
	}
	s.mu.Unlock()
}
