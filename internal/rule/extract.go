package rule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/theory/jsonpath"
)

// parseHTML 把响应字节变成可查询的文档。
func parseHTML(b []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(bytes.NewReader(b))
}

// extractHTMLFields 在 scope（列表项）或整篇文档里提取字段。
func extractHTMLFields(doc *goquery.Document, scope *goquery.Selection, fields map[string]Field, pageURL *url.URL) (map[string]string, error) {
	out := make(map[string]string, len(fields))
	for name, f := range fields {
		if f.Selector == "" {
			return nil, fmt.Errorf("字段 %s: HTML 页面必须用 selector", name)
		}
		var node *goquery.Selection
		if scope != nil {
			node = scope.Find(f.Selector).First()
			if node.Length() == 0 && scope.Is(f.Selector) {
				node = scope
			}
		} else {
			node = doc.Find(f.Selector).First()
		}
		var v string
		if node.Length() > 0 {
			switch {
			case f.Attr != "":
				v, _ = node.Attr(f.Attr)
			case f.HTML:
				v, _ = node.Html()
			default:
				v = node.Text()
			}
		}
		out[name] = postProcess(v, f, pageURL)
	}
	return out, nil
}

// extractJSONFields 在 JSON 对象（列表项或接口响应）上按 RFC 9535 取值。
func extractJSONFields(root any, fields map[string]Field) (map[string]string, error) {
	out := make(map[string]string, len(fields))
	for name, f := range fields {
		if f.JSONPath == "" {
			return nil, fmt.Errorf("字段 %s: JSON 页面必须用 jsonPath", name)
		}
		p, err := jsonpath.Parse(f.JSONPath)
		if err != nil {
			return nil, fmt.Errorf("字段 %s: %w", name, err)
		}
		nodes := p.Select(root)
		if len(nodes) == 0 {
			out[name] = f.Default
			continue
		}
		v, err := scalarString(nodes[0])
		if err != nil {
			return nil, fmt.Errorf("字段 %s: %w", name, err)
		}
		out[name] = postProcess(v, f, nil)
	}
	return out, nil
}

// extractDetailFields 提取详情页字段：selector 与 schema.org JSON-LD 可混用。
func extractDetailFields(doc *goquery.Document, fields map[string]Field, pageURL *url.URL) (map[string]string, error) {
	out := make(map[string]string, len(fields))
	for name, f := range fields {
		switch {
		case f.JSONLD != "":
			v, err := extractJSONLD(doc, f.JSONLD)
			if err != nil {
				return nil, fmt.Errorf("字段 %s: %w", name, err)
			}
			out[name] = postProcess(v, f, pageURL)
		case f.Selector != "":
			node := doc.Find(f.Selector).First()
			var v string
			if node.Length() > 0 {
				switch {
				case f.Attr != "":
					v, _ = node.Attr(f.Attr)
				case f.HTML:
					v, _ = node.Html()
				default:
					v = node.Text()
				}
			}
			out[name] = postProcess(v, f, pageURL)
		default:
			return nil, fmt.Errorf("字段 %s: 详情页需要 selector 或 jsonld", name)
		}
	}
	return out, nil
}

// extractJSONLD 从 schema.org JSON-LD 里取字段，例如 VideoObject.duration。
func extractJSONLD(doc *goquery.Document, expr string) (string, error) {
	objs := collectJSONLD(doc)
	seg, rest := splitFirst(expr)
	typ, idx := seg, -1
	if m := regexp.MustCompile(`^([A-Za-z0-9]+)(?:\[(\d+)\])?$`).FindStringSubmatch(seg); m != nil {
		typ = m[1]
		if m[2] != "" {
			idx, _ = strconv.Atoi(m[2])
		}
	}
	var matches []any
	for _, o := range objs {
		if hasType(o, typ) {
			matches = append(matches, o)
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	i := 0
	if idx >= 0 {
		if idx >= len(matches) {
			return "", nil
		}
		i = idx
	}
	v, ok := lookupPath(matches[i], rest)
	if !ok {
		return "", nil
	}
	s, err := scalarString(v)
	if err != nil {
		return "", nil
	}
	return s, nil
}

func collectJSONLD(doc *goquery.Document) []any {
	var out []any
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return
		}
		var v any
		if json.Unmarshal([]byte(raw), &v) != nil {
			return
		}
		out = append(out, flattenJSONLD(v)...)
	})
	return out
}

func flattenJSONLD(v any) []any {
	switch x := v.(type) {
	case []any:
		var out []any
		for _, item := range x {
			out = append(out, flattenJSONLD(item)...)
		}
		return out
	case map[string]any:
		if g, ok := x["@graph"]; ok {
			return flattenJSONLD(g)
		}
		return []any{x}
	default:
		return nil
	}
}

func hasType(v any, typ string) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	switch t := m["@type"].(type) {
	case string:
		return t == typ
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && s == typ {
				return true
			}
		}
	}
	return false
}

// lookupPath 解析 "interactionStatistic[0].userInteractionCount" 这类路径。
func lookupPath(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	for _, seg := range strings.Split(path, ".") {
		name, idx := seg, -1
		if i := strings.Index(seg, "["); i >= 0 {
			name = seg[:i]
			if j := strings.Index(seg, "]"); j > i {
				idx, _ = strconv.Atoi(seg[i+1 : j])
			}
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		nv, ok := m[name]
		if !ok {
			return nil, false
		}
		if idx >= 0 {
			arr, ok := nv.([]any)
			if !ok || idx >= len(arr) {
				return nil, false
			}
			nv = arr[idx]
		}
		v = nv
	}
	return v, true
}

func splitFirst(expr string) (string, string) {
	if i := strings.Index(expr, "."); i >= 0 {
		return expr[:i], expr[i+1:]
	}
	return expr, ""
}

func scalarString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(x), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("不是标量（%T）", v)
	}
}

func postProcess(v string, f Field, pageURL *url.URL) string {
	if f.Regex != "" {
		if re, err := regexp.Compile(f.Regex); err == nil {
			if m := re.FindStringSubmatch(v); len(m) > 1 {
				v = m[1]
			} else if len(m) == 1 {
				v = m[0]
			} else {
				v = ""
			}
		}
	}
	trim := true
	if f.Trim != nil {
		trim = *f.Trim
	}
	if trim {
		v = strings.TrimSpace(v)
	}
	if f.Absolute && v != "" {
		v = resolveURL(pageURL, v)
	}
	if v == "" {
		v = f.Default
	}
	return v
}

func resolveURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "//") && base != nil {
		ref = base.Scheme + ":" + ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if base != nil {
		return base.ResolveReference(u).String()
	}
	return u.String()
}

// subst 替换 {baseUrl} {id} {rank} {page} 等占位符。
func subst(tpl string, vars map[string]string) string {
	return tplPattern.ReplaceAllStringFunc(tpl, func(m string) string {
		key := m[1 : len(m)-1]
		if v, ok := vars[key]; ok {
			return v
		}
		return m
	})
}

// toSeconds 把 ISO 8601 / 1:02:33 / 6:20 转成秒，并给出规范时长文本。
func toSeconds(v string) (int, string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, ""
	}
	if m := regexp.MustCompile(`^P(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`).FindStringSubmatch(v); m != nil {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		sec, _ := strconv.ParseFloat(m[3], 64)
		total := h*3600 + min*60 + int(sec)
		return total, formatDuration(total)
	}
	if m := regexp.MustCompile(`^(\d+):(\d+):(\d+(?:\.\d+)?)$`).FindStringSubmatch(v); m != nil {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		sec, _ := strconv.ParseFloat(m[3], 64)
		total := h*3600 + min*60 + int(sec)
		return total, formatDuration(total)
	}
	if m := regexp.MustCompile(`^(\d+):(\d+(?:\.\d+)?)$`).FindStringSubmatch(v); m != nil {
		min, _ := strconv.Atoi(m[1])
		sec, _ := strconv.ParseFloat(m[2], 64)
		total := min*60 + int(sec)
		return total, formatDuration(total)
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return n, formatDuration(n)
	}
	return 0, v
}

func formatDuration(total int) string {
	if total <= 0 {
		return ""
	}
	if total >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", total/3600, total%3600/60, total%60)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}
