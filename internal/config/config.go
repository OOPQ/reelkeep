// Package config 负责「程序 vs 项目」的路径模型和项目选择。
//
// 项目（project）是数据单元：project.json + data\ + media\ + cache\，
// 程序目录只放二进制与前端产物。升级程序不动数据，换项目等于换一个数据目录。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/jsonx"
)

// Paths 是一个项目的固定目录约定（不可配置）。
type Paths struct {
	Root        string
	ProjectFile string
	DataDir     string
	Database    string
	LedgerCSV   string
	Content     string
	Covers      string
	Thumbs      string
	Logs        string
	Exports     string
	Snapshots   string
	Diagnostics string
}

// ForRoot 按项目根目录推导全部路径。
func ForRoot(root string) Paths {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return Paths{
		Root:        root,
		ProjectFile: filepath.Join(root, "project.json"),
		DataDir:     filepath.Join(root, "data"),
		Database:    filepath.Join(root, "data", "data.db"),
		LedgerCSV:   filepath.Join(root, "data", "ledger.csv"),
		Content:     filepath.Join(root, "media"),
		Covers:      filepath.Join(root, "cache", "covers"),
		Thumbs:      filepath.Join(root, "cache", "thumbs"),
		Logs:        filepath.Join(root, "data", "logs"),
		Exports:     filepath.Join(root, "data", "exports"),
		Snapshots:   filepath.Join(root, "data", "snapshots"),
		Diagnostics: filepath.Join(root, "data", "diagnostics"),
	}
}

// Ensure 创建项目结构（已存在则只补缺）。
func (p Paths) Ensure() error {
	for _, d := range []string{p.DataDir, p.Content, p.Covers, p.Thumbs, p.Logs, p.Exports, p.Snapshots, p.Diagnostics} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s: %w", d, err)
		}
	}
	if _, err := os.Stat(p.ProjectFile); os.IsNotExist(err) {
		meta := map[string]any{
			"version": 1,
			"kind":    "reelkeep-project",
			"created": time.Now().Format("2006-01-02T15:04:05"),
		}
		b, _ := json.MarshalIndent(meta, "", "  ")
		if err := os.WriteFile(p.ProjectFile, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// IsProject 判断目录是否像一个项目：有 project.json，或已有 data\ 目录。
func IsProject(root string) bool {
	if root == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, "project.json")); err == nil {
		return true
	}
	if st, err := os.Stat(filepath.Join(root, "data")); err == nil && st.IsDir() {
		return true
	}
	return false
}

// Settings 对应 项目\data\config.json。
type Settings struct {
	Proxy          string `json:"proxy"`
	Concurrent     int    `json:"concurrent"`
	FFmpegPath     string `json:"ffmpegPath"`
	RetryCount     int    `json:"retryCount"`
	RetryDelay     int    `json:"retryDelay"`
	TorrentLimitKB int    `json:"torrentLimitKB"`
	HTTPLimitKB    int    `json:"httpLimitKB"`
	// HTTPConnections 是单个 HTTP 直链的最大并行分片连接数；1 表示单连接模式。
	HTTPConnections int `json:"httpConnections"`
	// PerSourceConcurrent 是同一来源同时下载的文件数。
	PerSourceConcurrent int    `json:"perSourceConcurrent"`
	TorrentBackend      string `json:"torrentBackend"`
	QbtURL              string `json:"qbtUrl"`
	QbtUser             string `json:"qbtUser"`
	QbtPass             string `json:"qbtPass"`
	// SourceIndexURL 是「可用源」索引地址（JSON）；空表示用内置默认。
	SourceIndexURL string `json:"sourceIndexUrl"`
}

// DefaultSourceIndexURL 指向仓库里的公共源索引。
const DefaultSourceIndexURL = "https://raw.githubusercontent.com/OOPQ/reelkeep/main/sources/index.json"

// LoadSettings 读配置；缺省值与原版一致。
func LoadSettings(p Paths) Settings {
	s := Settings{Concurrent: 8, RetryCount: 4, RetryDelay: 1}
	b, err := os.ReadFile(filepath.Join(p.DataDir, "config.json"))
	if err != nil {
		return s
	}
	_ = jsonx.Unmarshal(b, &s)
	if s.Concurrent <= 0 {
		s.Concurrent = 8
	}
	if s.RetryCount <= 0 {
		s.RetryCount = 4
	}
	if s.RetryDelay <= 0 {
		s.RetryDelay = 1
	}
	// HTTPConnections 默认 8：自适应会从 4 条开始试探，确认更快才升到 6/8；
	// 服务器不支持分片时自动回退单连接。
	if s.HTTPConnections <= 0 {
		s.HTTPConnections = 8
	}
	if s.HTTPConnections > 16 {
		s.HTTPConnections = 16
	}
	if s.PerSourceConcurrent <= 0 {
		s.PerSourceConcurrent = 4
	}
	if s.PerSourceConcurrent > 8 {
		s.PerSourceConcurrent = 8
	}
	if s.TorrentBackend == "" {
		s.TorrentBackend = "builtin"
	}
	if s.SourceIndexURL == "" {
		s.SourceIndexURL = DefaultSourceIndexURL
	}
	return s
}

// SaveSettings 只持久化真正需要记住的项（contentDir 由项目结构决定，不写绝对路径）。
func SaveSettings(p Paths, s Settings) error {
	if err := os.MkdirAll(p.DataDir, 0o755); err != nil {
		return err
	}
	out := map[string]any{
		"proxy":               s.Proxy,
		"concurrent":          s.Concurrent,
		"retryCount":          s.RetryCount,
		"retryDelay":          s.RetryDelay,
		"httpConnections":     s.HTTPConnections,
		"perSourceConcurrent": s.PerSourceConcurrent,
	}
	if s.FFmpegPath != "" {
		out["ffmpegPath"] = s.FFmpegPath
	}
	if s.SourceIndexURL != "" {
		out["sourceIndexUrl"] = s.SourceIndexURL
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return os.WriteFile(filepath.Join(p.DataDir, "config.json"), b, 0o644)
}

// ---------- 最近项目 ----------

type registry struct {
	Last    string   `json:"last"`
	Recent  []string `json:"recent"`
	Updated string   `json:"updated"`
}

// appDataDir 返回配置目录；Windows 用 %APPDATA%\Reelkeep，其它平台用 UserConfigDir。
func appDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		if os.PathSeparator == '\\' {
			return filepath.Join(dir, "Reelkeep")
		}
		return filepath.Join(dir, "reelkeep")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".reelkeep")
}

// registryPath 是新版记录文件；旧版在 %APPDATA%\pektino\projects.json。
func registryPath() string { return filepath.Join(appDataDir(), "projects.json") }

// ImportedSourcesDir 是外部源包的存放目录（与项目无关，换项目不用重装源）。
func ImportedSourcesDir() string { return filepath.Join(appDataDir(), "sources") }

func legacyRegistryPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "pektino", "projects.json")
	}
	return ""
}

func readRegistry(path string) registry {
	var r registry
	b, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	_ = jsonx.Unmarshal(b, &r)
	return r
}

// RecentProjects 返回可用（目录仍存在）的最近项目。
func RecentProjects() []string {
	r := readRegistry(registryPath())
	if len(r.Recent) == 0 && len(r.Last) == 0 {
		// 首次运行：沿用旧版的最近项目，直接打开用户原来的库
		if legacy := legacyRegistryPath(); legacy != "" {
			r = readRegistry(legacy)
		}
	}
	out := make([]string, 0, len(r.Recent)+1)
	for _, p := range append([]string{r.Last}, r.Recent...) {
		if p == "" || !IsProject(p) {
			continue
		}
		dup := false
		for _, x := range out {
			if strings.EqualFold(x, p) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}

// LastProject 返回上次打开的项目（无效则空）。
func LastProject() string {
	r := readRegistry(registryPath())
	if IsProject(r.Last) {
		return r.Last
	}
	if legacy := legacyRegistryPath(); legacy != "" {
		lr := readRegistry(legacy)
		if IsProject(lr.Last) {
			return lr.Last
		}
	}
	return ""
}

// SaveProject 把项目写进最近列表。
func SaveProject(root string) error {
	if root == "" {
		return nil
	}
	r := readRegistry(registryPath())
	list := []string{root}
	for _, p := range append([]string{r.Last}, r.Recent...) {
		if p == "" || strings.EqualFold(p, root) {
			continue
		}
		list = append(list, p)
	}
	if len(list) > 10 {
		list = list[:10]
	}
	r.Last = root
	r.Recent = list
	r.Updated = time.Now().Format(time.RFC3339)
	if err := os.MkdirAll(appDataDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return os.WriteFile(registryPath(), b, 0o644)
}

// DefaultRoot 是新用户的默认项目目录：<文档>\Reelkeep。
func DefaultRoot() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		docs := filepath.Join(home, "Documents")
		if st, err := os.Stat(docs); err == nil && st.IsDir() {
			return filepath.Join(docs, "Reelkeep")
		}
		return filepath.Join(home, "Reelkeep")
	}
	return filepath.Join(".", "Reelkeep")
}

// EnsureProject 把任意文件夹变成可打开的项目：
//   - 已是项目：直接返回
//   - 不存在：创建目录与项目结构
//   - 存在但非空且不是项目：拒绝，避免误把杂物目录当项目
func EnsureProject(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("项目路径为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err == nil {
		if !st.IsDir() {
			return "", fmt.Errorf("不是文件夹：%s", abs)
		}
		if IsProject(abs) {
			return abs, nil
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return "", err
		}
		nonEmpty := false
		for _, e := range entries {
			if e.Name() != "project.json" {
				nonEmpty = true
				break
			}
		}
		if nonEmpty {
			return "", fmt.Errorf("目录不是 Reelkeep 项目，且非空：%s", abs)
		}
	}
	paths := ForRoot(abs)
	if err := paths.Ensure(); err != nil {
		return "", err
	}
	return abs, nil
}

// Resolve 决定要打开哪个项目：显式 -project > 最近项目 > 默认目录。
func Resolve(flag string) (string, error) {
	if flag != "" {
		return EnsureProject(flag)
	}
	if last := LastProject(); last != "" {
		return last, nil
	}
	return DefaultRoot(), nil
}

// ErrNoProject 在需要交互选择时返回（当前版本暂不做交互选择器）。
var ErrNoProject = errors.New("没有可用项目")
