//go:build !windows

package proc

import (
	"os"
	"syscall"
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
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
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
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
