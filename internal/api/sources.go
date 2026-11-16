package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/rule"
	"github.com/OOPQ/reelkeep/internal/source"
)

// SourceManager 是外部源包管理器（由 main 注入）。
type SourceManager interface {
	List() []rule.ItemView
	Import(data []byte, overwrite bool) (*rule.Package, error)
	SetEnabled(id string, on bool) error
	Remove(id string) error
	Export(id string) ([]byte, error)
	RecordResult(id string, err error)
	Package(id string) (*rule.Package, bool)
	NewFetcher(p *rule.Package) (rule.Fetcher, error)
	SetAuth(id, cookie string) error
	ClearAuth(id string) error
	TestAuth(ctx context.Context, id string) (bool, string, error)
	Proxy() string
	SetProxy(string)
}

// handleSourcePackage 返回安装包里保存的完整 JSON（不是列表摘要）。
func (s *Server) handleSourcePackage(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		bad(w, "缺少 id")
		return
	}
	pkg, ok := s.srcMgr.Package(id)
	if !ok {
		bad(w, "源不存在: "+id)
		return
	}
	b, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "package": json.RawMessage(b)})
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	items := s.srcMgr.List()
	if items == nil {
		items = []rule.ItemView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (s *Server) handleSourceImport(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	var body struct {
		Data      string `json:"data"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	p, err := s.srcMgr.Import([]byte(body.Data), body.Overwrite)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "error": err.Error(), "exists": isExists(err),
		})
		return
	}
	s.logbuf.Add("source", "导入源 "+p.ID+" v"+p.Version)
	s.Bump(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"source": map[string]any{
			"id": p.ID, "name": p.Name, "version": p.Version,
			"capabilities": p.Capabilities, "ranks": p.List.Ranks,
		},
	})
}

func isExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), rule.ErrExists.Error())
}

func (s *Server) handleSourceEnable(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	var body struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if err := s.srcMgr.SetEnabled(body.ID, body.Enabled); err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("source", map[bool]string{true: "启用源 ", false: "禁用源 "}[body.Enabled]+body.ID)
	s.Bump(false)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSourceRemove(w http.ResponseWriter, r *http.Request) {
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
	if err := s.srcMgr.Remove(body.ID); err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("source", "删除源 "+body.ID)
	s.Bump(false)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSourceExport(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	id := r.URL.Query().Get("id")
	data, err := s.srcMgr.Export(id)
	if err != nil {
		bad(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// handleSourceTest 跑一次小规模抓取，只回预览、不写库。
func (s *Server) handleSourceTest(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	var body struct {
		ID      string `json:"id"`
		Rank    string `json:"rank"`
		Limit   int    `json:"limit"`
		Resolve bool   `json:"resolve"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	pkg, ok := s.srcMgr.Package(body.ID)
	if !ok {
		bad(w, "没有这个源："+body.ID)
		return
	}
	limit := body.Limit
	if limit <= 0 {
		limit = 3
	}
	if limit > 10 {
		limit = 10
	}
	fetcher, err := s.srcMgr.NewFetcher(pkg)
	if err != nil {
		bad(w, err.Error())
		return
	}
	rs := rule.New(pkg, fetcher)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var rows []model.Row
	err = rs.Scrape(ctx, sourceRequest(pkg, body.Rank, limit), func(r model.Row) error {
		rows = append(rows, r)
		return nil
	})
	if err != nil {
		s.srcMgr.RecordResult(pkg.ID, err)
		bad(w, "测试失败："+err.Error())
		return
	}
	s.srcMgr.RecordResult(pkg.ID, nil)

	type preview struct {
		Token    string `json:"token"`
		Title    string `json:"title"`
		Playable string `json:"playable"`
		Cover    string `json:"cover"`
		Duration string `json:"duration"`
		Res      string `json:"res"`
	}
	out := make([]preview, 0, len(rows))
	for _, r := range rows {
		out = append(out, preview{
			Token: r.Token, Title: r.Title, Playable: r.URL,
			Cover: r.CoverURL, Duration: r.Duration, Res: r.Res,
		})
	}
	resp := map[string]any{"ok": true, "count": len(out), "rows": out}
	if body.Resolve {
		resolved := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			spec, serr := rs.Resolve(ctx, r)
			item := map[string]any{"token": r.Token}
			if serr != nil {
				item["error"] = serr.Error()
			} else {
				item["url"] = spec.URL
				item["mode"] = spec.Mode
				item["headers"] = spec.Headers
				item["refreshOn410"] = spec.RefreshOn410
			}
			resolved = append(resolved, item)
		}
		resp["resolved"] = resolved
	}
	writeJSON(w, http.StatusOK, resp)
}

// sourceRequest 组一个最小的抓取请求。
func sourceRequest(pkg *rule.Package, rank string, limit int) source.ScrapeRequest {
	return source.ScrapeRequest{
		Rank:     rank,
		Pages:    []int{1},
		Limit:    limit,
		PageSize: maxInt(limit, pkg.List.PageSize),
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- 登录凭据 ----------

func (s *Server) authItem(id string) (rule.ItemView, bool) {
	if s.srcMgr == nil {
		return rule.ItemView{}, false
	}
	for _, it := range s.srcMgr.List() {
		if strings.EqualFold(it.ID, id) {
			return it, true
		}
	}
	return rule.ItemView{}, false
}

// handleSourceAuth 返回某个源的登录配置（不回传 Cookie 本身）。
func (s *Server) handleSourceAuth(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	it, ok := s.authItem(r.URL.Query().Get("id"))
	if !ok {
		bad(w, "没有这个源")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": it.ID, "authType": it.AuthType,
		"configured": it.AuthConfigured, "updatedAt": it.AuthUpdatedAt,
		"loginUrl": it.LoginURL, "checkUrl": it.CheckURL,
	})
}

func (s *Server) handleSourceAuthSave(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	var body struct {
		ID     string `json:"id"`
		Cookie string `json:"cookie"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if strings.TrimSpace(body.Cookie) == "" {
		bad(w, "Cookie 不能为空")
		return
	}
	if err := s.srcMgr.SetAuth(body.ID, body.Cookie); err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("source", "更新登录凭据 "+body.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	loggedIn, message, testErr := s.srcMgr.TestAuth(ctx, body.ID)
	resp := map[string]any{"ok": true, "loggedIn": loggedIn, "message": message}
	if testErr != nil {
		resp["testError"] = testErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSourceAuthClear(w http.ResponseWriter, r *http.Request) {
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
	if err := s.srcMgr.ClearAuth(body.ID); err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("source", "清除登录凭据 "+body.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSourceAuthTest(w http.ResponseWriter, r *http.Request) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	loggedIn, message, err := s.srcMgr.TestAuth(ctx, body.ID)
	if err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "loggedIn": loggedIn, "message": message})
}

// handleSourceTestAll 并发测试一个源的所有榜单，只回每条榜单抓到多少/报错。
func (s *Server) handleSourceTestAll(w http.ResponseWriter, r *http.Request) {
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	var body struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	pkg, ok := s.srcMgr.Package(body.ID)
	if !ok {
		bad(w, "没有这个源："+body.ID)
		return
	}
	if len(pkg.List.Ranks) == 0 {
		bad(w, "这个源没有榜单")
		return
	}
	limit := body.Limit
	if limit <= 0 {
		limit = 1
	}
	if limit > 3 {
		limit = 3
	}
	fetcher, err := s.srcMgr.NewFetcher(pkg)
	if err != nil {
		bad(w, err.Error())
		return
	}
	rs := rule.New(pkg, fetcher)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	type rankResult struct {
		Rank      string `json:"rank"`
		Label     string `json:"label"`
		OK        bool   `json:"ok"`
		Count     int    `json:"count"`
		ListURL   string `json:"listURL"`
		ListOK    bool   `json:"listOK"`
		DetailOK  bool   `json:"detailOK"`
		CoverOK   bool   `json:"coverOK"`
		ResolveOK bool   `json:"resolveOK"`
		Mode      string `json:"mode,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	results := make([]rankResult, len(pkg.List.Ranks))
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for i, rank := range pkg.List.Ranks {
		wg.Add(1)
		go func(i int, rank source.Rank) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := rankResult{Rank: rank.Key, Label: rank.Label, ListURL: pkg.ListURL(rank.Key, 1)}
			var rows []model.Row
			serr := rs.Scrape(ctx, sourceRequest(pkg, rank.Key, limit), func(row model.Row) error {
				rows = append(rows, row)
				return nil
			})
			res.Count = len(rows)
			res.ListOK = serr == nil
			if serr != nil {
				res.Error = serr.Error()
				results[i] = res
				return
			}
			if len(rows) == 0 {
				res.Error = "列表页没有可入库条目（可能选择器/详情地址失效）"
				results[i] = res
				return
			}
			for _, row := range rows {
				if row.URL != "" && row.Title != "" {
					res.DetailOK = true
				}
				if row.CoverURL != "" {
					res.CoverOK = true
				} else if cv, ok := any(rs).(source.Coverer); ok {
					if u, err := cv.Cover(ctx, row); err == nil && u != "" {
						res.CoverOK = true
					}
				}
				spec, rerr := rs.Resolve(ctx, row)
				if rerr != nil {
					if res.Error == "" {
						res.Error = "下载地址解析失败: " + rerr.Error()
					}
					continue
				}
				if spec.URL != "" {
					res.ResolveOK = true
					res.Mode = spec.Mode
				}
			}
			res.OK = res.ListOK && res.DetailOK && res.ResolveOK
			if !res.DetailOK && res.Error == "" {
				res.Error = "详情页没有解析到标题/播放地址"
			}
			if !res.ResolveOK && res.Error == "" {
				res.Error = "没有得到可用的下载地址"
			}
			results[i] = res
		}(i, rank)
	}
	wg.Wait()
	passed := 0
	for _, res := range results {
		if res.OK {
			passed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": pkg.ID, "passed": passed, "total": len(results), "results": results,
	})
}
