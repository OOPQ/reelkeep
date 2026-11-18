package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// scrapeViaResolve 把链接交给第三方解析接口（GET/POST），从返回 JSON 里取字段。
// 用于「站点本身反爬、但公开的解析服务能拿到直链」的场景，比如 X/Twitter。
func (s *RuleSource) scrapeViaResolve(ctx context.Context, u *url.URL, id string) (model.Row, error) {
	rs := s.pkg.Resolve
	pf, ok := s.fetcher.(source.PostFetcher)
	if !ok {
		return model.Row{}, fmt.Errorf("源 %s 的解析接口需要 POST 能力，当前抓取器不支持", s.pkg.ID)
	}
	body := strings.ReplaceAll(rs.Body, "{pageUrl}", u.String())
	body = strings.ReplaceAll(body, "{id}", id)
	headers := map[string]string{"Content-Type": "application/json"}
	for k, v := range rs.Headers {
		headers[k] = v
	}
	raw, err := pf.Post(ctx, rs.URL, headers, body)
	if err != nil {
		return model.Row{}, fmt.Errorf("解析接口请求失败: %w", err)
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return model.Row{}, fmt.Errorf("解析接口返回的不是 JSON: %w", err)
	}
	fields, err := extractJSONFields(root, rs.Fields)
	if err != nil {
		return model.Row{}, err
	}
	fields["id"] = id
	if fields["pageUrl"] == "" {
		fields["pageUrl"] = u.String()
	}
	row, err := s.rowFromFields(fields, u, id)
	if err != nil {
		return model.Row{}, err
	}
	if row.URL == "" {
		return model.Row{}, fmt.Errorf("解析接口没有返回播放地址")
	}
	if row.Title == "" {
		row.Title = s.pkg.Name + " " + id
	}
	return row, nil
}
