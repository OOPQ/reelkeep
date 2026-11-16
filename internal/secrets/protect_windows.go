//go:build windows

package secrets

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	crypt32       = syscall.NewLazyDLL("crypt32.dll")
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procProtect   = crypt32.NewProc("CryptProtectData")
	procUnprot    = crypt32.NewProc("CryptUnprotectData")
	procLocalFree = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

// CRYPTPROTECT_UI_FORBIDDEN
const cryptProtectUIForbidden = 0x1

func protect(plain []byte) ([]byte, string, error) {
	if len(plain) == 0 {
		return nil, "", fmt.Errorf("空数据")
	}
	in := dataBlob{cbData: uint32(len(plain)), pbData: &plain[0]}
	var out dataBlob
	r, _, err := procProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, "", err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, out.cbData)
	cp := append([]byte(nil), buf...)
	return cp, "dpapi", nil
}

func unprotect(cipher []byte, enc string) ([]byte, error) {
	if enc != "dpapi" {
		return nil, fmt.Errorf("未知的加密类型: %s", enc)
	}
	if len(cipher) == 0 {
		return nil, fmt.Errorf("空数据")
	}
	in := dataBlob{cbData: uint32(len(cipher)), pbData: &cipher[0]}
	var out dataBlob
	r, _, err := procUnprot.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	buf := unsafe.Slice(out.pbData, out.cbData)
	return append([]byte(nil), buf...), nil
}
