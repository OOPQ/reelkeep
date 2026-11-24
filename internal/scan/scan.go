// Package scan 负责「媒体目录 → 台账」的扫盘回填。
//
// 语义与旧版（PowerShell）逐条对齐：
//   - 递归 4 层查找 *.mp4，文件名去掉 Windows 的 " (1)" 重名后缀
//   - token 与文件名（不含扩展名、小写）匹配
//   - 有文件：回填 file_* 并把状态标成 done（回收站除外）
//   - 文件消失：清空 file_*；原本是「自动标 done」的回退成 new
//   - 目录不可达：什么都不改（移动硬盘没挂载时不能把 done 全打回 new）
package scan

import (
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/OOPQ/reelkeep/internal/model"
)

var dupSuffix = regexp.MustCompile(`\s*\(\d+\)\s*$`)

// Entry 是媒体目录里的一个视频文件。
type Entry struct {
	Token      string `json:"token"`
	File       string `json:"file"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Date       string `json:"date"`
	SourceHint string `json:"sourceHint,omitempty"`
}

// Partial 是未完成的下载（.part / .xltd）。
type Partial struct {
	Name string
	Size int64
}

// Index 是一次扫盘的结果。
type Index struct {
	Dir     string
	Exists  bool
	Files   map[string]Entry // key: 小写 token
	Partial []Partial
}

// Build 扫描目录（PS 的 Get-DownloadIndex 等价物）。
func Build(dir string) Index {
	idx := Index{Dir: dir, Files: map[string]Entry{}}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return idx
	}
	idx.Exists = true

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 权限/瞬间消失的文件跳过，不让整次扫描失败
		}
		if d.IsDir() {
			if rel, e := filepath.Rel(dir, path); e == nil && rel != "." {
				// PS -Depth 4：目录层级超过 4 不再深入
				if strings.Count(rel, string(os.PathSeparator)) >= 4 {
					return filepath.SkipDir
				}
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".mp4" && ext != ".part" && ext != ".xltd" {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return nil
		}
		if ext != ".mp4" {
			idx.Partial = append(idx.Partial, Partial{Name: d.Name(), Size: info.Size()})
			return nil
		}
		stem := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		plain := dupSuffix.ReplaceAllString(stem, "")
		if plain == "" {
			return nil
		}
		hint := ""
		if rel, e := filepath.Rel(dir, path); e == nil {
			parts := strings.Split(rel, string(os.PathSeparator))
			if len(parts) > 1 {
				hint = strings.ToLower(parts[0]) // media/<source>/...，用目录名辅助识别来源
			}
		}
		key := strings.ToLower(plain)
		entry := Entry{
			Token:      plain,
			File:       path,
			Name:       d.Name(),
			Size:       info.Size(),
			Date:       info.ModTime().Format("2006-01-02 15:04:05"),
			SourceHint: hint,
		}
		if prev, ok := idx.Files[key]; ok {
			// 同时存在 "a.mp4" 和 "a (1).mp4" 时优先正名，别让副本抢走条目
			if dupSuffix.MatchString(strings.TrimSuffix(prev.Name, filepath.Ext(prev.Name))) && stem == plain {
				idx.Files[key] = entry
			}
			return nil
		}
		idx.Files[key] = entry
		return nil
	})
	return idx
}

// SourceKeys 返回一个台账行可能对应的文件名（小写，不含扩展名）：
// token、稳定 MediaUID、source_id、直链文件名。扫盘和导入共用，避免生成重复行。
func SourceKeys(r model.Row) []string {
	var out []string
	add := func(v string) {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			return
		}
		for _, x := range out {
			if x == v {
				return
			}
		}
		out = append(out, v)
	}
	add(r.Token)
	add(model.MediaUID(r.Source, r.SourceID, r.Token))
	add(r.SourceID)
	add(urlStem(r.URL))
	return out
}

func urlStem(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	stem := strings.ToLower(strings.TrimSuffix(base, path.Ext(base)))
	// 这些是 HLS/DASH/播放器常见的占位文件名，不能用来匹配台账。
	switch stem {
	case "index", "play", "video", "master", "media", "file", "download", "manifest":
		return ""
	}
	return stem
}

// Apply 把扫盘结果套到台账上，返回需要落库的脏行、命中数、孤立文件。
func (idx Index) Apply(rows []model.Row, keepStatus bool) (dirty []model.Row, found int, orphans []Entry) {
	if !idx.Exists {
		return nil, 0, nil
	}
	ledger := make(map[string]bool, len(rows)*4)
	sidCount := make(map[string]int, len(rows))
	urlCount := make(map[string]int, len(rows))
	for i := range rows {
		r := &rows[i]
		for _, k := range SourceKeys(*r) {
			ledger[k] = true
		}
		if sid := strings.ToLower(strings.TrimSpace(r.SourceID)); sid != "" {
			sidCount[sid]++
		}
		if uk := urlStem(r.URL); uk != "" {
			urlCount[uk]++
		}
	}

	used := make(map[string]bool, len(idx.Files))
	for i := range rows {
		r := &rows[i]
		tokenKey := strings.ToLower(r.Token)
		uidKey := strings.ToLower(model.MediaUID(r.Source, r.SourceID, r.Token))
		var f Entry
		var ok bool
		if f, ok = idx.Files[uidKey]; !ok {
			f, ok = idx.Files[tokenKey]
		}
		// 迅雷等外部工具常把文件存成 source_id 或链接文件名，不带 source@ 前缀。
		if !ok {
			if sid := strings.ToLower(strings.TrimSpace(r.SourceID)); sid != "" {
				if e, exists := idx.Files[sid]; exists {
					if sidCount[sid] <= 1 || (e.SourceHint != "" && e.SourceHint == strings.ToLower(r.Source)) {
						f, ok = e, true
					}
				}
			}
		}
		if !ok {
			if uk := urlStem(r.URL); uk != "" {
				if e, exists := idx.Files[uk]; exists {
					if urlCount[uk] <= 1 || (e.SourceHint != "" && e.SourceHint == strings.ToLower(r.Source)) {
						f, ok = e, true
					}
				}
			}
		}
		if ok && used[f.File] {
			continue // 同一文件已绑定给更明确的条目，不重复占用
		}
		if !ok {
			if r.FileName != "" {
				r.FileName, r.FilePath, r.FileSize, r.FileDate = "", "", "", ""
				dirty = append(dirty, *r)
			}
			if !keepStatus && r.Status == "done" && (r.Note == "auto:file" || r.Note == "scan:init") {
				r.Status = "new"
				r.Note = ""
				dirty = append(dirty, *r)
			}
			continue
		}
		used[f.File] = true
		changed := r.FilePath != f.File || r.FileName != f.Name || r.FileSize != itoa(f.Size) || r.FileDate != f.Date
		r.FilePath, r.FileName = f.File, f.Name
		r.FileSize, r.FileDate = itoa(f.Size), f.Date
		if changed {
			dirty = append(dirty, *r)
		}
		found++
		if r.Status != "done" && r.Status != "recycled" {
			r.Status = "done"
			r.Note = "auto:file"
			dirty = append(dirty, *r)
		}
	}
	for key, f := range idx.Files {
		if !ledger[key] {
			orphans = append(orphans, f)
		}
	}
	return dedupe(dirty), found, orphans
}

// dedupe 同一行可能被「文件字段变化」和「状态变化」各加一次。
// 必须保留最后一次：状态变化发生在文件字段之后，保留第一次会丢掉 done。
func dedupe(rows []model.Row) []model.Row {
	pos := make(map[string]int, len(rows))
	out := make([]model.Row, 0, len(rows))
	for _, r := range rows {
		if i, ok := pos[r.Token]; ok {
			out[i] = r
			continue
		}
		pos[r.Token] = len(out)
		out = append(out, r)
	}
	return out
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
