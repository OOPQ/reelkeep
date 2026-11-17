package store

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
)

var dirtyTitleSuffix = regexp.MustCompile(`\s*-\s*(免费视频|免費視頻|免费视频网|免費視頻網).*$`)

// dedupeItems 合并同一来源、同一原生 id 的旧 token 与带 source@ 前缀的新 token。
// 旧版本曾把原生 id 直接当 token，随后新版本又写成 source@id，导致同一条目出现两次。
func (s *Store) dedupeItems() error {
	rows, err := s.db.Query(`
		SELECT source, source_id, COUNT(*)
		FROM items
		WHERE source_id <> ''
		GROUP BY source, source_id
		HAVING COUNT(*) > 1`)
	if err != nil {
		return fmt.Errorf("dedupe groups: %w", err)
	}
	type group struct{ source, sourceID string }
	var groups []group
	for rows.Next() {
		var g group
		var n int
		if err := rows.Scan(&g.source, &g.sourceID, &n); err == nil {
			groups = append(groups, g)
		}
	}
	_ = rows.Close()
	for _, g := range groups {
		if err := s.dedupeGroup(g.source, g.sourceID); err != nil {
			return err
		}
	}
	return s.normalizeLegacyTokens("pektino")
}

// normalizeLegacyTokens turns bare native ids into source@native-id for one source.
func (s *Store) normalizeLegacyTokens(source string) error {
	rows, err := s.db.Query(`SELECT token, source_id FROM items
		WHERE LOWER(source) = ? AND source_id <> ''
		  AND token <> ? || '@' || source_id`, strings.ToLower(source), strings.ToLower(source))
	if err != nil {
		return err
	}
	type pair struct{ token, id string }
	var list []pair
	for rows.Next() {
		var p pair
		if rows.Scan(&p.token, &p.id) == nil {
			list = append(list, p)
		}
	}
	_ = rows.Close()
	for _, p := range list {
		canonical := strings.ToLower(source) + "@" + p.id
		var exists int
		_ = s.db.QueryRow("SELECT COUNT(*) FROM items WHERE token = ?", canonical).Scan(&exists)
		if exists > 0 {
			if err := s.dedupeGroup(strings.ToLower(source), p.id); err != nil {
				return err
			}
			continue
		}
		if _, err := s.db.Exec("UPDATE items SET token = ? WHERE token = ?", canonical, p.token); err != nil {
			return err
		}
		if err := s.moveTokenReferences(p.token, canonical); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) dedupeGroup(source, sourceID string) error {
	var tokens []string
	rs, err := s.db.Query("SELECT token FROM items WHERE source = ? AND source_id = ?", source, sourceID)
	if err != nil {
		return err
	}
	for rs.Next() {
		var t string
		if rs.Scan(&t) == nil {
			tokens = append(tokens, t)
		}
	}
	_ = rs.Close()
	if len(tokens) < 2 {
		return nil
	}
	canonical := strings.ToLower(source) + "@" + sourceID
	var base model.Row
	found := false
	for _, t := range tokens {
		if strings.EqualFold(t, canonical) {
			base, found, err = s.GetRow(t)
			if err != nil {
				return err
			}
			if found {
				break
			}
		}
	}
	if !found {
		// 没有规范 token 时，选信息最多的一条作为基础，再改名为规范 token。
		best := -1
		for _, t := range tokens {
			r, ok, err := s.GetRow(t)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			score := rowScore(r)
			if best < 0 || score > best {
				best, base = score, r
			}
		}
		if best < 0 {
			return nil
		}
	}
	for _, t := range tokens {
		if strings.EqualFold(t, base.Token) {
			continue
		}
		r, ok, err := s.GetRow(t)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		base = mergeDuplicate(base, r)
	}
	base.Token = canonical
	base.Source = source
	base.SourceID = sourceID
	base.Title = cleanDirtyTitle(base.Title, sourceID)
	if err := s.UpsertRows([]model.Row{base}); err != nil {
		return fmt.Errorf("dedupe upsert %s: %w", canonical, err)
	}
	for _, t := range tokens {
		if strings.EqualFold(t, canonical) {
			continue
		}
		if err := s.moveTokenReferences(t, canonical); err != nil {
			return err
		}
		if _, err := s.db.Exec("DELETE FROM items WHERE token = ?", t); err != nil {
			return fmt.Errorf("delete old token %s: %w", t, err)
		}
	}
	return nil
}

func (s *Store) moveTokenReferences(oldToken, newToken string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		"INSERT OR IGNORE INTO collection_items(collection_id, token, added_at) SELECT collection_id, ?, added_at FROM collection_items WHERE token = ?",
		"DELETE FROM collection_items WHERE token = ?",
	} {
		args := []any{oldToken}
		if strings.Contains(stmt, "SELECT") {
			args = []any{newToken, oldToken}
		}
		if _, err := tx.Exec(stmt, args...); err != nil {
			return fmt.Errorf("move %s: %s: %w", oldToken, stmt, err)
		}
	}
	for _, stmt := range []string{
		"INSERT OR IGNORE INTO item_tags(tag_id, token, added_at) SELECT tag_id, ?, added_at FROM item_tags WHERE token = ?",
		"DELETE FROM item_tags WHERE token = ?",
	} {
		args := []any{oldToken}
		if strings.Contains(stmt, "SELECT") {
			args = []any{newToken, oldToken}
		}
		if _, err := tx.Exec(stmt, args...); err != nil {
			return fmt.Errorf("move %s: %s: %w", oldToken, stmt, err)
		}
	}
	// 下载任务保留新 token 的任务；没有新任务时把旧任务迁移过去。
	var exists int
	_ = tx.QueryRow("SELECT COUNT(*) FROM download_tasks WHERE token = ?", newToken).Scan(&exists)
	if exists == 0 {
		if _, err := tx.Exec("UPDATE download_tasks SET token = ? WHERE token = ?", newToken, oldToken); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec("DELETE FROM download_tasks WHERE token = ?", oldToken); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE audit_log SET token = ? WHERE token = ?", newToken, oldToken); err != nil {
		return err
	}
	return tx.Commit()
}

func rowScore(r model.Row) int {
	n := 0
	for _, v := range []string{r.Title, r.CoverURL, r.URL, r.Duration, r.Res, r.Views, r.Likes, r.FirstSeen, r.LastSeen, r.FileName} {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

func mergeDuplicate(base, dup model.Row) model.Row {
	pick := func(a, b string) string {
		if strings.TrimSpace(a) != "" {
			return a
		}
		return b
	}
	base.CoverURL = pick(base.CoverURL, dup.CoverURL)
	base.URL = pick(base.URL, dup.URL)
	base.DownloadKind = pick(base.DownloadKind, dup.DownloadKind)
	base.DownloadHeaders = pick(base.DownloadHeaders, dup.DownloadHeaders)
	base.Duration = pick(base.Duration, dup.Duration)
	if base.Seconds == "" {
		base.Seconds = dup.Seconds
	}
	base.Res = pick(base.Res, dup.Res)
	base.Views = pick(base.Views, dup.Views)
	base.Likes = pick(base.Likes, dup.Likes)
	base.Uploaded = pick(base.Uploaded, dup.Uploaded)
	base.Rank = pick(base.Rank, dup.Rank)
	base.Page = pick(base.Page, dup.Page)
	base.Site = pick(base.Site, dup.Site)
	base.PageURL = pick(base.PageURL, dup.PageURL)
	base.Note = pick(base.Note, dup.Note)
	base.FileName = pick(base.FileName, dup.FileName)
	base.FileSize = pick(base.FileSize, dup.FileSize)
	base.FileDate = pick(base.FileDate, dup.FileDate)
	base.FilePath = pick(base.FilePath, dup.FilePath)
	base.PlayCount = pick(base.PlayCount, dup.PlayCount)
	base.LastPlay = pick(base.LastPlay, dup.LastPlay)
	base.Rating = pick(base.Rating, dup.Rating)
	base.Extra = pick(base.Extra, dup.Extra)
	if dup.Fav == "1" {
		base.Fav = "1"
	}
	if base.FirstSeen == "" || (dup.FirstSeen != "" && dup.FirstSeen < base.FirstSeen) {
		base.FirstSeen = dup.FirstSeen
	}
	if dup.LastSeen > base.LastSeen {
		base.LastSeen = dup.LastSeen
	}
	if base.Status == "" || base.Status == "new" {
		if dup.FileName != "" || dup.Status == "done" || dup.Status == "sent" {
			base.Status = dup.Status
		}
	}
	// 标题为空/Native ID/带站点后缀时，优先使用另一条更干净的标题。
	if betterTitle(dup.Title, base.Title, base.SourceID) {
		base.Title = dup.Title
	}
	return base
}

func betterTitle(candidate, current, sourceID string) bool {
	c := cleanDirtyTitle(candidate, sourceID)
	if c == "" {
		return false
	}
	cur := cleanDirtyTitle(current, sourceID)
	if cur == "" {
		return true
	}
	return len(c) > len(cur) && cur == sourceID
}

func cleanDirtyTitle(title, sourceID string) string {
	t := strings.TrimSpace(title)
	t = dirtyTitleSuffix.ReplaceAllString(t, "")
	t = strings.TrimSpace(t)
	if t == sourceID {
		return ""
	}
	return t
}
