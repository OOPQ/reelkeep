// Package desktoplogin 通过 WebView2 的 CDP 读取登录窗口的 Cookie。
package desktoplogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Cookie 是 CDP 返回的 cookie。
type Cookie struct {
	Name    string  `json:"name"`
	Value   string  `json:"value"`
	Domain  string  `json:"domain"`
	Path    string  `json:"path"`
	Expires float64 `json:"expires"`
}

type target struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	Title                string `json:"title"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// Capture 连接本机 WebView2 的 CDP，选出登录页 target，读取并拼出 Cookie 头。
func Capture(ctx context.Context, port int, domains []string) (string, error) {
	if port <= 0 {
		return "", errors.New("没有可用的调试端口")
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cli := http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json", nil)
	resp, err := cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("连接 WebView2 调试端口失败: %w", err)
	}
	defer resp.Body.Close()
	var targets []target
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return "", err
	}
	var chosen *target
	for i := range targets {
		t := &targets[i]
		if t.Type != "page" || t.WebSocketDebuggerURL == "" {
			continue
		}
		if hostMatches(t.URL, domains) {
			chosen = t
		}
	}
	if chosen == nil {
		return "", errors.New("没有找到登录页窗口，请先打开登录窗口")
	}

	dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(dctx, chosen.WebSocketDebuggerURL, nil)
	if err != nil {
		return "", fmt.Errorf("连接登录页调试通道失败: %w", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	var enabled struct{}
	if err := call(dctx, ws, 1, "Network.enable", nil, &enabled); err != nil {
		return "", err
	}
	var got struct {
		Cookies []Cookie `json:"cookies"`
	}
	if err := call(dctx, ws, 2, "Network.getAllCookies", nil, &got); err != nil {
		return "", err
	}

	now := float64(time.Now().Unix())
	best := map[string]Cookie{}
	for _, c := range got.Cookies {
		if !domainAllowed(c.Domain, domains) {
			continue
		}
		if c.Expires > 0 && c.Expires < now {
			continue
		}
		if c.Name == "" {
			continue
		}
		old, ok := best[c.Name]
		if !ok || prefer(c, old) {
			best[c.Name] = c
		}
	}
	if len(best) == 0 {
		return "", errors.New("没有读到该站点的 Cookie（可能尚未登录）")
	}
	names := make([]string, 0, len(best))
	for n := range best {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+"="+best[n].Value)
	}
	return strings.Join(parts, "; "), nil
}

func prefer(a, b Cookie) bool {
	// 同名的优先 path=/，其次更晚过期
	if a.Path == "/" && b.Path != "/" {
		return true
	}
	return a.Expires > b.Expires
}

func domainAllowed(cookieDomain string, domains []string) bool {
	host := strings.TrimPrefix(cookieDomain, ".")
	for _, d := range domains {
		d = strings.TrimPrefix(strings.ToLower(d), ".")
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func hostMatches(rawURL string, domains []string) bool {
	lower := strings.ToLower(rawURL)
	for _, d := range domains {
		d = strings.TrimPrefix(strings.ToLower(d), ".")
		if d != "" && strings.Contains(lower, d) {
			return true
		}
	}
	return false
}

func call(ctx context.Context, ws *websocket.Conn, id int, method string, params any, out any) error {
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, _ := json.Marshal(msg)
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &resp) != nil || resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		if out != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	}
}
