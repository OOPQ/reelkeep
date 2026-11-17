package api

import (
	"net/http"
	"strconv"
	"strings"
)

type TagView struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count,omitempty"`
	Has   bool   `json:"has,omitempty"`
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.Tags()
	if err != nil {
		bad(w, err.Error())
		return
	}
	tk := r.URL.Query().Get("token")
	if tk != "" {
		has, err := s.st.TagsForToken(tk)
		if err != nil {
			bad(w, err.Error())
			return
		}
		items := make([]TagView, 0, len(list))
		for _, c := range list {
			items = append(items, TagView{ID: c.ID, Name: c.Name, Color: c.Color, Has: has[c.ID]})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	items := make([]TagView, 0, len(list))
	for _, c := range list {
		items = append(items, TagView{ID: c.ID, Name: c.Name, Color: c.Color, Count: c.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleTagItems(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		bad(w, "标签 id 不合法")
		return
	}
	tokens, err := s.st.TagTokens(id)
	if err != nil {
		bad(w, err.Error())
		return
	}
	if tokens == nil {
		tokens = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "tokens": tokens})
}

func (s *Server) handleTagsWrite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string   `json:"action"`
		ID       int64    `json:"id"`
		TargetID int64    `json:"targetId"`
		Name     string   `json:"name"`
		Color    string   `json:"color"`
		Tokens   []string `json:"tokens"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	switch body.Action {
	case "create":
		name := strings.TrimSpace(body.Name)
		if name == "" {
			bad(w, "标签名不能为空")
			return
		}
		list, _ := s.st.Tags()
		color := body.Color
		if color == "" {
			color = palette[len(list)%len(palette)]
		}
		id, err := s.st.CreateTag(name, color)
		if err != nil {
			bad(w, "创建标签失败（可能重名）："+err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	case "rename", "color":
		if body.ID <= 0 {
			bad(w, "标签 id 不合法")
			return
		}
		if err := s.st.UpdateTag(body.ID, strings.TrimSpace(body.Name), body.Color); err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "delete":
		if body.ID <= 0 {
			bad(w, "标签 id 不合法")
			return
		}
		if err := s.st.DeleteTag(body.ID); err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "add", "remove":
		if body.ID <= 0 || len(body.Tokens) == 0 {
			bad(w, "参数不完整")
			return
		}
		var err error
		if body.Action == "add" {
			err = s.st.AddToTag(body.ID, body.Tokens)
		} else {
			err = s.st.RemoveFromTag(body.ID, body.Tokens)
		}
		if err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "merge":
		if body.ID <= 0 || body.TargetID <= 0 || body.ID == body.TargetID {
			bad(w, "合并标签参数不正确")
			return
		}
		if err := s.st.MergeTag(body.ID, body.TargetID); err != nil {
			bad(w, err.Error())
			return
		}
		s.Bump(true)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		bad(w, "未知动作: "+body.Action)
	}
}
