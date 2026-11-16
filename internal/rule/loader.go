package rule

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/OOPQ/reelkeep/internal/jsonx"
	"github.com/OOPQ/reelkeep/internal/source"
)

// LoadFile 读取并校验一个源包。未知字段直接报错（防止拼写错误被静默忽略）。
func LoadFile(path string) (*Package, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Package
	dec := json.NewDecoder(bytes.NewReader(jsonx.Clean(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	p.path = path
	p.normalize()
	if errs := p.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("%s:\n%w", filepath.Base(path), errors.Join(errs...))
	}
	return &p, nil
}

// LoadDir 读取目录下所有 *.json 源包；坏文件不影响其它文件。
func LoadDir(dir string) ([]*Package, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var out []*Package
	var errs []error
	for _, name := range names {
		p, err := LoadFile(filepath.Join(dir, name))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	return out, errs
}

func (p *Package) normalize() {
	p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	for i := range p.Hosts {
		p.Hosts[i] = strings.ToLower(strings.TrimSpace(p.Hosts[i]))
	}
	if p.Defaults.TimeoutSec <= 0 {
		p.Defaults.TimeoutSec = 15
	}
	if p.Defaults.MaxBytes <= 0 {
		p.Defaults.MaxBytes = 8 << 20
	}
	if p.List.PageStart <= 0 {
		p.List.PageStart = 1
	}
	if p.List.PageSize <= 0 {
		p.List.PageSize = 24
	}
	if p.List.Ranks == nil {
		p.List.Ranks = []source.Rank{}
	}
}
