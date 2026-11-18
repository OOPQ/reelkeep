// Package source 是多源适配器的注册表。
//
// 设计要点（与旧版的差异）：
//   - 能力用字符串声明，与 sources.json 一致；具体能力用可选接口协商
//     （Detailer / Coverer / DownloadResolver），下载器不按源名分支。
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
	"sync"

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
	AuthType      string   `json:"authType,omitempty"`
	Capabilities  []string `json:"capabilities"`
	SiteURL       string   `json:"siteUrl"`
	PageSize      int      `json:"pageSize"`
	SupportsPages bool     `json:"supportsPages"`
	Incremental   bool     `json:"incremental"`
	Ranks         []Rank   `json:"ranks"`
	// LinkMatch 是链接抓取时的归类依据：正则命中则优先用这个源。
	LinkMatch []string `json:"linkMatch,omitempty"`
	// LinkHosts 是额外的链接归类域名（比如 pektino 源也接 X/Twitter 链接）。
	LinkHosts []string `json:"linkHosts,omitempty"`
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
	// KnownTokens 里的 token 视为「最近抓过」，跳过详情页，只保留列表字段。
	KnownTokens map[string]bool
	// DetailWorkers 覆盖源包里的详情并发数（0 = 用源包默认）。
	DetailWorkers int
}

// DownloadSpec 是解析后的可下载地址。
type DownloadSpec struct {
	URL     string
	Mode    string // http | hls
	Headers map[string]string
	// RefreshOn410 为 true 时，下载器遇到 410 应先重新解析一次源地址再重试。
	RefreshOn410 bool
	// UseAuth 为 true 时，下载器应带上该源保存的登录凭据（Cookie）。
	UseAuth bool
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
// 签名刷新、码率回退等站点细节都封在源内部，下载器不需要认识源名。
type DownloadResolver interface {
	Resolve(ctx context.Context, r model.Row) (DownloadSpec, error)
}

// Refresher 是可选能力：签名地址过期后重新解析这条记录。
type Refresher interface {
	Refresh(ctx context.Context, r model.Row) (model.Row, error)
}

// URLScraper 是可选能力：从一个页面/直链直接生成一条记录（链接抓取）。
type URLScraper interface {
	ScrapeURL(ctx context.Context, rawURL string) (model.Row, error)
}

// PlaylistScraper 是可选能力：从播放清单页批量抓取条目，并返回清单名。
type PlaylistScraper interface {
	ScrapePlaylist(ctx context.Context, rawURL string) (rows []model.Row, name string, err error)
}

// PlaylistSupporter 由「声明式实现」提供：只有源包真的写了 playlist 段才算支持，
// 否则走普通的单条链接抓取。
type PlaylistSupporter interface {
	SupportsPlaylist() bool
}

// PostFetcher 是可选能力：对第三方解析接口发 POST，返回响应体。
type PostFetcher interface {
	Post(ctx context.Context, rawURL string, headers map[string]string, body string) ([]byte, error)
}

// Registry 保存构造函数、动态导入的源和已启用的清单。
type Registry struct {
	mu        sync.RWMutex
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ctors[strings.ToLower(id)] = ctor
}

// Add 挂载一个运行时加载的源（外部源包）。同一 id 会覆盖。
func (r *Registry) Add(m Manifest, src Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dynamic == nil {
		r.dynamic = map[string]Source{}
	}
	key := strings.ToLower(m.Key)
	r.dynamic[key] = src
	kept := r.extra[:0]
	for _, x := range r.extra {
		if x.Key != key {
			kept = append(kept, x)
		}
	}
	r.extra = append(kept, m)
}

// Remove 卸载一个动态源。
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(id)
	delete(r.dynamic, key)
	kept := r.extra[:0]
	for _, x := range r.extra {
		if x.Key != key {
			kept = append(kept, x)
		}
	}
	r.extra = kept
}

// sourceRank 固定本地在最前、其他在最后。
func sourceRank(key string) int {
	switch strings.ToLower(key) {
	case "local":
		return 0
	case "other":
		return 2
	default:
		return 1
	}
}

// Enabled 返回已启用的源（内置 + 导入，按 order 排序）。
// 同 id 的导入源会覆盖内置清单，避免界面上出现两条同名源。
func (r *Registry) Enabled() []Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]Manifest, 0, len(r.manifests)+len(r.extra))
	for _, m := range r.manifests {
		if !m.Enabled {
			continue
		}
		if _, imported := r.dynamic[m.Key]; imported {
			continue
		}
		all = append(all, m)
	}
	all = append(all, r.extra...)
	sort.SliceStable(all, func(i, j int) bool {
		ri, rj := sourceRank(all[i].Key), sourceRank(all[j].Key)
		if ri != rj {
			return ri < rj
		}
		if all[i].Order != all[j].Order {
			return all[i].Order < all[j].Order
		}
		return all[i].Key < all[j].Key
	})
	return all
}

// HasBuiltin 判断是否有同 id 的原生实现（用于导入时的冲突检查）。
func (r *Registry) HasBuiltin(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.ctors[strings.ToLower(id)]
	return ok
}

// Get 按 id 取适配器（先内置，再导入）。
func (r *Registry) Get(id string) (Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := strings.ToLower(id)
	if ctor, ok := r.ctors[key]; ok {
		return ctor(), true
	}
	if src, ok := r.dynamic[key]; ok {
		return src, true
	}
	return nil, false
}

// Manifest 按 id 取清单项（导入优先，其次内置）。
func (r *Registry) Manifest(id string) (Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := strings.ToLower(id)
	for _, m := range r.extra {
		if m.Key == key {
			return m, true
		}
	}
	for _, m := range r.manifests {
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
