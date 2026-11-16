package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallerAsset 从 Release 产物里挑当前平台的安装包。
func (r Release) InstallerAsset(goos, goarch string) (Asset, bool) {
	goos = strings.ToLower(goos)
	goarch = strings.ToLower(goarch)
	var candidates []Asset
	for _, a := range r.Assets {
		name := strings.ToLower(a.Name)
		switch goos {
		case "windows":
			if strings.HasPrefix(name, "reelkeep-setup-") && strings.HasSuffix(name, ".exe") {
				candidates = append(candidates, a)
			}
		case "darwin":
			if strings.HasSuffix(name, ".dmg") {
				candidates = append(candidates, a)
			}
		case "linux":
			if strings.HasSuffix(name, ".deb") {
				candidates = append(candidates, a)
			}
		}
	}
	if len(candidates) == 0 {
		return Asset{}, false
	}
	for _, a := range candidates {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, goarch) {
			return a, true
		}
	}
	return candidates[0], true
}

// ExpectedSHA 返回 Release asset 里带的 sha256（GitHub digest 字段）。
func ExpectedSHA(a Asset) string {
	d := strings.TrimSpace(strings.ToLower(a.Digest))
	return strings.TrimPrefix(d, "sha256:")
}

// Download 下载到 dest，返回实际 sha256（hex）；maxBytes<=0 时默认 600MB。
func Download(ctx context.Context, client *http.Client, url, dest string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 600 << 20
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Reelkeep-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if n > maxBytes {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("安装包超过大小上限（%d 字节）", maxBytes)
	}
	_ = os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Checksums 下载 Release 里的 SHA256SUMS 并解析成 map[文件名]hash。
func Checksums(ctx context.Context, client *http.Client, assets []Asset) (map[string]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	var sumsURL string
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if name == "sha256sums" || name == "sha256sums.txt" {
			sumsURL = a.URL
			break
		}
	}
	if sumsURL == "" {
		return nil, fmt.Errorf("Release 里没有 SHA256SUMS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Reelkeep-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 SHA256SUMS 失败: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		out[filepath.Base(name)] = strings.ToLower(fields[0])
	}
	return out, nil
}
