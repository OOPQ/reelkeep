package proc

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FindFFmpeg 按优先级找 ffmpeg：显式路径 → PATH → 程序目录 → 常见安装目录 → 旧项目配置。
// 显式路径不存在时继续自动查找，避免项目迁移后配置失效。
func FindFFmpeg(explicit string) string {
	if p := strings.TrimSpace(explicit); p != "" && isFile(p) {
		return p
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	for _, p := range candidates() {
		if isFile(p) {
			return p
		}
	}
	if p := legacyProjectFFmpeg(); p != "" {
		return p
	}
	return ""
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func candidates() []string {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	var out []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		out = append(out,
			filepath.Join(dir, name),
			filepath.Join(dir, "bin", name),
			filepath.Join(dir, "ffmpeg", name),
			filepath.Join(dir, "ffmpeg", "bin", name),
			filepath.Join(dir, "tools", name),
		)
	}
	for _, env := range []string{"LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)"} {
		if base := os.Getenv(env); base != "" {
			out = append(out,
				filepath.Join(base, "ffmpeg", "bin", name),
				filepath.Join(base, "ffmpeg", name),
				filepath.Join(base, "Programs", "ffmpeg", "bin", name),
			)
		}
	}
	if runtime.GOOS == "windows" {
		out = append(out,
			`C:\ffmpeg\bin\ffmpeg.exe`,
			`C:\ffmpeg\ffmpeg.exe`,
			`D:\ffmpeg\bin\ffmpeg.exe`,
		)
	}
	return out
}

// legacyProjectFFmpeg 从旧版/新版 projects.json 关联的 data/config.json 里找 ffmpegPath。
func legacyProjectFFmpeg() string {
	home, _ := os.UserConfigDir()
	if home == "" {
		return ""
	}
	var regs []string
	if runtime.GOOS == "windows" {
		regs = append(regs,
			filepath.Join(home, "pektino", "projects.json"),
			filepath.Join(home, "Reelkeep", "projects.json"),
		)
	} else {
		regs = append(regs, filepath.Join(home, "reelkeep", "projects.json"))
	}
	for _, regPath := range regs {
		b, err := os.ReadFile(regPath)
		if err != nil {
			continue
		}
		var reg struct {
			Last   string   `json:"last"`
			Recent []string `json:"recent"`
		}
		if json.Unmarshal(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}), &reg) != nil {
			continue
		}
		roots := append([]string{reg.Last}, reg.Recent...)
		for _, root := range roots {
			if strings.TrimSpace(root) == "" {
				continue
			}
			cfgPath := filepath.Join(root, "data", "config.json")
			cb, err := os.ReadFile(cfgPath)
			if err != nil {
				continue
			}
			var cfg struct {
				FFmpegPath string `json:"ffmpegPath"`
			}
			if json.Unmarshal(bytes.TrimPrefix(cb, []byte{0xEF, 0xBB, 0xBF}), &cfg) != nil {
				continue
			}
			if p := strings.TrimSpace(cfg.FFmpegPath); p != "" && isFile(p) {
				return p
			}
		}
	}
	return ""
}
