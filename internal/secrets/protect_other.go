//go:build !windows

package secrets

import "fmt"

// 非 Windows 平台没有 DPAPI：退化为仅靠文件权限保护（0600）。
// 发布 macOS/Linux 版本时应改为系统钥匙串。
func protect(plain []byte) ([]byte, string, error) {
	return plain, "plain", nil
}

func unprotect(cipher []byte, enc string) ([]byte, error) {
	if enc != "plain" {
		return nil, fmt.Errorf("未知的加密类型: %s", enc)
	}
	return cipher, nil
}
