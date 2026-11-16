package rule

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/andybalholm/cascadia"
	"github.com/theory/jsonpath"
)

var (
	idPattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)
	versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	rankPattern    = regexp.MustCompile(`^[a-z0-9-]+$`)
	hostPattern    = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	jsonldPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(\.[A-Za-z][A-Za-z0-9]*|\[[0-9]+\])*$`)
	tplPattern     = regexp.MustCompile(`\{([A-Za-z0-9]+)\}`)
)

var allowedPlaceholders = map[string]bool{
	"baseUrl": true, "id": true, "rank": true, "page": true,
	"playable": true, "pageUrl": true, "url": true, "token": true, "sourceId": true,
}

// Validate 返回全部问题（而不是遇到第一个就退出），方便作者/LLM 一次改完。
func (p *Package) Validate() []error {
	var errs []error
	add := func(path, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...)))
	}

	if p.Schema != 1 {
		add("schema", "当前只支持 1")
	}
	if !idPattern.MatchString(p.ID) {
		add("id", "必须是 2–41 位小写字母/数字/连字符，且以字母开头：%q", p.ID)
	}
	if strings.TrimSpace(p.Name) == "" {
		add("name", "不能为空")
	}
	if !versionPattern.MatchString(p.Version) {
		add("version", "必须是 x.y.z")
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		add("baseUrl", "必须是 http(s) 绝对地址：%q", p.BaseURL)
	}
	for i, h := range p.Hosts {
		if !hostPattern.MatchString(strings.ToLower(h)) || strings.Contains(h, ":") || strings.Contains(h, "/") {
			add(fmt.Sprintf("hosts[%d]", i), "必须是纯域名（不含协议/端口/路径）：%q", h)
		}
	}
	caps := map[string]bool{}
	for i, c := range p.Capabilities {
		switch c {
		case "scrape", "detail", "cover", "download":
			caps[c] = true
		default:
			add(fmt.Sprintf("capabilities[%d]", i), "未知能力：%q", c)
		}
	}

	// list
	if strings.TrimSpace(p.List.URL) == "" {
		add("list.url", "不能为空")
	} else {
		checkTemplate(&errs, "list.url", p.List.URL)
	}
	if p.List.Item == "" && p.List.JSONPath == "" {
		add("list", "item 与 jsonPath 必须二选一")
	}
	if p.List.Item != "" && p.List.JSONPath != "" {
		add("list", "item 与 jsonPath 不能同时设置")
	}
	if p.List.JSONLD != "" && p.List.JSONPath == "" {
		add("list.jsonld", "使用 jsonld 时必须同时给 jsonPath（从该 JSON-LD 对象里取列表）")
	}
	if p.List.JSONLD != "" && p.List.Item != "" {
		add("list.jsonld", "jsonld 与 item 不能同时设置")
	}
	if p.List.PageSize < 1 || p.List.PageSize > 200 {
		add("list.pageSize", "必须在 1–200 之间")
	}
	seenRank := map[string]bool{}
	for i, r := range p.List.Ranks {
		if !rankPattern.MatchString(r.Key) {
			add(fmt.Sprintf("list.ranks[%d].key", i), "只能是小写字母/数字/连字符：%q", r.Key)
		}
		if seenRank[r.Key] {
			add(fmt.Sprintf("list.ranks[%d].key", i), "重复的榜单 key")
		}
		seenRank[r.Key] = true
	}
	mode := "html"
	if p.List.JSONPath != "" {
		mode = "json"
	}
	errs = append(errs, validateFields("list.fields", p.List.Fields, mode)...)
	if _, ok := p.List.Fields["id"]; !ok {
		add("list.fields.id", "必须提取 id（原生 id，用作 token 后缀）")
	}

	// detail
	if p.Detail != nil {
		if strings.TrimSpace(p.Detail.URL) == "" {
			add("detail.url", "不能为空")
		} else {
			checkTemplate(&errs, "detail.url", p.Detail.URL)
		}
		errs = append(errs, validateFields("detail.fields", p.Detail.Fields, "detail")...)
		if !caps["detail"] {
			add("capabilities", "提供了 detail 段就必须声明 detail 能力")
		}
	} else if caps["detail"] {
		add("detail", "声明了 detail 能力但没有 detail 段")
	}

	// download
	if p.Download != nil {
		if p.Download.Kind != "http" && p.Download.Kind != "hls" {
			add("download.kind", "只能是 http 或 hls")
		}
		if p.Download.URL != "" {
			checkTemplate(&errs, "download.url", p.Download.URL)
		}
		for k, v := range p.Download.Headers {
			checkTemplate(&errs, "download.headers."+k, v)
		}
		for i, h := range p.Download.ExcludeHeaderHosts {
			if !hostPattern.MatchString(strings.ToLower(h)) {
				add(fmt.Sprintf("download.excludeHeaderHosts[%d]", i), "必须是纯域名：%q", h)
			}
		}
		for status, r := range p.Download.OnStatus {
			if len(status) != 3 || status[0] < '1' || status[0] > '5' {
				add("download.onStatus", "状态码必须三位：%q", status)
			}
			if r.Action != "refreshDetail" {
				add("download.onStatus."+status+".action", "目前只支持 refreshDetail")
			}
		}
		for i, c := range p.Download.Candidates {
			if strings.TrimSpace(c.URL) == "" {
				add(fmt.Sprintf("download.candidates[%d].url", i), "不能为空")
			} else {
				checkTemplate(&errs, fmt.Sprintf("download.candidates[%d].url", i), c.URL)
			}
		}
		if !caps["download"] {
			add("capabilities", "提供了 download 段就必须声明 download 能力")
		}
	} else if caps["download"] {
		add("download", "声明了 download 能力但没有 download 段")
	}

	// auth
	if p.Auth != nil {
		if p.Auth.Type != "cookie" {
			add("auth.type", "目前只支持 cookie")
		}
		if p.Auth.LoginURL != "" {
			if u, err := url.Parse(p.Auth.LoginURL); err != nil || (u.Host == "" && !strings.HasPrefix(p.Auth.LoginURL, "/")) {
				add("auth.loginUrl", "必须是绝对地址或以 / 开头的相对地址：%q", p.Auth.LoginURL)
			}
		}
		for i, d := range p.Auth.Domains {
			if !hostPattern.MatchString(strings.ToLower(d)) {
				add(fmt.Sprintf("auth.domains[%d]", i), "必须是纯域名：%q", d)
			}
		}
		if p.Auth.Check != nil {
			if strings.TrimSpace(p.Auth.Check.URL) == "" {
				add("auth.check.url", "不能为空")
			} else {
				checkTemplate(&errs, "auth.check.url", p.Auth.Check.URL)
			}
		}
	}

	return errs
}

func validateFields(prefix string, fields map[string]Field, mode string) []error {
	var errs []error
	add := func(path, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...)))
	}
	if len(fields) == 0 {
		add(prefix, "至少定义一个字段")
		return errs
	}
	for name, f := range fields {
		path := prefix + "." + name
		src := 0
		if f.Selector != "" {
			src++
		}
		if f.JSONPath != "" {
			src++
		}
		if f.JSONLD != "" {
			src++
		}
		if f.Raw {
			src++
		}
		if src != 1 {
			add(path, "selector / jsonPath / jsonld / raw 必须且只能有一个")
			continue
		}
		switch {
		case f.Selector != "":
			if mode == "json" {
				add(path, "JSON 列表里不能用 selector")
			}
			if _, err := cascadia.Parse(f.Selector); err != nil {
				add(path, "CSS 选择器不合法：%v", err)
			}
			if f.Attr != "" && f.Text {
				add(path, "attr 与 text 不能同时设置")
			}
		case f.JSONPath != "":
			if mode == "html" || mode == "detail" {
				add(path, "HTML 页面里不能用 jsonPath（详情页可用 jsonld）")
			}
			if _, err := jsonpath.Parse(f.JSONPath); err != nil {
				add(path, "JSONPath 不合法（RFC 9535）：%v", err)
			}
		case f.JSONLD != "":
			if !jsonldPattern.MatchString(f.JSONLD) {
				add(path, "jsonld 路径不合法，例如 VideoObject.duration 或 VideoObject.interactionStatistic[0].userInteractionCount")
			}
			if f.Attr != "" || f.Text || f.HTML {
				add(path, "jsonld 不能和 attr/text/html 组合")
			}
		case f.Raw:
			if mode == "json" {
				add(path, "JSON 列表里不能用 raw")
			}
			if f.Regex == "" {
				add(path, "raw 必须配合 regex（对整页 HTML 做匹配）")
			}
			if f.Attr != "" || f.Text || f.HTML || f.Selector != "" {
				add(path, "raw 不能和 selector/attr/text/html 组合")
			}
		}
		if f.Attr != "" && f.Selector == "" {
			add(path, "attr 只能和 selector 一起用")
		}
		if f.HTML && f.Attr != "" {
			add(path, "html 与 attr 不能同时设置")
		}
		if f.Regex != "" {
			if _, err := regexp.Compile(f.Regex); err != nil {
				add(path, "regex 不合法：%v", err)
			}
		}
	}
	return errs
}

func checkTemplate(errs *[]error, path, tpl string) {
	for _, m := range tplPattern.FindAllStringSubmatch(tpl, -1) {
		if !allowedPlaceholders[m[1]] {
			*errs = append(*errs, fmt.Errorf("%s: 未知占位符 {%s}", path, m[1]))
		}
	}
}
