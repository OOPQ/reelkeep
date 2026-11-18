package boot

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/OOPQ/reelkeep/internal/config"
)

//go:embed assets/sources/*.json
var builtinSources embed.FS

// seedBuiltinSources 把随程序分发的内置源包写入用户源目录。
// 只在目标文件不存在时写入，避免覆盖用户自行修改/更新的源。
func seedBuiltinSources() {
	dir := config.ImportedSourcesDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	entries, err := fs.ReadDir(builtinSources, "assets/sources")
	if err != nil {
		return
	}
	// 已下线的内置源：xvideo 的解析能力已并入 pektino 源，避免同一个 X 链接被两个源抢。
	removeRetiredSource(dir, "x.json", `"id": "xvideo"`, `pektino.com/api/media`)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		target := filepath.Join(dir, e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		b, err := builtinSources.ReadFile("assets/sources/" + e.Name())
		if err != nil {
			continue
		}
		_ = os.WriteFile(target, b, 0o644)
	}
}

// removeRetiredSource 删除已并入其它源的内置源文件（只认内容特征，避免误删用户自己的源）。
func removeRetiredSource(dir, name string, markers ...string) {
	p := filepath.Join(dir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	text := string(b)
	for _, m := range markers {
		if !strings.Contains(text, m) {
			return
		}
	}
	_ = os.Remove(p)
}
