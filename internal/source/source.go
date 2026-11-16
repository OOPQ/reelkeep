// Package source 是多源适配器的注册表。
//
// 设计要点（与旧版的差异）：
//   - 能力用字符串声明，与 sources.json 一致；具体能力用可选接口协商
//     （Detailer / Coverer / DownloadResolver），下载器不再出现 if source == "qise"。
//   - 注册表只注册构造函数；启用/顺序/榜单仍由 configs/sources.json 控制，
//     加源或关源不需要改代码。
package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/OOPQ/reelkeep/internal/jsonx"
	"github.com/OOPQ/reelkeep/internal/model"
)

// 能力名（与 sources.json 的 capabilities 对应）。
const (
	CapScrape   = "scrape"
	CapDetail   = "detail"
	CapCover    = "cover"
	CapDownload = "download"
)

// Rank 是一个榜单/分类。
type Rank struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group,omitempty"`
	Path  string `json:"path,omitempty"`
}

// Manifest 是 sources.json / 导入源包对外暴露的一份描述（/api/state 的 sources 字段）。
type Manifest struct {
	Key           string   `json:"key"`
	Name          string   `json:"name"`
	File          string   `json:"-"`
	Enabled       bool     `json:"-"`
	Order         int      `json:"-"`
	Origin        string   `json:"origin,omitempty"` // builtin | imported
	Capabilities  []string `json:"capabilities"`
	SiteURL       string   `json:"siteUrl"`
	PageSize      int      `json:"pageSize"`
	SupportsPages bool     `json:"supportsPages"`
	Incremental   bool     `json:"incremental"`
	Ranks         []Rank   `json:"ranks"`
}

// ScrapeRequest 是一次抓取的参数（对应旧版 options）。
type ScrapeRequest struct {
	Rank        string
	Pages       []int
	Limit       int
	Threads     int
	PageSize    int
	Incremental bool
	FreshDays   int
	URL         string
	HD          bool
	Progress    func(phase string, done, total int)
}

// DownloadSpec 是解析后的可下载地址。
type DownloadSpec struct {
	URL     string
	Mode    string // http | hls
	Headers map[string]string
	// RefreshOn410 为 true 时，下载器遇到 410 应先重新解析一次源地址再重试。
	RefreshOn410 bool
}

// Source 是所有源都必须实现的接口。
type Source interface {
	ID() string
	Capabilities() []string
	// Scrape 流式产出规范化记录；emit 返回错误时应尽快停止。
	Scrape(ctx context.Context, req ScrapeRequest, emit func(model.Row) error) error
	HealthCheck(ctx context.Context) error
}

// Detailer 是可选能力：按原生 id 抓详情。
type Detailer interface {
	Detail(ctx context.Context, id string) (model.Row, error)
}

// Coverer 是可选能力：解析封面地址。
type Coverer interface {
	Cover(ctx context.Context, r model.Row) (string, error)
}

// DownloadResolver 是可选能力：解析/刷新可下载地址。
// qise 的签名刷新、标清回退都封在源内部，下载器不需要认识源名。
type DownloadResolver interface {
	Resolve(ctx context.Context, r model.Row) (DownloadSpec, error)
}

// Registry 保存构造函数、动态导入的源和已启用的清单。
type Registry struct {
	ctors     map[string]func() Source
	dynamic   map[string]Source
	manifests []Manifest
	extra     []Manifest
}

// manifestFile 是 sources.json 的原始形状；Manifest 里 Output 字段标了 "-"，
// 所以必须经这个中间结构解析，否则 enabled/order/file 永远是零值。
type manifestFile struct {
	Key           string   `json:"key"`
	Name          string   `json:"name"`
	File          string   `json:"file"`
	Enabled       *bool    `json:"enabled"`
	Order         int      `json:"order"`
	Capabilities  []string `json:"capabilities"`
	SiteURL       string   `json:"siteUrl"`
	PageSize      int      `json:"pageSize"`
	SupportsPages *bool    `json:"supportsPages"`
	Incremental   *bool    `json:"incremental"`
	Ranks         []Rank   `json:"ranks"`
}

// NewRegistry 从 sources.json 加载清单。
func NewRegistry(configPath string) (*Registry, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取源清单 %s: %w", configPath, err)
	}
	var raw []manifestFile
	if err := jsonx.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("解析源清单: %w", err)
	}
	out := make([]Manifest, 0, len(raw))
	for _, m := range raw {
		key := strings.ToLower(strings.TrimSpace(m.Key))
		if key == "" {
			continue
		}
		name := m.Name
		if name == "" {
			name = key
		}
		enabled := true
		if m.Enabled != nil {
			enabled = *m.Enabled
		}
		supportsPages := true
		if m.SupportsPages != nil {
			supportsPages = *m.SupportsPages
		}
		incremental := true
		if m.Incremental != nil {
			incremental = *m.Incremental
		}
		order := m.Order
		if order == 0 {
			order = 100
		}
		ranks := m.Ranks
		if ranks == nil {
			ranks = []Rank{}
		}
		caps := m.Capabilities
		if caps == nil {
			caps = []string{}
		}
		out = append(out, Manifest{
			Key:           key,
			Name:          name,
			File:          m.File,
			Enabled:       enabled,
			Order:         order,
			Capabilities:  caps,
			SiteURL:       m.SiteURL,
			PageSize:      m.PageSize,
			SupportsPages: supportsPages,
			Incremental:   incremental,
			Ranks:         ranks,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Key < out[j].Key
	})
	return &Registry{ctors: map[string]func() Source{}, manifests: out}, nil
}

// EmptyRegistry 在 sources.json 缺失时使用：前端仍能浏览台账，只是不能抓取。
func EmptyRegistry() *Registry {
	return &Registry{ctors: map[string]func() Source{}, dynamic: map[string]Source{}}
}

// Register 注册一个源的构造函数（在各适配器的 init 里调用）。
func (r *Registry) Register(id string, ctor func() Source) {
	r.ctors[strings.ToLower(id)] = ctor
}

// Add 挂载一个运行时加载的源（外部源包）。
func (r *Registry) Add(m Manifest, src Source) {
	if r.dynamic == nil {
		r.dynamic = map[string]Source{}
	}
	key := strings.ToLower(m.Key)
	r.dynamic[key] = src
	r.extra = append(r.extra, m)
}

// Enabled 返回已启用的源（内置 + 导入，按 order 排序）。
func (r *Registry) Enabled() []Manifest {
	all := make([]Manifest, 0, len(r.manifests)+len(r.extra))
	for _, m := range r.manifests {
		if m.Enabled {
			all = append(all, m)
		}
	}
	all = append(all, r.extra...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Order != all[j].Order {
			return all[i].Order < all[j].Order
		}
		return all[i].Key < all[j].Key
	})
	return all
}

// Get 按 id 取适配器（先内置，再导入）。
func (r *Registry) Get(id string) (Source, bool) {
	key := strings.ToLower(id)
	if ctor, ok := r.ctors[key]; ok {
		return ctor(), true
	}
	if src, ok := r.dynamic[key]; ok {
		return src, true
	}
	return nil, false
}

// Manifest 按 id 取清单项（内置或导入）。
func (r *Registry) Manifest(id string) (Manifest, bool) {
	key := strings.ToLower(id)
	for _, m := range r.manifests {
		if m.Key == key {
			return m, true
		}
	}
	for _, m := range r.extra {
		if m.Key == key {
			return m, true
		}
	}
	return Manifest{}, false
}

// DefaultConfigPath 在程序目录/当前目录下找 configs/sources.json。
func DefaultConfigPath() string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, "configs", "sources.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join("configs", "sources.json")
}
