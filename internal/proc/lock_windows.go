//go:build windows

package proc

import (
	"os"

	"golang.org/x/sys/windows"
)

// FileLock 是跨进程的独占文件锁；进程退出（含崩溃）时由系统释放。
type FileLock struct {
	f *os.File
}

// LockFile 尝试对 path 加独占锁；已被占用时返回 ErrLocked。
func LockFile(path string) (*FileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	var ol windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &ol)
	if err != nil {
		_ = f.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &FileLock{f: f}, nil
}

// Release 解锁并关闭文件。
func (l *FileLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &ol)
	_ = l.f.Close()
	l.f = nil
}
