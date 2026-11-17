package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/OOPQ/reelkeep/internal/store"
)

// handleRows 是媒体库的服务端分页/筛选接口。
func (s *Server) handleRows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	minDur, _ := strconv.Atoi(q.Get("minDur"))
	var tags []int64
	for _, raw := range strings.Split(q.Get("tags"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && id > 0 {
			tags = append(tags, id)
		}
	}
	rows, total, err := s.st.QueryRows(store.RowQuery{
		Q:      strings.TrimSpace(q.Get("q")),
		Status: q.Get("status"),
		Source: q.Get("source"),
		Flag:   q.Get("flag"),
		Res:    q.Get("res"),
		MinDur: minDur,
		Tags:   tags,
		Sort:   q.Get("sort"),
		Dir:    q.Get("dir"),
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		bad(w, "查询失败: "+err.Error())
		return
	}
	rows = s.attachTags(rows)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "rows": rows, "total": total, "offset": offset, "limit": limit,
	})
}
