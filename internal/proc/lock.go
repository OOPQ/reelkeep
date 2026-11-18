package proc

import "errors"

// ErrLocked 表示锁已被另一个进程持有。
var ErrLocked = errors.New("lock held by another process")

// WriteData 通过持锁的句柄写入内容（用别的句柄写会被自己的锁挡住）。
func (l *FileLock) WriteData(s string) error {
	if l == nil || l.f == nil {
		return errors.New("lock released")
	}
	if err := l.f.Truncate(0); err != nil {
		return err
	}
	if _, err := l.f.Seek(0, 0); err != nil {
		return err
	}
	_, err := l.f.WriteString(s)
	return err
}
