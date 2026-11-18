// Package rule 实现「外部源包」：用声明式 JSON 描述一个站点的列表/详情/下载地址，
// 由解释器在执行期取值。它实现 source.Source 及其可选能力接口，
// 因此 API 层、下载器、前端都不需要知道「这个源是内置的还是导入的」。
package rule

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/OOPQ/reelkeep/internal/source"
)

// Package 是一个源包（source package），对应 docs/schema/source.schema.json。
type Package struct {
	Schema       int       `json:"schema"`
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	Author       string    `json:"author,omitempty"`
	Description  string    `json:"description,omitempty"`
	BaseURL      string    `json:"baseUrl"`
	Hosts        []string  `json:"hosts,omitempty"`
	// LinkMatch 是链接抓取时的归类依据（对 URL 路径做正则匹配），用来区分同一站点下的多个源。
	LinkMatch []string `json:"linkMatch,omitempty"`
	// LinkHosts 是额外的链接归类域名（如 pektino 源同时处理 X/Twitter 链接）。
	LinkHosts    []string  `json:"linkHosts,omitempty"`
	Capabilities []string  `json:"capabilities"`
	Defaults     Defaults  `json:"defaults"`
	List         List      `json:"list"`
	Playlist     *Playlist `json:"playlist,omitempty"`
	Detail       *Detail   `json:"detail,omitempty"`
	Resolve      *Resolve  `json:"resolve,omitempty"`
	Download     *Download `json:"download,omitempty"`
	Auth         *Auth     `json:"auth,omitempty"`

	path string
}

// Resolve 声明「把链接交给第三方解析接口」的抓取方式（支持 POST），
// 返回的 JSON 用 fields 里的 jsonPath 取值（字段名同列表：title/cover/playable）。
type Resolve struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	// OnlyHosts 限定哪些域名的链接才走解析接口（留空表示所有链接都走）。
	OnlyHosts []string         `json:"onlyHosts,omitempty"`
	Fields    map[string]Field `json:"fields"`
}

// Auth 声明源需要登录（目前只支持 Cookie）。
type Auth struct {
	Type     string     `json:"type"` // cookie
	LoginURL string     `json:"loginUrl,omitempty"`
	Domains  []string   `json:"domains,omitempty"`
	Check    *AuthCheck `json:"check,omitempty"`
}

// AuthCheck 描述如何判断登录是否有效。
type AuthCheck struct {
	URL               string `json:"url"`
	LoggedInContains  string `json:"loggedInContains,omitempty"`
	LoggedOutContains string `json:"loggedOutContains,omitempty"`
}

// Defaults 是请求默认值。
type Defaults struct {
	Headers    map[string]string `json:"headers,omitempty"`
	UserAgent  string            `json:"userAgent,omitempty"`
	TimeoutSec int               `json:"timeoutSec,omitempty"`
	MaxBytes   int64             `json:"maxBytes,omitempty"`
	// Proxy: none（默认，直连）| system（跟随 HTTP(S)_PROXY 环境变量）。
	// 设置里的显式代理由调用方注入，优先级更高。
	Proxy string `json:"proxy,omitempty"`
	// Render: 留空用普通 HTTP；browser 用本机 Chrome/Edge 无头渲染后再取 DOM。
	Render string `json:"render,omitempty"`
	// TLSProfile: 留空用 Go 默认 TLS；chrome/firefox/safari 用对应指纹发请求。
	TLSProfile string `json:"tlsProfile,omitempty"`
	// DetailWorkers: 详情页并发数（默认 4，上限 16）。
	DetailWorkers int `json:"detailWorkers,omitempty"`
	// RateLimitMs: 同一域名两次请求的最小间隔（默认 200ms，最小 20ms）。
	RateLimitMs int `json:"rateLimitMs,omitempty"`
}

// List 描述列表页（HTML 或 JSON API）。
type List struct {
	URL       string           `json:"url"`
	PageStart int              `json:"pageStart,omitempty"`
	PageSize  int              `json:"pageSize,omitempty"`
	Ranks     []source.Rank    `json:"ranks,omitempty"`
	Item      string           `json:"item,omitempty"`
	JSONPath  string           `json:"jsonPath,omitempty"`
	JSONLD    string           `json:"jsonld,omitempty"`
	Fields    map[string]Field `json:"fields"`
}

// Detail 描述详情页。
type Detail struct {
	URL    string           `json:"url"`
	Always bool             `json:"always,omitempty"`
	Fields map[string]Field `json:"fields"`
}

// Playlist 描述播放清单页；链接抓取命中 /playlist 时批量导入。
type Playlist struct {
	Item   string           `json:"item"`
	Name   Field            `json:"name,omitempty"`
	Fields map[string]Field `json:"fields"`
}

// Download 描述如何拿到可下载地址。
type Download struct {
	Kind               string               `json:"kind"`
	URL                string               `json:"url,omitempty"`
	Headers            map[string]string    `json:"headers,omitempty"`
	ExcludeHeaderHosts []string             `json:"excludeHeaderHosts,omitempty"`
	Candidates         []Candidate          `json:"candidates,omitempty"`
	OnStatus           map[string]RetryRule `json:"onStatus,omitempty"`
}

// Candidate 是候选下载页/地址（预留，v1 只实现模板形式）。
type Candidate struct {
	URL    string `json:"url"`
	Prefer bool   `json:"prefer,omitempty"`
}

// RetryRule 是下载失败后的声明式修复动作。
type RetryRule struct {
	Action    string `json:"action"`
	RetryOnce bool   `json:"retryOnce,omitempty"`
}

// Field 是一个字段取值器：selector / jsonPath / jsonld / raw 四选一。
type Field struct {
	Selector string `json:"selector,omitempty"`
	JSONPath string `json:"jsonPath,omitempty"`
	JSONLD   string `json:"jsonld,omitempty"`
	Raw      bool   `json:"raw,omitempty"`
	Attr     string `json:"attr,omitempty"`
	Text     bool   `json:"text,omitempty"`
	HTML     bool   `json:"html,omitempty"`
	Regex    string `json:"regex,omitempty"`
	Absolute bool   `json:"absolute,omitempty"`
	Trim     *bool  `json:"trim,omitempty"`
	Default  string `json:"default,omitempty"`
}

// Path 返回源包来自哪个文件（内置为空）。
func (p *Package) Path() string { return p.path }

// Origin 区分内置与导入。
func (p *Package) Origin() string {
	if p.path == "" {
		return "builtin"
	}
	return "imported"
}

// Has 判断能力。
func (p *Package) Has(cap string) bool {
	for _, c := range p.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// Manifest 把源包映射成前端的 sources 项。
func (p *Package) Manifest() source.Manifest {
	ranks := p.List.Ranks
	if ranks == nil {
		ranks = []source.Rank{}
	}
	pageSize := p.List.PageSize
	if pageSize <= 0 {
		pageSize = 24
	}
	authType := ""
	if p.Auth != nil {
		authType = p.Auth.Type
	}
	return source.Manifest{
		Key:           p.ID,
		Name:          p.Name,
		Enabled:       true,
		Order:         900,
		Origin:        p.Origin(),
		AuthType:      authType,
		Capabilities:  p.Capabilities,
		SiteURL:       p.BaseURL,
		PageSize:      pageSize,
		SupportsPages: true,
		Incremental:   false,
		Ranks:         ranks,
		LinkMatch:     p.LinkMatch,
		LinkHosts:     p.LinkHosts,
	}
}

// firstRank 返回默认榜单。
func (p *Package) firstRank() string {
	if len(p.List.Ranks) > 0 {
		return p.List.Ranks[0].Key
	}
	return "default"
}

// ListURL 拼出列表页地址（CLI / 测试 / fixture 用）。
func (p *Package) ListURL(rank string, page int) string {
	if rank == "" {
		rank = p.firstRank()
	}
	tmpl := p.List.URL
	for _, r := range p.List.Ranks {
		if r.Key == rank && r.Path != "" {
			tmpl = r.Path
			break
		}
	}
	return absoluteURL(p.BaseURL, subst(tmpl, map[string]string{
		"baseUrl": p.BaseURL,
		"rank":    rank,
		"page":    strconv.Itoa(page),
	}))
}

// DetailURL 拼出详情页地址。
func (p *Package) DetailURL(id string) string {
	return p.DetailURLFor(id, "")
}

// DetailURLFor 拼出详情页地址；模板可用 {pageUrl}（列表里给出的页面地址）。
func (p *Package) DetailURLFor(id, pageURL string) string {
	if p.Detail == nil {
		return ""
	}
	return absoluteURL(p.BaseURL, subst(p.Detail.URL, map[string]string{
		"baseUrl": p.BaseURL,
		"id":      id,
		"pageUrl": pageURL,
	}))
}

// absoluteURL 把相对模板解析成绝对地址（模板允许写 /list/{page}）。
func absoluteURL(base, ref string) string {
	bu, err := url.Parse(base)
	if err != nil {
		return ref
	}
	ru, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return bu.ResolveReference(ru).String()
}

// rankKeys 用于校验请求的榜单。
func (p *Package) rankKeys() []string {
	out := make([]string, 0, len(p.List.Ranks))
	for _, r := range p.List.Ranks {
		out = append(out, r.Key)
	}
	if len(out) == 0 {
		out = []string{"default"}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}
