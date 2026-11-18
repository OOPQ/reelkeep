package rule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/OOPQ/reelkeep/internal/jsonx"
)

// SourceIndex 是「可用源」索引：一个 JSON 文件，列出可以一键安装/更新的源包。
// 格式与 Prowlarr/Stash 的社区源索引同构，只是字段更少。
type SourceIndex struct {
	Version int          `json:"version"`
	Sources []IndexEntry `json:"sources"`
}

// IndexEntry 是索引里的一项。
type IndexEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	MinApp      string `json:"minApp,omitempty"`
	Description string `json:"description,omitempty"`
}

// ParseIndex 解析并校验索引文件。
func ParseIndex(b []byte) (*SourceIndex, error) {
	var idx SourceIndex
	dec := json.NewDecoder(bytes.NewReader(jsonx.Clean(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("解析索引失败: %w", err)
	}
	if idx.Version != 1 {
		return nil, fmt.Errorf("不支持的索引版本: %d", idx.Version)
	}
	seen := map[string]bool{}
	for i := range idx.Sources {
		e := &idx.Sources[i]
		e.ID = strings.ToLower(strings.TrimSpace(e.ID))
		if e.ID == "" || e.Version == "" || e.File == "" {
			return nil, fmt.Errorf("sources[%d] 缺少 id/version/file", i)
		}
		if !idPattern.MatchString(e.ID) {
			return nil, fmt.Errorf("sources[%d] id 不合法: %q", i, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("索引里重复的源: %s", e.ID)
		}
		seen[e.ID] = true
		if e.SHA256 != "" && (len(e.SHA256) != 64 || strings.Trim(e.SHA256, "0123456789abcdef") != "") {
			return nil, fmt.Errorf("%s 的 sha256 不合法（应为 64 位小写十六进制）", e.ID)
		}
	}
	return &idx, nil
}

// Resolve 返回条目对应的源包地址（相对索引地址解析）。
func (i *SourceIndex) FileURL(indexURL string, e IndexEntry) (string, error) {
	base, err := url.Parse(indexURL)
	if err != nil {
		return "", fmt.Errorf("索引地址不合法: %w", err)
	}
	ref, err := url.Parse(e.File)
	if err != nil {
		return "", fmt.Errorf("%s 的 file 不合法: %w", e.ID, err)
	}
	return base.ResolveReference(ref).String(), nil
}

// VersionLess 比较 x.y.z 形式的版本号；无法解析时按字符串比较返回 false。
func VersionLess(a, b string) bool {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n := 0
		if p == "" {
			return out, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return out, false
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out, true
}
