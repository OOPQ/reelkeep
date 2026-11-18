package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/theory/jsonpath"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// RuleSource 是 source.Source 的规则解释实现。
type RuleSource struct {
	pkg     *Package
	fetcher Fetcher
	http    *HTTPFetcher
}

// New 创建一个规则源。
func New(p *Package, f Fetcher) *RuleSource {
	rs := &RuleSource{pkg: p, fetcher: f}
	if hf, ok := f.(*HTTPFetcher); ok {
		rs.http = hf
	}
	return rs
}

// Package 暴露源包（CLI / 诊断用）。
func (s *RuleSource) Package() *Package { return s.pkg }

// ID 返回源标识。
func (s *RuleSource) ID() string { return s.pkg.ID }

// Capabilities 返回能力列表。
func (s *RuleSource) Capabilities() []string { return s.pkg.Capabilities }

// HealthCheck 抓一次首页确认可达。
func (s *RuleSource) HealthCheck(ctx context.Context) error {
	_, err := s.fetcher.Get(ctx, s.pkg.BaseURL, s.headers(nil))
	return err
}

func (s *RuleSource) headers(extra map[string]string) map[string]string {
	out := make(map[string]string, len(s.pkg.Defaults.Headers)+len(extra))
	for k, v := range s.pkg.Defaults.Headers {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// Scrape 抓取一个榜单的若干页。
func (s *RuleSource) Scrape(ctx context.Context, req source.ScrapeRequest, emit func(model.Row) error) error {
	if !s.pkg.Has("scrape") {
		return fmt.Errorf("源 %s 不支持抓取", s.pkg.ID)
	}
	rank := req.Rank
	if rank == "" {
		rank = s.pkg.firstRank()
	}
	if !contains(s.pkg.rankKeys(), rank) {
		return fmt.Errorf("源 %s 没有榜单 %q（可用：%s）", s.pkg.ID, rank, strings.Join(s.pkg.rankKeys(), ", "))
	}
	pages := req.Pages
	auto := len(pages) == 0
	if auto {
		pages = []int{1}
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = s.pkg.List.PageSize
	}
	limit := req.Limit

	emitted := 0
	seen := map[string]bool{}
	var firstErr error
	detailWorkers := s.pkg.Defaults.DetailWorkers
	if req.DetailWorkers > 0 {
		detailWorkers = req.DetailWorkers
	}
	if detailWorkers <= 0 {
		detailWorkers = 4
	}
	if detailWorkers > 16 {
		detailWorkers = 16
	}
	for idx := 0; idx < len(pages); idx++ {
		page := pages[idx]
		listURL := s.pkg.ListURL(rank, page)
		body, err := s.fetcher.Get(ctx, listURL, s.headers(nil))
		if err != nil {
			return fmt.Errorf("列表页 %s: %w", listURL, err)
		}
		rows, err := s.parseList(body, listURL, rank, page, pageSize)
		if err != nil {
			return err
		}
		// 先按 token 去重、决定哪些条目需要详情，再并发抓详情，最后按列表顺序 emit。
		type candidate struct {
			row  model.Row
			need bool
		}
		cands := make([]candidate, 0, len(rows))
		for _, r := range rows {
			if limit > 0 && emitted+len(cands) >= limit {
				break
			}
			key := strings.ToLower(r.Token)
			if seen[key] {
				continue // 列表页里同一个条目可能出现多次（导航/推荐位）
			}
			need := s.pkg.Detail != nil && (s.pkg.Detail.Always || r.URL == "")
			if need && req.KnownTokens != nil && req.KnownTokens[key] {
				need = false // 增量：最近抓过的条目不再打详情
			}
			if !need && r.URL == "" {
				continue
			}
			seen[key] = true
			cands = append(cands, candidate{row: r, need: need})
		}

		results := make([]model.Row, len(cands))
		errs := make([]error, len(cands))
		for i := range cands {
			results[i] = cands[i].row
		}
		workers := detailWorkers
		if workers > len(cands) {
			workers = len(cands)
		}
		if workers > 1 {
			sem := make(chan struct{}, workers)
			var wg sync.WaitGroup
			for i := range cands {
				if !cands[i].need {
					continue
				}
				wg.Add(1)
				sem <- struct{}{}
				go func(i int) {
					defer wg.Done()
					defer func() { <-sem }()
					d, derr := s.detailFor(ctx, cands[i].row.SourceID, cands[i].row.PageURL)
					if derr != nil {
						errs[i] = derr
						return
					}
					r := cands[i].row
					mergeRow(&r, d)
					results[i] = r
				}(i)
			}
			wg.Wait()
		} else {
			for i := range cands {
				if !cands[i].need {
					continue
				}
				d, derr := s.detailFor(ctx, cands[i].row.SourceID, cands[i].row.PageURL)
				if derr != nil {
					errs[i] = derr
					continue
				}
				r := cands[i].row
				mergeRow(&r, d)
				results[i] = r
			}
		}

		pageNew := 0
		for i := range cands {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errs[i] != nil {
				if firstErr == nil {
					firstErr = errs[i]
				}
				if results[i].URL == "" {
					continue // 没有播放地址的条目不入库（与旧版一致）
				}
			}
			r := results[i]
			if r.URL == "" {
				continue
			}
			if err := emit(r); err != nil {
				return err
			}
			emitted++
			pageNew++
			if req.Progress != nil {
				req.Progress("details", emitted, 0)
			}
		}
		if auto {
			if limit > 0 && emitted >= limit {
				return nil
			}
			// 站点忽略 page 参数或本页全是重复项时停止，避免无限翻页
			if pageNew == 0 || page >= 50 {
				if emitted == 0 && firstErr != nil {
					return firstErr
				}
				return nil
			}
			pages = append(pages, page+1)
		}
	}
	if emitted == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}

func (s *RuleSource) parseList(body []byte, listURL, rank string, page, pageSize int) ([]model.Row, error) {
	pageURL, _ := url.Parse(listURL)
	if s.pkg.List.JSONLD != "" {
		doc, err := parseHTML(body)
		if err != nil {
			return nil, err
		}
		root, err := jsonLDObject(doc, s.pkg.List.JSONLD)
		if err != nil {
			return nil, err
		}
		items, err := selectJSONPath(root, s.pkg.List.JSONPath)
		if err != nil {
			return nil, err
		}
		return s.rowsFromJSON(items, pageURL, rank, page, pageSize)
	}
	if s.pkg.List.JSONPath != "" {
		var root any
		if err := json.Unmarshal(body, &root); err != nil {
			return nil, fmt.Errorf("列表接口不是 JSON: %w", err)
		}
		items, err := selectJSONPath(root, s.pkg.List.JSONPath)
		if err != nil {
			return nil, err
		}
		return s.rowsFromJSON(items, pageURL, rank, page, pageSize)
	}

	doc, err := parseHTML(body)
	if err != nil {
		return nil, err
	}
	var out []model.Row
	nodes := doc.Find(s.pkg.List.Item)
	nodes.EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if len(out) >= pageSize {
			return false
		}
		fields, ferr := extractHTMLFields(doc, sel, s.pkg.List.Fields, pageURL)
		if ferr != nil {
			return true
		}
		r, rerr := s.rowFromFields(fields, pageURL, "")
		if rerr != nil {
			return true
		}
		r.Rank, r.Page = rank, strconv.Itoa(page)
		out = append(out, r)
		return true
	})
	return out, nil
}

func (s *RuleSource) rowsFromJSON(items []any, pageURL *url.URL, rank string, page, pageSize int) ([]model.Row, error) {
	var out []model.Row
	for _, item := range items {
		if len(out) >= pageSize {
			break
		}
		fields, err := extractJSONFields(item, s.pkg.List.Fields)
		if err != nil {
			return nil, err
		}
		r, err := s.rowFromFields(fields, pageURL, "")
		if err != nil {
			continue
		}
		r.Rank, r.Page = rank, strconv.Itoa(page)
		out = append(out, r)
	}
	return out, nil
}

func selectJSONPath(root any, expr string) ([]any, error) {
	p, err := jsonpath.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("列表 jsonPath: %w", err)
	}
	nodes := p.Select(root)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("列表 jsonPath 没有匹配到任何项")
	}
	return nodes, nil
}

func (s *RuleSource) rowFromFields(fields map[string]string, pageURL *url.URL, nativeID string) (model.Row, error) {
	id := strings.TrimSpace(nativeID)
	if id == "" {
		id = strings.TrimSpace(firstOf(fields, "id", "sourceId"))
	}
	if id == "" {
		return model.Row{}, fmt.Errorf("缺少 id")
	}
	if !validNativeID(id) {
		return model.Row{}, fmt.Errorf("id 含非法字符: %q", id)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	r := model.Row{
		Token:     s.pkg.ID + "@" + id,
		Status:    "new",
		Source:    s.pkg.ID,
		SourceID:  id,
		Site:      hostOf(s.pkg.BaseURL),
		Title:     firstOf(fields, "title", "name"),
		CoverURL:  firstOf(fields, "cover", "coverUrl"),
		URL:       firstOf(fields, "playable", "url", "playUrl"),
		PageURL:   firstOf(fields, "pageUrl", "page_url", "page"),
		Res:       fields["res"],
		Views:     fields["views"],
		Likes:     fields["likes"],
		Uploaded:  firstOf(fields, "uploaded", "uploadDate"),
		FileName:  "",
		FileSize:  "",
		FirstSeen: now,
		LastSeen:  now,
	}
	if r.PageURL == "" && pageURL != nil {
		r.PageURL = pageURL.String()
	}
	if s.pkg.Download != nil {
		r.DownloadKind = s.pkg.Download.Kind
	} else {
		r.DownloadKind = "http"
	}
	if d := fields["duration"]; d != "" {
		secs, norm := toSeconds(d)
		r.Seconds = strconv.Itoa(secs)
		r.Duration = norm
		if r.Duration == "" {
			r.Duration = d
		}
	}
	if raw := extraFields(fields); raw != "" {
		r.Extra = raw
	}
	return r, nil
}

// Detail 抓一条详情。
func (s *RuleSource) Detail(ctx context.Context, id string) (model.Row, error) {
	if s.pkg.Detail == nil {
		return model.Row{}, fmt.Errorf("源 %s 没有 detail 段", s.pkg.ID)
	}
	return s.detailFor(ctx, id, "")
}

func (s *RuleSource) detailFor(ctx context.Context, id, pageURL string) (model.Row, error) {
	if !validNativeID(id) {
		return model.Row{}, fmt.Errorf("id 含非法字符: %q", id)
	}
	if strings.Contains(s.pkg.Detail.URL, "{pageUrl}") && pageURL == "" {
		return model.Row{}, fmt.Errorf("源 %s 的详情页需要列表页地址（{pageUrl}），请从抓取流程调用", s.pkg.ID)
	}
	detailURL := s.pkg.DetailURLFor(id, pageURL)
	if detailURL == "" {
		return model.Row{}, fmt.Errorf("源 %s 没有详情页模板", s.pkg.ID)
	}
	body, err := s.fetcher.Get(ctx, detailURL, s.headers(nil))
	if err != nil {
		return model.Row{}, fmt.Errorf("详情页 %s: %w", detailURL, err)
	}
	doc, err := parseHTML(body)
	if err != nil {
		return model.Row{}, err
	}
	pageRef, _ := url.Parse(detailURL)
	fields, err := extractDetailFields(doc, s.pkg.Detail.Fields, pageRef)
	if err != nil {
		return model.Row{}, err
	}
	if fields["pageUrl"] == "" && fields["page_url"] == "" {
		fields["pageUrl"] = detailURL
	}
	return s.rowFromFields(fields, pageRef, id)
}

// Refresh 重新解析一条记录的播放地址（签名过期时由下载器调用）。
func (s *RuleSource) Refresh(ctx context.Context, r model.Row) (model.Row, error) {
	if s.pkg.Detail == nil {
		return r, nil
	}
	nr, err := s.detailFor(ctx, r.SourceID, r.PageURL)
	if err != nil {
		return r, err
	}
	mergeRow(&r, nr)
	return r, nil
}

// ScrapeURL 从详情页 URL 生成一条记录（链接抓取）。
// 用列表 id 字段的正则从 URL 里提取原生 id，再走详情解析。
func (s *RuleSource) ScrapeURL(ctx context.Context, rawURL string) (model.Row, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return model.Row{}, fmt.Errorf("链接不合法: %s", rawURL)
	}
	if s.pkg.Detail == nil {
		return model.Row{}, fmt.Errorf("源 %s 不支持链接抓取（没有 detail 段）", s.pkg.ID)
	}
	idField, ok := s.pkg.List.Fields["id"]
	if !ok || idField.Regex == "" {
		return model.Row{}, fmt.Errorf("源 %s 没有可用于链接抓取的 id 正则", s.pkg.ID)
	}
	re, err := regexp.Compile(idField.Regex)
	if err != nil {
		return model.Row{}, err
	}
	m := re.FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return model.Row{}, fmt.Errorf("这个链接不符合源 %s 的格式", s.pkg.ID)
	}
	row, err := s.detailFor(ctx, m[1], rawURL)
	if err != nil {
		return model.Row{}, err
	}
	if row.PageURL == "" {
		row.PageURL = rawURL
	}
	return row, nil
}

// Cover 解析封面地址（缺省回落到详情页）。
func (s *RuleSource) Cover(ctx context.Context, r model.Row) (string, error) {
	if r.CoverURL != "" {
		return r.CoverURL, nil
	}
	if s.pkg.Detail == nil {
		return "", nil
	}
	d, err := s.detailFor(ctx, r.SourceID, r.PageURL)
	if err != nil {
		return "", err
	}
	return d.CoverURL, nil
}

// Resolve 解析可下载地址；签名地址过期由 RefreshOn410 交给下载器。
func (s *RuleSource) Resolve(ctx context.Context, r model.Row) (source.DownloadSpec, error) {
	spec := source.DownloadSpec{URL: r.URL, Mode: "http", Headers: map[string]string{}}
	if s.pkg.Auth != nil {
		spec.UseAuth = true
	}
	if s.pkg.Download == nil {
		return spec, nil
	}
	dl := s.pkg.Download
	vars := map[string]string{
		"baseUrl":  s.pkg.BaseURL,
		"id":       r.SourceID,
		"playable": r.URL,
		"pageUrl":  r.PageURL,
		"url":      r.URL,
		"token":    r.Token,
		"sourceId": r.SourceID,
	}
	target := dl.URL
	if target == "" {
		target = r.URL
	} else {
		target = subst(target, vars)
	}
	if target == "" {
		return spec, fmt.Errorf("源 %s 没有解析出下载地址", s.pkg.ID)
	}
	target = absoluteURL(s.pkg.BaseURL, target)
	if s.http != nil {
		if err := s.http.Allowed(target); err != nil {
			return spec, err
		}
	}
	headers := s.headers(dl.Headers)
	for k, v := range headers {
		headers[k] = subst(v, vars)
	}
	host := hostOf(target)
	for _, h := range dl.ExcludeHeaderHosts {
		if strings.EqualFold(h, host) {
			headers = map[string]string{}
			break
		}
	}
	spec.URL = target
	spec.Mode = dl.Kind
	spec.Headers = headers
	for code, rr := range dl.OnStatus {
		if (code == "403" || code == "410") && rr.Action == "refreshDetail" {
			spec.RefreshOn410 = true
		}
	}
	return spec, nil
}

// ---------- 小工具 ----------

func mergeRow(dst *model.Row, src model.Row) {
	if src.Title != "" {
		dst.Title = src.Title
	}
	if src.CoverURL != "" {
		dst.CoverURL = src.CoverURL
	}
	if src.URL != "" {
		dst.URL = src.URL
	}
	if src.PageURL != "" {
		dst.PageURL = src.PageURL
	}
	if src.Duration != "" {
		dst.Duration = src.Duration
		dst.Seconds = src.Seconds
	}
	if src.Res != "" {
		dst.Res = src.Res
	}
	if src.Views != "" {
		dst.Views = src.Views
	}
	if src.Likes != "" {
		dst.Likes = src.Likes
	}
	if src.Uploaded != "" {
		dst.Uploaded = src.Uploaded
	}
	if src.Extra != "" {
		dst.Extra = src.Extra
	}
}

func firstOf(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

func extraFields(m map[string]string) string {
	known := map[string]bool{
		"id": true, "sourceId": true, "title": true, "name": true,
		"cover": true, "coverUrl": true, "playable": true, "url": true, "playUrl": true,
		"pageUrl": true, "page_url": true, "page": true, "res": true, "views": true,
		"likes": true, "uploaded": true, "uploadDate": true, "duration": true,
	}
	extra := map[string]string{}
	for k, v := range m {
		if !known[k] && strings.TrimSpace(v) != "" {
			extra[k] = v
		}
	}
	if len(extra) == 0 {
		return ""
	}
	b, _ := json.Marshal(extra)
	return string(b)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func validNativeID(id string) bool {
	if id == "" || len(id) > 120 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
