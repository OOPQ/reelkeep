// Package jsonx 读取旧版（PowerShell 5.1）写出的 JSON。
//
// PowerShell 的 Set-Content -Encoding UTF8 / ConvertTo-Json 会带 UTF-8 BOM，
// 而 encoding/json 不接受 BOM。所有读旧数据的入口都要先过这里。
package jsonx

import (
	"bytes"
	"encoding/json"
	"os"
)

var bom = []byte{0xEF, 0xBB, 0xBF}

// Clean 去掉 UTF-8 BOM。
func Clean(b []byte) []byte { return bytes.TrimPrefix(b, bom) }

// Unmarshal 是 json.Unmarshal 的 BOM 兼容版。
func Unmarshal(b []byte, v any) error { return json.Unmarshal(Clean(b), v) }

// ReadFile 读文件并去 BOM。
func ReadFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Clean(b), nil
}
