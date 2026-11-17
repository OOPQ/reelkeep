// Package download 是内置下载器：队列、HTTP 断点续传、HLS/ffmpeg、暂停/取消。
package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/jsonx"
	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/netproxy"
	"github.com/OOPQ/reelkeep/internal/proc"
	"github.com/OOPQ/reelkeep/internal/source"
	"github.com/OOPQ/reelkeep/internal/store"
)

// Settings 是下载器需要的可变配置。
type Settings struct {
	Concurrent     int
	FFmpeg         string
	Proxy          string
	RetryCount     int
	RetryDelay     time.Duration
	TorrentLimitKB int
}

// Manager 管理下载队列。
type Manager struct {
	paths    config.Paths
	st       *store.Store
	reg      *source.Registry
	cookie   func(string) string
	onChange func()
	onRow    func(string)
	logf     func(kind, msg string)
	client   *http.Client

	mu        sync.Mutex
	sessionMu sync.Mutex
	sessions  map[string]*torrentSession
	tasks     map[string]*Task
	active    map[string]int
	running   int
	paused    bool
	settings  Settings
	ctx       context.Context
	cancel    context.CancelFunc
	wake      chan struct{}
}

// Task 是内存里的任务和运行时状态。
type Task struct {
	M         model.DownloadTask
	cancel    context.CancelFunc
	running   bool
	speed     int64
	lastBytes int64
	lastAt    time.Time
}

// NewManager 创建下载器。
func NewManager(paths config.Paths, st *store.Store, reg *source.Registry, cookie func(string) string,
	onChange func(), onRow func(string), logf func(kind, msg string)) *Manager {
	m := &Manager{
		paths: paths, st: st, reg: reg, cookie: cookie,
		onChange: onChange, onRow: onRow, logf: logf,
		tasks: map[string]*Task{}, active: map[string]int{},
		wake: make(chan struct{}, 1),
	}
	m.client = &http.Client{
		Transport: &http.Transport{
			Proxy: func(req *http.Request) (*url.URL, error) {
				return netproxy.RequestProxy(strings.TrimSpace(m.getSettings().Proxy), req)
			},
			DialContext:           dialAny,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConnsPerHost:   4,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("重定向次数过多")
			}
			// CDN 经常 302 到另一个域名，并校验 Referer；Go 默认会丢掉它。
			if len(via) > 0 {
				for _, k := range []string{"Referer", "User-Agent"} {
					if v := via[0].Header.Get(k); v != "" && req.Header.Get(k) == "" {
						req.Header.Set(k, v)
					}
				}
			}
			return nil
		},
	}
	return m
}

// SetOnRow 在条目完成后通知上层刷新内存台账。
func (m *Manager) SetOnRow(fn func(string)) { m.onRow = fn }

// SetSettings 更新并发/ffmpeg/代理。
func (m *Manager) SetSettings(s Settings) {
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	m.wakeup()
}

func (m *Manager) getSettings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// Start 加载队列并开始调度。
func (m *Manager) Start(ctx context.Context) error {
	m.migrateLegacyQueue()
	tasks, err := m.st.LoadTasks()
	if err != nil {
		return err
	}
	m.mu.Lock()
	for i := range tasks {
		t := tasks[i]
		if !safeName(t.Token) || !safeName(sourceKey(t.Source)) {
			t.State = "failed"
			t.Error = "token/来源名含非法路径字符"
			_ = m.st.SaveTask(t)
			m.tasks[t.Token] = &Task{M: t}
			continue
		}
		if t.State == "downloading" {
			t.State = "queued" // 进程重启，进行中的任务回到队列
			_ = m.st.SaveTask(t)
		}
		m.tasks[t.Token] = &Task{M: t}
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	go m.loop()
	return nil
}

// Stop 停止调度（进行中的任务会被取消）。
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) wakeup() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) loop() {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.dispatch()
		case <-m.wake:
			m.dispatch()
		}
	}
}

func (m *Manager) dispatch() {
	s := m.getSettings()
	conc := s.Concurrent
	if conc <= 0 {
		conc = 8
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paused || m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	var queued []*Task
	for _, t := range m.tasks {
		if t.M.State == "queued" {
			queued = append(queued, t)
		}
	}
	sort.Slice(queued, func(i, j int) bool {
		if queued[i].M.AddedAt != queued[j].M.AddedAt {
			return queued[i].M.AddedAt < queued[j].M.AddedAt
		}
		return queued[i].M.Token < queued[j].M.Token
	})
	for _, t := range queued {
		if m.running >= conc {
			break
		}
		if t.running {
			continue // 暂停后立刻继续时，旧 goroutine 可能还在退出
		}
		src := sourceKey(t.M.Source)
		if m.active[src] >= perSourceLimit() {
			continue
		}
		m.running++
		m.active[src]++
		t.running = true
		t.M.State = "downloading"
		t.M.StartedAt = now()
		_ = m.st.SaveTask(t.M)
		ctx, cancel := context.WithCancel(m.ctx)
		t.cancel = cancel
		go m.run(ctx, t)
	}
}

func perSourceLimit() int { return 2 }

// run 执行一条任务。
func (m *Manager) run(ctx context.Context, t *Task) {
	defer func() {
		src := sourceKey(t.M.Source)
		m.mu.Lock()
		t.running = false
		m.running--
		m.active[src]--
		if m.active[src] <= 0 {
			delete(m.active, src)
		}
		m.mu.Unlock()
		m.wakeup()
	}()

	var err error
	switch {
	case strings.EqualFold(t.M.Mode, "hls"):
		err = m.runHLS(ctx, t)
	case strings.EqualFold(t.M.Mode, "torrent"):
		err = m.runTorrent(ctx, t)
	default:
		err = m.runHTTP(ctx, t)
	}
	m.mu.Lock()
	state := t.M.State
	m.mu.Unlock()
	if state == "cancelled" {
		// 文件已关闭，这里再删 .part，避免 Windows 上删不掉正在写的文件
		_, part := m.pathsFor(t)
		_ = os.Remove(part)
	}
	// 暂停/取消/立刻继续后，旧 goroutine 不再写终态
	if state != "downloading" {
		return
	}
	if err != nil {
		m.finish(t, "failed", err)
		return
	}
	m.finish(t, "done", nil)
}

func (m *Manager) finish(t *Task, state string, err error) {
	m.mu.Lock()
	t.M.State = state
	t.M.FinishedAt = now()
	t.speed = 0
	if err != nil {
		t.M.Error = err.Error()
	} else {
		t.M.Error = ""
	}
	_ = m.st.SaveTask(t.M)
	m.mu.Unlock()

	if strings.HasPrefix(t.M.Token, "torrent@") {
		m.st.UpdateJobState(strings.TrimPrefix(t.M.Token, "torrent@"), state)
	} else if state == "done" {
		m.updateLedger(t)
	}
	if m.onChange != nil {
		m.onChange()
	}
}

// updateLedger 把完成文件写回台账。
func (m *Manager) updateLedger(t *Task) {
	final := m.finalPath(t)
	st, err := os.Stat(final)
	if err != nil {
		return
	}
	row, ok, err := m.st.GetRow(t.M.Token)
	if err != nil || !ok {
		return
	}
	row.FileName = filepath.Base(final)
	row.FilePath = final
	row.FileSize = fmt.Sprintf("%d", st.Size())
	row.FileDate = st.ModTime().Format("2006-01-02 15:04:05")
	row.Status = "done"
	if t.M.Name != "" {
		row.Title = strings.TrimSuffix(filepath.Base(final), filepath.Ext(final))
	}
	row.Note = "auto:download"
	if err := m.st.UpsertRows([]model.Row{row}); err != nil {
		m.logf("download", "回填台账失败: "+err.Error())
		return
	}
	m.autoCover(t.M.Token, final)
	if m.onRow != nil {
		m.onRow(t.M.Token)
	}
}

// autoCover 用视频第一帧生成本地封面；已有缓存封面则保留。
func (m *Manager) autoCover(token, video string) {
	if token == "" {
		return
	}
	cover := filepath.Join(m.paths.Covers, token+".jpg")
	if st, err := os.Stat(cover); err == nil && st.Size() > 1024 {
		return
	}
	ff := proc.FindFFmpeg(strings.TrimSpace(m.getSettings().FFmpeg))
	if ff == "" {
		m.logf("download", "未找到 ffmpeg，跳过自动封面")
		return
	}
	if err := os.MkdirAll(m.paths.Covers, 0o755); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ff, "-y", "-ss", "0", "-i", video, "-frames:v", "1", "-vf", "scale=640:-2", "-q:v", "3", cover)
	proc.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		m.logf("download", "自动封面失败: "+strings.TrimSpace(string(out)))
	}
}

// Enqueue 入队一条任务。
func (m *Manager) Enqueue(t model.DownloadTask) (bool, error) {
	if t.Token == "" || t.URL == "" {
		return false, errors.New("任务缺少 token 或 url")
	}
	if !safeName(t.Token) {
		return false, errors.New("token 含非法路径字符")
	}
	if !safeName(sourceKey(t.Source)) {
		return false, errors.New("来源名含非法路径字符")
	}
	if t.Mode == "" {
		t.Mode = "http"
	}
	if t.State == "" {
		t.State = "queued"
	}
	if t.AddedAt == "" {
		t.AddedAt = now()
	}
	m.mu.Lock()
	if old, ok := m.tasks[t.Token]; ok {
		switch old.M.State {
		case "queued", "downloading", "paused":
			m.mu.Unlock()
			return false, nil // 已经在队列里
		}
	}
	m.tasks[t.Token] = &Task{M: t}
	err := m.st.SaveTask(t)
	m.mu.Unlock()
	if err != nil {
		return false, err
	}
	m.wakeup()
	return true, nil
}

func jobIDForToken(token string) string {
	if strings.HasPrefix(token, "torrent@") {
		return strings.TrimPrefix(token, "torrent@")
	}
	return ""
}

func (m *Manager) setJobFilesState(id, state string) {
	if id == "" {
		return
	}
	files, err := m.st.JobFiles(id)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.State == "done" {
			continue
		}
		m.st.UpdateJobFile(id, f.Index, state, f.Bytes, f.Error, f.MediaToken)
	}
}

// Pause 暂停/继续：tokens 为空表示全部。
func (m *Manager) Pause(on bool, tokens []string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	if len(tokens) == 0 {
		m.paused = on
		for _, t := range m.tasks {
			if on && t.M.State == "downloading" {
				t.M.State = "paused"
				if t.cancel != nil {
					t.cancel()
				}
				_ = m.st.SaveTask(t.M)
				if id := jobIDForToken(t.M.Token); id != "" {
					m.st.UpdateJobState(id, "paused")
				}
				n++
			}
			if !on && t.M.State == "paused" {
				t.M.State = "queued"
				_ = m.st.SaveTask(t.M)
				if id := jobIDForToken(t.M.Token); id != "" {
					m.st.UpdateJobState(id, "queued")
					m.setJobFilesState(id, "queued")
				}
				n++
			}
		}
		if !on {
			m.wakeup()
		}
		return n
	}
	set := tokenSet(tokens)
	if !on && m.paused {
		// 全部暂停后单条继续：解除全局闸门，只把这一条放回队列
		m.paused = false
	}
	for _, t := range m.tasks {
		if !set[t.M.Token] {
			continue
		}
		if on {
			if t.M.State == "downloading" || t.M.State == "queued" {
				t.M.State = "paused"
				if t.cancel != nil {
					t.cancel()
				}
				_ = m.st.SaveTask(t.M)
				if id := jobIDForToken(t.M.Token); id != "" {
					m.st.UpdateJobState(id, "paused")
				}
				n++
			}
		} else if t.M.State == "paused" {
			t.M.State = "queued"
			_ = m.st.SaveTask(t.M)
			if id := jobIDForToken(t.M.Token); id != "" {
				m.st.UpdateJobState(id, "queued")
				m.setJobFilesState(id, "queued")
			}
			n++
		}
	}
	if !on {
		m.wakeup()
	}
	return n
}

// Cancel 取消：tokens 为空表示全部。取消会删掉 .part。
func (m *Manager) Cancel(tokens []string) int {
	m.mu.Lock()
	var targets []*Task
	if len(tokens) == 0 {
		for _, t := range m.tasks {
			if t.M.State == "downloading" || t.M.State == "queued" || t.M.State == "paused" {
				targets = append(targets, t)
			}
		}
	} else {
		set := tokenSet(tokens)
		for _, t := range m.tasks {
			if set[t.M.Token] {
				targets = append(targets, t)
			}
		}
	}
	for _, t := range targets {
		t.M.State = "cancelled"
		t.M.FinishedAt = now()
		if t.cancel != nil {
			t.cancel()
		}
		_ = m.st.SaveTask(t.M)
		if id := jobIDForToken(t.M.Token); id != "" {
			m.st.UpdateJobState(id, "cancelled")
			m.setJobFilesState(id, "cancelled")
		}
	}
	m.mu.Unlock()
	if m.onChange != nil {
		m.onChange()
	}
	return len(targets)
}

// ClearDone 清理已完成/已取消的队列记录。
func (m *Manager) ClearDone() int {
	n, _ := m.st.ClearDoneTasks()
	m.mu.Lock()
	for token, t := range m.tasks {
		if t.M.State == "done" || t.M.State == "cancelled" {
			delete(m.tasks, token)
		}
	}
	m.mu.Unlock()
	if m.onChange != nil {
		m.onChange()
	}
	return int(n)
}

// Reset 解除占用：取消所有进行中的任务并清除暂停标志。
func (m *Manager) Reset() int {
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()
	return m.Cancel(nil)
}

// Item 是给前端的队列项。
type Item struct {
	Token   string `json:"token"`
	Title   string `json:"title,omitempty"`
	State   string `json:"state"`
	Bytes   int64  `json:"bytes"`
	Total   int64  `json:"total"`
	Speed   int64  `json:"speed"`
	Error   string `json:"error"`
	History bool   `json:"history"`
	AddedAt string `json:"addedAt"`
	Source  string `json:"source,omitempty"`
}

// Info 是 /api/download 和 /api/state.download 的快照。
type Info struct {
	Running     bool                `json:"running"`
	Paused      bool                `json:"paused"`
	Phase       string              `json:"phase"`
	Total       int                 `json:"total"`
	Done        int                 `json:"done"`
	Failed      int                 `json:"failed"`
	Queued      int                 `json:"queued"`
	PausedCount int                 `json:"pausedCount"`
	Bytes       int64               `json:"bytes"`
	Speed       int64               `json:"speed"`
	PID         int                 `json:"pid"`
	Active      []string            `json:"active"`
	Errors      []map[string]string `json:"errors"`
	Items       []Item              `json:"items"`
}

// Info 生成队列快照。
func (m *Manager) Info() Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := Info{Paused: m.paused, Active: []string{}, Errors: []map[string]string{}, Items: []Item{}}
	for _, t := range m.tasks {
		state := t.M.State
		info.Total++
		switch state {
		case "done":
			info.Done++
		case "failed", "dead":
			info.Failed++
			if t.M.Error != "" && len(info.Errors) < 50 {
				info.Errors = append(info.Errors, map[string]string{"token": t.M.Token, "error": t.M.Error})
			}
		case "queued":
			info.Queued++
		case "paused":
			info.PausedCount++
		}
		if state == "downloading" {
			info.Active = append(info.Active, t.M.Token)
			info.Speed += t.speed
		}
		info.Bytes += t.M.Bytes
		info.Items = append(info.Items, Item{
			Token: t.M.Token, Title: t.M.Name, State: state, Bytes: t.M.Bytes, Total: t.M.Total,
			Speed: t.speed, Error: t.M.Error, History: t.M.History,
			AddedAt: t.M.AddedAt, Source: t.M.Source,
		})
	}
	info.Running = info.Active != nil && len(info.Active) > 0
	info.Phase = "idle"
	if info.Running {
		info.Phase = "downloading"
	}
	sort.Slice(info.Items, func(i, j int) bool { return info.Items[i].AddedAt > info.Items[j].AddedAt })
	return info
}

// ---------- 迁移 ----------

// migrateLegacyQueue 把旧版 .queue.json 导入（仅当任务表为空）。
func (m *Manager) migrateLegacyQueue() {
	if m.st.CountTasks() > 0 {
		return
	}
	p := filepath.Join(m.paths.DataDir, ".queue.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var items []struct {
		Token    string            `json:"token"`
		URL      string            `json:"url"`
		Mode     string            `json:"mode"`
		Headers  map[string]string `json:"headers"`
		Subdir   string            `json:"subdir"`
		Source   string            `json:"source"`
		SourceID string            `json:"source_id"`
		PageURL  string            `json:"page_url"`
		AddedAt  string            `json:"addedAt"`
	}
	if err := jsonx.Unmarshal(b, &items); err != nil {
		return
	}
	n := 0
	for _, it := range items {
		if it.Token == "" || it.URL == "" {
			continue
		}
		src := it.Source
		if src == "" {
			src = it.Subdir
		}
		if src == "" {
			src = "pektino"
		}
		h, _ := json.Marshal(it.Headers)
		t := model.DownloadTask{
			Token: it.Token, Source: src, SourceID: it.SourceID, URL: it.URL,
			Mode: it.Mode, Headers: string(h), State: "queued", AddedAt: it.AddedAt,
		}
		if t.Mode == "" {
			t.Mode = "http"
		}
		if t.AddedAt == "" {
			t.AddedAt = now()
		}
		if err := m.st.SaveTask(t); err == nil {
			n++
		}
	}
	_ = os.Rename(p, p+".migrated")
	if n > 0 {
		m.logf("download", fmt.Sprintf("已迁移旧下载队列 %d 条", n))
	}
}

// ---------- 小工具 ----------

func now() string { return time.Now().Format("2006-01-02 15:04:05") }

func sourceKey(s string) string {
	if s == "" {
		return "pektino"
	}
	return s
}

// safeName 阻止 token/source 变成路径穿越或 Windows 非法文件名。
func safeName(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 200 {
		return false
	}
	for _, r := range s {
		if r == '/' || r == '\\' || r == 0 || r < 0x20 {
			return false
		}
		switch r {
		case ':', '*', '?', '"', '<', '>', '|':
			return false
		}
	}
	return true
}

func tokenSet(tokens []string) map[string]bool {
	out := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		out[t] = true
	}
	return out
}

func (m *Manager) finalPath(t *Task) string {
	final, _ := m.pathsFor(t)
	return final
}

func (m *Manager) pathsFor(t *Task) (final, part string) {
	dir := filepath.Join(m.paths.Content, sourceKey(t.M.Source))
	name := strings.TrimSpace(t.M.Name)
	if name == "" {
		name = t.M.Token + ".mp4"
	}
	name = safeMediaName(name)
	final = filepath.Join(dir, name)
	return final, final + ".part"
}

func safeMediaName(name string) string {
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
		name = "video"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".mp4") {
		name += ".mp4"
	}
	if len([]rune(name)) > 120 {
		ext := filepath.Ext(name)
		base := []rune(strings.TrimSuffix(name, ext))
		if len(base) > 116 {
			base = base[:116]
		}
		name = string(base) + ext
	}
	return name
}

func parseHeaders(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) == nil {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// dialAny 优先 IPv4：本机 IPv6 经常没有路由，而 Go 的默认拨号在这种网络里
// 可能卡在 IPv6 上直到超时。
func dialAny(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, addr)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, addr)
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	try := func(v4 bool) (net.Conn, error) {
		var lastErr error
		for _, ip := range ips {
			if (ip.IP.To4() != nil) != v4 {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			conn, err := d.DialContext(cctx, network, net.JoinHostPort(ip.IP.String(), port))
			cancel()
			if err == nil {
				return conn, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, lastErr
	}
	if conn, err := try(true); err == nil {
		return conn, nil
	}
	if conn, err := try(false); err == nil {
		return conn, nil
	}
	return nil, fmt.Errorf("连接 %s 失败", addr)
}
