package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/update"
)

// handleUpdateApply 下载最新安装包，校验 SHA256 后静默安装（目前仅 Windows 桌面版）。
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !s.isDesktop() {
		bad(w, "自动安装只支持桌面版；Web 端请前往下载页")
		return
	}
	if runtime.GOOS != "windows" {
		bad(w, "自动安装目前只支持 Windows，请前往下载页")
		return
	}
	s.mu.RLock()
	current := s.version
	s.mu.RUnlock()
	if current == "" {
		current = "0.0.0-dev"
	}

	rel, err := update.Check(r.Context(), current)
	if err != nil {
		bad(w, err.Error())
		return
	}
	if !rel.HasUpdate {
		bad(w, "已经是最新版本")
		return
	}
	asset, ok := rel.InstallerAsset(runtime.GOOS, runtime.GOARCH)
	if !ok {
		bad(w, "Release 里没有找到 Windows 安装包")
		return
	}

	expected := update.ExpectedSHA(asset)
	if expected == "" {
		// 旧 Release 没有 digest 字段时，退回下载 SHA256SUMS
		sums, serr := update.Checksums(r.Context(), nil, rel.Assets)
		if serr == nil {
			expected = strings.ToLower(sums[filepath.Base(asset.Name)])
		}
	}
	if expected == "" {
		bad(w, "安装包缺少 SHA256 校验值，已拒绝自动安装")
		return
	}

	dir := filepath.Join(os.TempDir(), "reelkeep-update")
	dest := filepath.Join(dir, filepath.Base(asset.Name))
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	got, err := update.Download(ctx, &http.Client{Timeout: 25 * time.Minute}, asset.URL, dest, 600<<20)
	if err != nil {
		bad(w, "下载安装包失败: "+err.Error())
		return
	}
	if !strings.EqualFold(got, expected) {
		_ = os.Remove(dest)
		bad(w, fmt.Sprintf("SHA256 校验失败（期望 %s，实际 %s）", expected, got))
		return
	}

	cmd := exec.Command(dest, "/S")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		bad(w, "启动安装程序失败: "+err.Error())
		return
	}
	_ = cmd.Process.Release()
	s.logbuf.Add("update", "已下载并启动安装程序 "+rel.Latest+"（SHA256 校验通过）")

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "version": rel.Latest, "file": dest, "sha256": got,
	})
	// 安装器会 taskkill 当前进程；这里再兜底关窗
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if win := s.window(); win != nil {
			win.Close()
		}
	}()
}
