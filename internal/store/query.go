package store

import (
	"fmt"
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
)

// RowQuery 是服务端分页/筛选参数。
type RowQuery struct {
	Q      string
	Status string
	Source string
	Flag   string
	Res    string
	MinDur int
	Tags   []int64
	Sort   string
	Dir    string
	Offset int
	Limit  int
}

func (q RowQuery) where() (string, []any) {
	var parts []string
	var args []any
	parts = append(parts, "status <> 'recycled'")
	if q.Status != "" && q.Status != "all" {
		parts = append(parts, "status = ?")
		args = append(args, q.Status)
	}
	if q.Source != "" && q.Source != "all" {
		parts = append(parts, "source = ?")
		args = append(args, q.Source)
	}
	if q.Res != "" && q.Res != "all" {
		parts = append(parts, "res = ?")
		args = append(args, q.Res)
	}
	if q.MinDur > 0 {
		parts = append(parts, "CAST(NULLIF(seconds,'') AS INTEGER) >= ?")
		args = append(args, q.MinDur)
	}
	if len(q.Tags) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(q.Tags)), ",")
		parts = append(parts, "token IN (SELECT token FROM item_tags WHERE tag_id IN ("+ph+") GROUP BY token HAVING COUNT(DISTINCT tag_id) = ?)")
		for _, id := range q.Tags {
			args = append(args, id)
		}
		args = append(args, len(q.Tags))
	}
	switch q.Flag {
	case "fav":
		parts = append(parts, "fav = '1'")
	case "have":
		parts = append(parts, "file_name <> ''")
	case "miss":
		parts = append(parts, "file_name = ''")
	case "towatch":
		parts = append(parts, "file_name <> '' AND last_play = ''")
	case "watched":
		parts = append(parts, "last_play <> ''")
	case "rated":
		parts = append(parts, "CAST(NULLIF(rating,'') AS INTEGER) > 0")
	case "top":
		parts = append(parts, "CAST(NULLIF(rating,'') AS INTEGER) >= 4")
	case "today":
		parts = append(parts, "substr(first_seen,1,10) = date('now','localtime')")
	}
	search := strings.TrimSpace(q.Q)
	if search != "" {
		if len([]rune(search)) >= 3 {
			parts = append(parts, "rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?)")
			args = append(args, ftsQuery(search))
		} else {
			like := "%" + strings.ToLower(search) + "%"
			parts = append(parts, "(LOWER(title) LIKE ? OR LOWER(token) LIKE ? OR LOWER(source) LIKE ? OR LOWER(note) LIKE ?)")
			args = append(args, like, like, like, like)
		}
	}
	return strings.Join(parts, " AND "), args
}

func ftsQuery(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		fields[i] = `"` + f + `"`
	}
	return strings.Join(fields, " ")
}

func (q RowQuery) order() string {
	dir := "ASC"
	if strings.EqualFold(q.Dir, "desc") {
		dir = "DESC"
	}
	switch strings.ToLower(q.Sort) {
	case "title":
		return "LOWER(title) " + dir + ", rowid"
	case "duration":
		return "CAST(NULLIF(seconds,'') AS INTEGER) " + dir + ", rowid"
	case "views":
		return "CAST(NULLIF(views,'') AS INTEGER) " + dir + ", rowid"
	case "likes":
		return "CAST(NULLIF(likes,'') AS INTEGER) " + dir + ", rowid"
	case "rating":
		return "CAST(NULLIF(rating,'') AS INTEGER) " + dir + ", rowid"
	case "first_seen":
		return "first_seen " + dir + ", rowid"
	case "file_size":
		return "CAST(NULLIF(file_size,'') AS INTEGER) " + dir + ", rowid"
	case "rank":
		return "CAST(NULLIF(rank,'') AS INTEGER) " + dir + ", rowid"
	default:
		return "rowid DESC"
	}
}

// QueryRows 返回筛选后的当前页和总数。
func (s *Store) QueryRows(q RowQuery) ([]model.Row, int, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	where, args := q.where()
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sql := fmt.Sprintf("SELECT %s FROM items WHERE %s ORDER BY %s LIMIT ? OFFSET ?", s.cols, where, q.order())
	args = append(args, limit, offset)
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.Row
	for rows.Next() {
		var r model.Row
		if err := rows.Scan(r.Ptrs()...); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
