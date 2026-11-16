package api

import (
	"net/http"
	"strings"

	"github.com/OOPQ/reelkeep/internal/rule"
)

// SetLoginWindow 绑定桌面端的「应用内登录」窗口。
func (s *Server) SetLoginWindow(fn func(id, loginURL string) error) {
	s.mu.Lock()
	s.loginWin = fn
	s.mu.Unlock()
}

// SetLoginCapture 绑定桌面端的 Cookie 读取（CDP）。
func (s *Server) SetLoginCapture(fn func(id string) (string, error)) {
	s.mu.Lock()
	s.loginCapture = fn
	s.mu.Unlock()
}

// handleDesktopLoginCapture 读取登录窗口的 Cookie 并写入加密凭据。
func (s *Server) handleDesktopLoginCapture(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	s.mu.RLock()
	fn := s.loginCapture
	s.mu.RUnlock()
	if fn == nil {
		bad(w, "当前不是桌面窗口")
		return
	}
	cookie, err := fn(body.ID)
	if err != nil {
		bad(w, err.Error())
		return
	}
	if cookie == "" {
		bad(w, "没有读到 Cookie，请确认已在登录窗口完成登录")
		return
	}
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	if err := s.srcMgr.SetAuth(body.ID, cookie); err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("auth", "已从登录窗口读取并保存 Cookie（"+body.ID+"）")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "length": len(cookie)})
}

// handleDesktopLogin 在桌面壳里打开登录窗口（带「完成登录并保存」按钮）。
func (s *Server) handleDesktopLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if s.srcMgr == nil {
		bad(w, "源管理器未启用")
		return
	}
	var item *rule.ItemView
	for _, it := range s.srcMgr.List() {
		if strings.EqualFold(it.ID, body.ID) {
			copied := it
			item = &copied
			break
		}
	}
	if item == nil {
		bad(w, "源不存在: "+body.ID)
		return
	}
	if item.LoginURL == "" {
		bad(w, "该源没有配置 loginUrl")
		return
	}
	s.mu.RLock()
	fn := s.loginWin
	s.mu.RUnlock()
	if fn == nil {
		bad(w, "当前不是桌面窗口，请用浏览器登录后手动粘贴 Cookie")
		return
	}
	if err := fn(item.ID, item.LoginURL); err != nil {
		bad(w, "打开登录窗口失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "loginUrl": item.LoginURL})
}
