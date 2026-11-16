// Package secrets 保存源登录凭据（目前只有 Cookie）。
//
// 原则：
//   - 凭据与源包分离：导出的源包永远不含 Cookie，方便分享；
//   - Windows 用 DPAPI 加密（CryptProtectData），其它平台退化为 0600 权限文件；
//   - 文件按 <source>.json 存放，接口只返回「是否已配置 / 更新时间」，不回传明文。
package secrets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxCookieBytes = 8192

// Record 是一个源保存的凭据。
type Record struct {
	Source    string
	Cookie    string
	UpdatedAt string
}

// Store 是凭据目录。
type Store struct {
	dir string
}

type fileFormat struct {
	V       int    `json:"v"`
	Source  string `json:"source"`
	Enc     string `json:"enc"`
	Updated string `json:"updated"`
	Data    string `json:"data"`
}

// New 创建凭据存储。
func New(dir string) *Store { return &Store{dir: dir} }

func (s *Store) path(source string) (string, error) {
	if !validSource(source) {
		return "", fmt.Errorf("源 id 不合法: %q", source)
	}
	return filepath.Join(s.dir, source+".json"), nil
}

func validSource(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

func sanitizeCookie(v string) string {
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return strings.TrimSpace(v)
}

// Save 加密保存一个源的 Cookie。
func (s *Store) Save(source, cookie string) error {
	cookie = sanitizeCookie(cookie)
	if cookie == "" {
		return fmt.Errorf("Cookie 不能为空")
	}
	if len(cookie) > maxCookieBytes {
		return fmt.Errorf("Cookie 超过 %d 字节", maxCookieBytes)
	}
	p, err := s.path(source)
	if err != nil {
		return err
	}
	cipher, enc, err := protect([]byte(cookie))
	if err != nil {
		return fmt.Errorf("加密失败: %w", err)
	}
	f := fileFormat{
		V: 1, Source: source, Enc: enc,
		Updated: time.Now().Format("2006-01-02 15:04:05"),
		Data:    base64.StdEncoding.EncodeToString(cipher),
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Get 读取并解密。
func (s *Store) Get(source string) (Record, bool, error) {
	var out Record
	p, err := s.path(source)
	if err != nil {
		return out, false, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return out, false, nil
		}
		return out, false, err
	}
	var f fileFormat
	if err := json.Unmarshal(b, &f); err != nil {
		return out, false, fmt.Errorf("凭据文件损坏: %w", err)
	}
	cipher, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil {
		return out, false, fmt.Errorf("凭据数据损坏: %w", err)
	}
	plain, err := unprotect(cipher, f.Enc)
	if err != nil {
		return out, false, fmt.Errorf("解密失败（可能换了 Windows 账户）: %w", err)
	}
	return Record{Source: source, Cookie: string(plain), UpdatedAt: f.Updated}, true, nil
}

// Delete 删除凭据。
func (s *Store) Delete(source string) error {
	p, err := s.path(source)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Info 返回是否已配置与更新时间。
func (s *Store) Info(source string) (bool, string) {
	rec, ok, err := s.Get(source)
	if err != nil || !ok {
		return false, ""
	}
	return true, rec.UpdatedAt
}
