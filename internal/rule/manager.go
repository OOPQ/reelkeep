package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OOPQ/reelkeep/internal/jsonx"
	"github.com/OOPQ/reelkeep/internal/secrets"
	"github.com/OOPQ/reelkeep/internal/source"
)

// ErrExists 表示同 id 的源已存在且未要求覆盖。
var ErrExists = errors.New("同 id 的源已存在")

const maxImportBytes = 256 << 10

// Item 是管理器里的一个源包。
type Item struct {
	Pkg         *Package
	Enabled     bool
	Path        string
	InstalledAt string
	LastRunAt   string
	LastRunOK   bool
	LastError   string
}

// ItemView 是给 UI 的快照。
type ItemView struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Version        string        `json:"version"`
	Author         string        `json:"author,omitempty"`
	Description    string        `json:"description,omitempty"`
	BaseURL        string        `json:"baseUrl"`
	Hosts          []string      `json:"hosts"`
	Capabilities   []string      `json:"capabilities"`
	Ranks          []source.Rank `json:"ranks"`
	Enabled        bool          `json:"enabled"`
	Origin         string        `json:"origin"`
	Path           string        `json:"path"`
	InstalledAt    string        `json:"installedAt,omitempty"`
	LastRunAt      string        `json:"lastRunAt,omitempty"`
	LastRunOK      bool          `json:"lastRunOk"`
	LastError      string        `json:"lastError,omitempty"`
	AuthType       string        `json:"authType,omitempty"`
	AuthConfigured bool          `json:"authConfigured"`
	AuthUpdatedAt  string        `json:"authUpdatedAt,omitempty"`
	LoginURL       string        `json:"loginUrl,omitempty"`
	CheckURL       string        `json:"checkUrl,omitempty"`
}

type indexFile struct {
	Items []indexItem `json:"items"`
}

type indexItem struct {
	ID          string `json:"id"`
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installedAt,omitempty"`
	LastRunAt   string `json:"lastRunAt,omitempty"`
	LastRunOK   bool   `json:"lastRunOk,omitempty"`
	LastError   string `json:"lastError,omitempty"`
}

// Manager 负责外部源包的安装、启用、删除和最近运行状态。
type Manager struct {
	dir     string
	reg     *source.Registry
	secrets *secrets.Store

	mu    sync.Mutex
	items map[string]*Item
	proxy string
}

// NewManager 创建管理器（不会自动加载）。
func NewManager(dir string, reg *source.Registry) *Manager {
	return &Manager{
		dir:     dir,
		reg:     reg,
		items:   map[string]*Item{},
		secrets: secrets.New(filepath.Join(filepath.Dir(dir), "secrets")),
	}
}

// Proxy 返回当前显式代理（供抓取器动态读取）。
func (m *Manager) Proxy() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.proxy
}

// SetProxy 更新显式代理；已创建的抓取器会在下一次请求时生效。
func (m *Manager) SetProxy(p string) {
	m.mu.Lock()
	m.proxy = strings.TrimSpace(p)
	m.mu.Unlock()
}

func (m *Manager) index() indexFile {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := indexFile{}
	ids := make([]string, 0, len(m.items))
	for id := range m.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		it := m.items[id]
		out.Items = append(out.Items, indexItem{
			ID: it.Pkg.ID, Enabled: it.Enabled, InstalledAt: it.InstalledAt,
			LastRunAt: it.LastRunAt, LastRunOK: it.LastRunOK, LastError: it.LastError,
		})
	}
	return out
}

func (m *Manager) saveIndex() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	idx := m.index()
	b, _ := json.MarshalIndent(idx, "", "  ")
	tmp := filepath.Join(m.dir, ".sources.index.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.dir, "sources.index.json"))
}

func (m *Manager) readIndex() map[string]indexItem {
	out := map[string]indexItem{}
	b, err := os.ReadFile(filepath.Join(m.dir, "sources.index.json"))
	if err != nil {
		return out
	}
	var idx indexFile
	if jsonx.Unmarshal(b, &idx) != nil {
		return out
	}
	for _, it := range idx.Items {
		out[strings.ToLower(it.ID)] = it
	}
	return out
}

func (m *Manager) fetcher(p *Package) (Fetcher, error) {
	f, err := NewHTTPFetcher(p, m.Proxy)
	if err != nil {
		return nil, err
	}
	f.SetCookieProvider(func() string { return m.cookie(p.ID) })
	return f, nil
}

// cookie 读取该源保存的 Cookie（没有就是空串）。
func (m *Manager) cookie(id string) string {
	rec, ok, err := m.secrets.Get(strings.ToLower(id))
	if err != nil || !ok {
		return ""
	}
	return rec.Cookie
}

// Cookie 对外暴露当前 Cookie（下载器注入用）。
func (m *Manager) Cookie(id string) string { return m.cookie(id) }

// LoadAll 加载目录下的全部源包，并按索引恢复启用状态。
func (m *Manager) LoadAll() []error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return []error{err}
	}
	idx := m.readIndex()
	pkgs, errs := LoadDir(m.dir)

	m.mu.Lock()
	m.items = map[string]*Item{}
	m.mu.Unlock()

	for _, p := range pkgs {
		st, ok := idx[strings.ToLower(p.ID)]
		enabled := true
		installedAt := time.Now().Format("2006-01-02 15:04:05")
		lastAt, lastOK, lastErr := "", false, ""
		if ok {
			enabled = st.Enabled
			if st.InstalledAt != "" {
				installedAt = st.InstalledAt
			}
			lastAt, lastOK, lastErr = st.LastRunAt, st.LastRunOK, st.LastError
		}
		it := &Item{
			Pkg: p, Enabled: enabled, Path: p.Path(), InstalledAt: installedAt,
			LastRunAt: lastAt, LastRunOK: lastOK, LastError: lastErr,
		}
		m.mu.Lock()
		m.items[strings.ToLower(p.ID)] = it
		m.mu.Unlock()
		if enabled {
			f, err := m.fetcher(p)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p.ID, err))
				continue
			}
			m.reg.Add(p.Manifest(), New(p, f))
		}
	}
	return errs
}

// List 返回全部已安装源（含禁用的）。
func (m *Manager) List() []ItemView {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.items))
	for id := range m.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ItemView, 0, len(ids))
	for _, id := range ids {
		it := m.items[id]
		p := it.Pkg
		ranks := p.List.Ranks
		if ranks == nil {
			ranks = []source.Rank{}
		}
		hosts := append([]string{}, p.Hosts...)
		authType, loginURL := "", ""
		if p.Auth != nil {
			authType = p.Auth.Type
			loginURL = absoluteURL(p.BaseURL, p.Auth.LoginURL)
		}
		checkURL := ""
		if p.Auth != nil && p.Auth.Check != nil {
			checkURL = absoluteURL(p.BaseURL, subst(p.Auth.Check.URL, map[string]string{"baseUrl": p.BaseURL}))
		}
		hasAuth, authAt := m.secrets.Info(p.ID)
		out = append(out, ItemView{
			ID: p.ID, Name: p.Name, Version: p.Version, Author: p.Author,
			Description: p.Description, BaseURL: p.BaseURL, Hosts: hosts,
			Capabilities: p.Capabilities, Ranks: ranks, Enabled: it.Enabled,
			Origin: "imported", Path: it.Path, InstalledAt: it.InstalledAt,
			LastRunAt: it.LastRunAt, LastRunOK: it.LastRunOK, LastError: it.LastError,
			AuthType: authType, AuthConfigured: hasAuth, AuthUpdatedAt: authAt,
			LoginURL: loginURL, CheckURL: checkURL,
		})
	}
	return out
}

// Package 按 id 取源包。
func (m *Manager) Package(id string) (*Package, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[strings.ToLower(id)]
	if !ok {
		return nil, false
	}
	return it.Pkg, true
}

// Import 校验并安装一个源包。同 id 存在时需 overwrite。
func (m *Manager) Import(data []byte, overwrite bool) (*Package, error) {
	if len(data) == 0 {
		return nil, errors.New("内容为空")
	}
	if len(data) > maxImportBytes {
		return nil, fmt.Errorf("源包超过 %d KB 上限", maxImportBytes/1024)
	}
	p, err := Parse(data, "")
	if err != nil {
		return nil, err
	}
	// 不允许覆盖已有原生实现的源（否则会被同名外部源静默替换）；
	// 只有配置清单、没有 Go 实现的 id 允许用外部源补位。
	if m.reg.HasBuiltin(p.ID) {
		return nil, fmt.Errorf("源 id %q 已有内置实现，请换一个 id", p.ID)
	}

	m.mu.Lock()
	prev, exists := m.items[strings.ToLower(p.ID)]
	enabled := true
	installedAt := time.Now().Format("2006-01-02 15:04:05")
	if exists {
		if !overwrite {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w：%s（如需升级请勾选覆盖）", ErrExists, p.ID)
		}
		enabled = prev.Enabled
		installedAt = prev.InstalledAt
	}
	m.mu.Unlock()

	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, err
	}
	dest := filepath.Join(m.dir, p.ID+".json")
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, jsonx.Clean(data), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, err
	}
	p.path = dest

	it := &Item{Pkg: p, Enabled: enabled, Path: dest, InstalledAt: installedAt}
	if exists {
		it.LastRunAt, it.LastRunOK, it.LastError = prev.LastRunAt, prev.LastRunOK, prev.LastError
	}
	m.mu.Lock()
	m.items[strings.ToLower(p.ID)] = it
	m.mu.Unlock()

	m.reg.Remove(p.ID)
	if enabled {
		f, err := m.fetcher(p)
		if err != nil {
			return nil, err
		}
		m.reg.Add(p.Manifest(), New(p, f))
	}
	if err := m.saveIndex(); err != nil {
		return nil, err
	}
	return p, nil
}

// SetEnabled 启用/禁用一个源。
func (m *Manager) SetEnabled(id string, on bool) error {
	key := strings.ToLower(id)
	m.mu.Lock()
	it, ok := m.items[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("没有这个源：%s", id)
	}
	it.Enabled = on
	pkg := it.Pkg
	m.mu.Unlock()

	m.reg.Remove(pkg.ID)
	if on {
		f, err := m.fetcher(pkg)
		if err != nil {
			return err
		}
		m.reg.Add(pkg.Manifest(), New(pkg, f))
	}
	return m.saveIndex()
}

// Remove 删除一个源（只删导入目录里的文件）。
func (m *Manager) Remove(id string) error {
	key := strings.ToLower(id)
	m.mu.Lock()
	it, ok := m.items[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("没有这个源：%s", id)
	}
	path := it.Path
	delete(m.items, key)
	m.mu.Unlock()

	m.reg.Remove(it.Pkg.ID)
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return m.saveIndex()
}

// Export 返回源包原始 JSON（用于备份/分享）。
func (m *Manager) Export(id string) ([]byte, error) {
	m.mu.Lock()
	it, ok := m.items[strings.ToLower(id)]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("没有这个源：%s", id)
	}
	b, err := os.ReadFile(it.Path)
	if err != nil {
		return nil, err
	}
	return jsonx.Clean(b), nil
}

// RecordResult 记录最近一次抓取结果（UI 用来显示成功率/错误）。
func (m *Manager) RecordResult(id string, runErr error) {
	key := strings.ToLower(id)
	m.mu.Lock()
	it, ok := m.items[key]
	if !ok {
		m.mu.Unlock()
		return
	}
	it.LastRunAt = time.Now().Format("2006-01-02 15:04:05")
	it.LastRunOK = runErr == nil
	if runErr != nil {
		it.LastError = runErr.Error()
	} else {
		it.LastError = ""
	}
	m.mu.Unlock()
	_ = m.saveIndex()
}

// NewFetcher 给上层（测试/预览）创建带登录态的抓取器。
func (m *Manager) NewFetcher(p *Package) (Fetcher, error) { return m.fetcher(p) }

// HasAuth 判断某个源是否已保存凭据。
func (m *Manager) HasAuth(id string) bool {
	has, _ := m.secrets.Info(strings.ToLower(id))
	return has
}

// SetAuth 保存一个源的登录 Cookie（加密落盘）。
func (m *Manager) SetAuth(id, cookie string) error {
	p, ok := m.Package(id)
	if !ok {
		return fmt.Errorf("没有这个源：%s", id)
	}
	if p.Auth == nil {
		return fmt.Errorf("源 %s 不需要登录", id)
	}
	return m.secrets.Save(p.ID, cookie)
}

// ClearAuth 清除源保存的 Cookie。
func (m *Manager) ClearAuth(id string) error {
	p, ok := m.Package(id)
	if !ok {
		return fmt.Errorf("没有这个源：%s", id)
	}
	return m.secrets.Delete(p.ID)
}

// TestAuth 用已保存的 Cookie 请求检测页，判断登录是否仍然有效。
func (m *Manager) TestAuth(ctx context.Context, id string) (bool, string, error) {
	p, ok := m.Package(id)
	if !ok || p.Auth == nil {
		return false, "", fmt.Errorf("源 %s 没有配置登录", id)
	}
	has, _ := m.secrets.Info(p.ID)
	if !has {
		return false, "还没有保存 Cookie", nil
	}
	if p.Auth.Check == nil {
		return true, "已保存 Cookie（该源没有配置登录检测地址）", nil
	}
	checkURL := absoluteURL(p.BaseURL, subst(p.Auth.Check.URL, map[string]string{"baseUrl": p.BaseURL}))
	f, err := m.fetcher(p)
	if err != nil {
		return false, "", err
	}
	body, err := f.Get(ctx, checkURL, p.Defaults.Headers)
	if err != nil {
		return false, "请求检测页失败: " + err.Error(), nil
	}
	s := string(body)
	if p.Auth.Check.LoggedOutContains != "" && strings.Contains(s, p.Auth.Check.LoggedOutContains) {
		return false, "未登录（页面出现未登录标记）", nil
	}
	if p.Auth.Check.LoggedInContains != "" {
		if strings.Contains(s, p.Auth.Check.LoggedInContains) {
			return true, "登录有效", nil
		}
		return false, "未检测到登录标记，Cookie 可能已过期", nil
	}
	return true, "检测页可访问（未配置登录标记）", nil
}
