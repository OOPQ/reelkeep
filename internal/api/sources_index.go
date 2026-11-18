package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
	"github.com/OOPQ/reelkeep/internal/rule"
)

const (
	maxIndexBytes = 2 << 20
	maxPkgBytes   = 5 << 20
)

// indexItem 是「可用源」列表项：索引内容 + 本机安装状态。
type indexItem struct {
	rule.IndexEntry
	InstalledVersion string `json:"installedVersion,omitempty"`
	Updatable        bool   `json:"updatable"`
	Compatible       bool   `json:"compatible"`
}

func (s *Server) indexURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u := strings.TrimSpace(s.settings.SourceIndexURL); u != "" {
		return u
	}
	return config.DefaultSourceIndexURL
}

func (s *Server) appVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// fetchIndex 拉取并解析索引。
func (s *Server) fetchIndex(ctx context.Context, rawURL string) (*rule.SourceIndex, error) {
	b, err := fetchLimited(ctx, rawURL, maxIndexBytes)
	if err != nil {
		return nil, err
	}
	return rule.ParseIndex(b)
}

// handleSourceIndex 返回索引里可安装/可更新的源，并附带本机版本。
func (s *Server) handleSourceIndex(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	urlStr := s.indexURL()
	if urlStr == "" {
		bad(w, "还没有配置源索引地址")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	idx, err := s.fetchIndex(ctx, urlStr)
	if err != nil {
		bad(w, "读取索引失败: "+err.Error())
		return
	}
	installed := map[string]string{}
	for _, it := range s.srcMgr.List() {
		installed[strings.ToLower(it.ID)] = it.Version
	}
	ver := s.appVersion()
	items := make([]indexItem, 0, len(idx.Sources))
	for _, e := range idx.Sources {
		cur := installed[e.ID]
		items = append(items, indexItem{
			IndexEntry:       e,
			InstalledVersion: cur,
			Updatable:        cur != "" && rule.VersionLess(cur, e.Version),
			Compatible:       e.MinApp == "" || !rule.VersionLess(ver, e.MinApp),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "indexUrl": urlStr, "items": items})
}

// handleSourceInstall 按索引条目下载源包（校验 sha256）并安装/覆盖。
func (s *Server) handleSourceInstall(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	id := strings.ToLower(strings.TrimSpace(body.ID))
	if id == "" {
		bad(w, "缺少 id")
		return
	}
	urlStr := s.indexURL()
	if urlStr == "" {
		bad(w, "还没有配置源索引地址")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	idx, err := s.fetchIndex(ctx, urlStr)
	if err != nil {
		bad(w, "读取索引失败: "+err.Error())
		return
	}
	var entry *rule.IndexEntry
	for i := range idx.Sources {
		if idx.Sources[i].ID == id {
			entry = &idx.Sources[i]
			break
		}
	}
	if entry == nil {
		bad(w, "索引里没有这个源: "+id)
		return
	}
	if entry.MinApp != "" && rule.VersionLess(s.appVersion(), entry.MinApp) {
		bad(w, fmt.Sprintf("该源需要 %s 及以上版本", entry.MinApp))
		return
	}
	pkgURL, err := idx.FileURL(urlStr, *entry)
	if err != nil {
		bad(w, err.Error())
		return
	}
	b, err := fetchLimited(ctx, pkgURL, maxPkgBytes)
	if err != nil {
		bad(w, "下载源包失败: "+err.Error())
		return
	}
	if entry.SHA256 != "" {
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != entry.SHA256 {
			bad(w, "源包校验失败（sha256 不匹配）")
			return
		}
	}
	p, err := s.srcMgr.Import(b, true)
	if err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("source", fmt.Sprintf("从索引安装源 %s %s", p.ID, p.Version))
	s.Bump(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": p.ID, "name": p.Name, "version": p.Version,
	})
}

// handleSourceStats 返回各源进程内的请求计数（2xx/拦截/挑战/失败）。
func (s *Server) handleSourceStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": rule.StatsSnapshot()})
}

// fetchLimited 只允许 http(s)，限制响应大小。
func fetchLimited(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("地址必须是 http(s) 绝对地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "reelkeep-source-index/1")
	cli := http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("响应超过 %d KB", limit/1024)
	}
	return b, nil
}
