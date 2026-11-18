package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGuardRejectsCrossSite 确认跨站来源被挡、同源放行、健康检查是 JSON。
func TestGuardRejectsCrossSite(t *testing.T) {
	s, _ := newP3Server(t)
	h := s.Handler()

	cases := []struct {
		name   string
		host   string
		origin string
		fetch  string
		want   int
	}{
		{"loopback", "127.0.0.1:8791", "", "", 200},
		{"localhost origin", "127.0.0.1:8791", "http://127.0.0.1:8791", "", 200},
		{"cross-site origin", "127.0.0.1:8791", "https://evil.example", "", 403},
		{"cross-site fetch", "127.0.0.1:8791", "", "cross-site", 403},
		{"rebinding host", "attacker.example:8791", "", "", 403},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/health", nil)
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.fetch != "" {
				req.Header.Set("Sec-Fetch-Site", c.fetch)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, c.want, rec.Body.String())
			}
			if c.want == 200 && !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("health 不是 JSON: %s", rec.Header().Get("Content-Type"))
			}
		})
	}
}

// TestUnknownAPI404 未注册的 /api/* 要返回 JSON 404，不能落到前端页面。
func TestUnknownAPI404(t *testing.T) {
	s, _ := newP3Server(t)
	req := httptest.NewRequest("GET", "/api/nope", nil)
	req.Host = "127.0.0.1:8791"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"ok":false`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
