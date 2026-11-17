package boot

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

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
