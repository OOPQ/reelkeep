package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/config"
)

// WindowController 由桌面壳实现，用于窗口按钮。
type WindowController interface {
	Minimise()
	ToggleMaximise()
	Close()
	IsMaximised() bool
}

// SetDesktop 标记当前进程是否跑在桌面壳里。
func (s *Server) SetDesktop(v bool) {
	s.mu.Lock()
	s.desktop = v
	s.mu.Unlock()
}

// SetWindowController 绑定桌面窗口。
func (s *Server) SetWindowController(w WindowController) {
	s.mu.Lock()
	s.win = w
	s.mu.Unlock()
}

func (s *Server) isDesktop() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.desktop
}

func (s *Server) window() WindowController {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.win
}

// handleDesktop 返回桌面壳状态（Web 端为 false）。
func (s *Server) handleDesktop(w http.ResponseWriter, r *http.Request) {
	win := s.window()
	max := win != nil && win.IsMaximised()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "desktop": s.isDesktop(), "maximized": max,
	})
}

// handleDesktopWindow 处理最小化/最大化/关闭。
func (s *Server) handleDesktopWindow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	win := s.window()
	if win == nil {
		bad(w, "当前不是桌面窗口")
		return
	}
	switch body.Action {
	case "minimize":
		win.Minimise()
	case "maximize":
		win.ToggleMaximise()
	case "close":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "closing": true})
		go func() {
			time.Sleep(80 * time.Millisecond)
			win.Close()
		}()
		return
	default:
		bad(w, "未知窗口操作: "+body.Action)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "maximized": win.IsMaximised()})
}

type projectEntry struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// handleProjects 返回当前项目与最近项目。
func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	current := s.paths.Root
	seen := map[string]bool{}
	recent := make([]projectEntry, 0, 10)
	add := func(p string) {
		key := strings.ToLower(filepath.Clean(p))
		if p == "" || seen[key] {
			return
		}
		seen[key] = true
		recent = append(recent, projectEntry{
			Path: p, Name: filepath.Base(p),
			Current: strings.EqualFold(filepath.Clean(p), filepath.Clean(current)),
		})
	}
	add(current)
	for _, p := range config.RecentProjects() {
		add(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"current": projectEntry{
			Path: current, Name: filepath.Base(current), Current: true,
		},
		"recent":  recent,
		"desktop": s.isDesktop(),
	})
}

// handleProjectsOpen 把某个文件夹作为项目打开：
// 桌面壳开新窗口；浏览器模式启动一个新的本机实例并返回 URL。
func (s *Server) handleProjectsOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	root, err := config.EnsureProject(strings.TrimSpace(body.Path))
	if err != nil {
		bad(w, err.Error())
		return
	}
	_ = config.SaveProject(root)

	if s.isDesktop() {
		if err := spawnSelf(root, 0); err != nil {
			bad(w, "打开新窗口失败: "+err.Error())
			return
		}
		s.logbuf.Add("project", "打开项目（新窗口）："+root)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "action": "new-window", "path": root,
		})
		return
	}

	port, err := freePort()
	if err != nil {
		bad(w, "找不到空闲端口: "+err.Error())
		return
	}
	if err := spawnSelf(root, port); err != nil {
		bad(w, "启动新实例失败: "+err.Error())
		return
	}
	url, err := waitProject(port, root, 20*time.Second)
	if err != nil {
		bad(w, err.Error())
		return
	}
	s.logbuf.Add("project", "打开项目（新实例）："+root)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "action": "new-instance", "url": url, "path": root,
	})
}

// spawnSelf 启动一个自己的新进程；port=0 时由子进程自己选端口。
func spawnSelf(root string, port int) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"-project", root}
	if port > 0 {
		args = append(args, "-port", strconv.Itoa(port), "-no-browser")
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p, nil
}

func waitProject(port int, root string, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	client := http.Client{Timeout: 1200 * time.Millisecond}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url + "api/state")
		if err == nil {
			var p struct {
				OK      bool `json:"ok"`
				Project struct {
					Root string `json:"root"`
				} `json:"project"`
			}
			if json.NewDecoder(resp.Body).Decode(&p) == nil && p.OK &&
				strings.EqualFold(filepath.Clean(p.Project.Root), filepath.Clean(root)) {
				_ = resp.Body.Close()
				return url, nil
			}
			_ = resp.Body.Close()
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("新项目实例启动超时（端口 %d），请查看 data/logs/console.log", port)
}
