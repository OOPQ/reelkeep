package api

import (
	"net/http"
	"testing"
)

func TestUpdateApplyDesktopOnly(t *testing.T) {
	_, ts := newP3Server(t)
	status, out := postJSONAccept(t, ts, "/api/update/apply", map[string]any{})
	if status != http.StatusOK || out["ok"] != false {
		t.Fatalf("web 模式应拒绝自动安装: status=%d %v", status, out)
	}
	if out["error"] == nil || out["error"] == "" {
		t.Fatalf("缺少 error: %v", out)
	}
}
