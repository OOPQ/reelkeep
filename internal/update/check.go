// Package update 检查 GitHub Releases，并支持校验下载安装包。
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Asset 是 Release 里的一个产物。
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// Release 是一次检查的结果。
type Release struct {
	Current   string  `json:"current"`
	Latest    string  `json:"latest"`
	HasUpdate bool    `json:"hasUpdate"`
	URL       string  `json:"url"`
	Notes     string  `json:"notes,omitempty"`
	Assets    []Asset `json:"assets,omitempty"`
}

type ghRelease struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Body    string  `json:"body"`
	Assets  []Asset `json:"assets"`
}

// Repo 返回要检查的仓库，可在构建/运行环境覆盖。
func Repo() string {
	if r := strings.TrimSpace(os.Getenv("REELKEEP_UPDATE_REPO")); r != "" {
		return r
	}
	return "OOPQ/reelkeep"
}

// Check 查询最新 Release 并和当前版本比较。
func Check(ctx context.Context, current string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := "https://api.github.com/repos/" + Repo() + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Reelkeep/"+current)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("检查更新失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, fmt.Errorf("没有找到 Release（仓库尚未发布或不存在）")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("检查更新失败: HTTP %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	out := Release{
		Current: current, Latest: latest, URL: rel.HTMLURL, Notes: rel.Body,
		Assets: rel.Assets,
	}
	out.HasUpdate = newer(latest, current)
	return out, nil
}

func newer(latest, current string) bool {
	a, b := parse(latest), parse(current)
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, part := range strings.Split(v, ".") {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(part)
		out[i] = n
	}
	return out
}
