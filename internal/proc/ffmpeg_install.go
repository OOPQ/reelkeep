package proc

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const ffmpegWindowsURL = "https://github.com/eugeneware/ffmpeg-static/releases/download/b6.0/ffmpeg-win32-x64.gz"

// FFmpegBinDir 返回应用自己的工具目录。
func FFmpegBinDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	name := "reelkeep"
	if runtime.GOOS == "windows" {
		name = "Reelkeep"
	}
	return filepath.Join(base, name, "bin")
}

// InstallFFmpeg 下载官方/常用 Windows 静态构建，解压出 ffmpeg 到应用工具目录。
// macOS/Linux 不强行下载第三方二进制，返回可执行的安装提示。
func InstallFFmpeg(ctx context.Context, proxy string) (string, error) {
	if runtime.GOOS != "windows" {
		if runtime.GOOS == "darwin" {
			return "", errors.New("macOS 请先执行 brew install ffmpeg，或在设置里填写 ffmpeg 路径")
		}
		return "", errors.New("Linux 请先执行 sudo apt install ffmpeg，或在设置里填写 ffmpeg 路径")
	}
	dir := FFmpegBinDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "ffmpeg.exe")

	tmp, err := os.CreateTemp("", "reelkeep-ffmpeg-*.zip")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if p := strings.TrimSpace(proxy); p == "" {
		proxy = systemProxy()
	}
	if p := strings.TrimSpace(proxy); p != "" {
		u, err := url.Parse(p)
		if err != nil {
			_ = tmp.Close()
			return "", errors.New("代理格式不正确")
		}
		transport.Proxy = http.ProxyURL(u)
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ffmpegWindowsURL, nil)
	if err != nil {
		_ = tmp.Close()
		return "", err
	}
	req.Header.Set("User-Agent", "Reelkeep/ffmpeg-installer")
	resp, err := client.Do(req)
	if err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("下载 FFmpeg 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = tmp.Close()
		return "", fmt.Errorf("下载 FFmpeg 失败: HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 256<<20)); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("写入 FFmpeg 安装包失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	in, err := os.Open(tmpPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return "", fmt.Errorf("FFmpeg 安装包损坏: %w", err)
	}
	defer gz.Close()
	out, err := os.OpenFile(target+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, io.LimitReader(gz, 256<<20))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(target + ".part")
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target + ".part")
		return "", closeErr
	}
	if err := os.Rename(target+".part", target); err != nil {
		return "", err
	}
	return target, nil
}
