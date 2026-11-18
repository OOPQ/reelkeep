package rule

import (
	"context"
	"fmt"
	"net/url"

	"github.com/PuerkitoBio/goquery"

	"github.com/OOPQ/reelkeep/internal/model"
)

// SupportsPlaylist 报告源包是否声明了 playlist 段。
func (s *RuleSource) SupportsPlaylist() bool { return s.pkg.Playlist != nil }

// ScrapePlaylist 解析播放清单页，逐条补齐详情并返回清单名。
func (s *RuleSource) ScrapePlaylist(ctx context.Context, rawURL string) ([]model.Row, string, error) {
	if s.pkg.Playlist == nil {
		return nil, "", fmt.Errorf("源 %s 没有 playlist 段", s.pkg.ID)
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", fmt.Errorf("播放清单链接不合法: %s", rawURL)
	}
	body, err := s.fetcher.Get(ctx, rawURL, s.headers(nil))
	if err != nil {
		return nil, "", fmt.Errorf("播放清单页 %s: %w", rawURL, err)
	}
	doc, err := parseHTML(body)
	if err != nil {
		return nil, "", err
	}
	name := ""
	if s.pkg.Playlist.Name.Selector != "" || s.pkg.Playlist.Name.Raw {
		fields, ferr := extractHTMLFields(doc, doc.Selection, map[string]Field{"name": s.pkg.Playlist.Name}, u)
		if ferr == nil {
			name = fields["name"]
		}
	}

	var out []model.Row
	seen := map[string]bool{}
	nodes := doc.Find(s.pkg.Playlist.Item)
	nodes.EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if len(out) >= 200 {
			return false
		}
		fields, ferr := extractHTMLFields(doc, sel, s.pkg.Playlist.Fields, u)
		if ferr != nil {
			return true
		}
		r, rerr := s.rowFromFields(fields, u, "")
		if rerr != nil || r.SourceID == "" || seen[r.Token] {
			return true
		}
		seen[r.Token] = true
		r.Rank = "playlist"
		if s.pkg.Detail != nil && (s.pkg.Detail.Always || r.URL == "") {
			if d, derr := s.detailFor(ctx, r.SourceID, r.PageURL); derr == nil {
				mergeRow(&r, d)
			}
		}
		out = append(out, r)
		return true
	})
	if len(out) == 0 {
		return nil, name, fmt.Errorf("播放清单里没有可解析的条目")
	}
	return out, name, nil
}
