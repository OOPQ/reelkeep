package download

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/source"
)

// runHTTP 下载普通 http(s) 地址，支持断点续传。
func (m *Manager) runHTTP(ctx context.Context, t *Task) error {
	final, part := m.pathsFor(t)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	if st, err := os.Stat(final); err == nil && st.Size() > 0 {
		return nil // 文件已经存在
	}

	offset := int64(0)
	if st, err := os.Stat(part); err == nil {
		offset = st.Size()
	}
	m.mu.Lock()
	t.lastAt = time.Now()
	t.lastBytes = offset
	m.mu.Unlock()

	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.M.URL, nil)
		if err != nil {
			return err
		}
		m.applyHeaders(req, t)
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		resp, err := m.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < 2 {
				sleepCtx(ctx, time.Duration(attempt+1)*time.Second)
				continue
			}
			return err
		}

		switch code := resp.StatusCode; {
		case code == http.StatusRequestedRangeNotSatisfiable:
			_ = resp.Body.Close()
			_ = os.Remove(part)
			offset = 0
			continue
		case code == http.StatusGone:
			_ = resp.Body.Close()
			if t.M.RefreshOn410 {
				if err := m.refresh(ctx, t); err != nil {
					return fmt.Errorf("地址已失效（410），刷新失败: %w", err)
				}
				_ = os.Remove(part)
				offset = 0
				continue
			}
			return errors.New("HTTP 410：地址已失效")
		case code == http.StatusForbidden:
			_ = resp.Body.Close()
			return errors.New("HTTP 403：源站拒绝（可能需要登录或 Referer）")
		case code >= 500:
			_ = resp.Body.Close()
			if attempt < 2 {
				sleepCtx(ctx, time.Duration(attempt+1)*time.Second)
				continue
			}
			return fmt.Errorf("HTTP %d", code)
		case code != http.StatusOK && code != http.StatusPartialContent:
			_ = resp.Body.Close()
			return fmt.Errorf("HTTP %d", code)
		}

		flags := os.O_CREATE | os.O_WRONLY
		if resp.StatusCode == http.StatusOK {
			offset = 0
			flags |= os.O_TRUNC
		} else {
			flags |= os.O_APPEND
		}
		f, err := os.OpenFile(part, flags, 0o644)
		if err != nil {
			_ = resp.Body.Close()
			return err
		}
		return m.copyBody(ctx, t, resp, f, offset, part, final)
	}
	return errors.New("重试次数用尽")
}

func (m *Manager) copyBody(ctx context.Context, t *Task, resp *http.Response, f *os.File, offset int64, part, final string) error {
	defer func() { _ = resp.Body.Close() }()
	if resp.ContentLength > 0 {
		m.mu.Lock()
		t.M.Total = offset + resp.ContentLength
		m.mu.Unlock()
	}
	buf := make([]byte, 128*1024)
	written := offset
	lastSave := time.Now()
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				_ = f.Close()
				return werr
			}
			written += int64(n)
			m.updateProgress(t, written)
			if time.Since(lastSave) > time.Second {
				_ = f.Sync()
				m.save(t)
				lastSave = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = f.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	m.mu.Lock()
	total := t.M.Total
	t.M.Bytes = written
	m.mu.Unlock()
	if total > 0 && written < total {
		return fmt.Errorf("下载不完整：%d/%d 字节", written, total)
	}
	m.mu.Lock()
	t.M.Total = written
	m.mu.Unlock()
	if err := os.Rename(part, final); err != nil {
		return err
	}
	return nil
}

// runHLS 用 ffmpeg 下载 HLS；410 时先刷新详情再重试一次。
func (m *Manager) runHLS(ctx context.Context, t *Task) error {
	final, part := m.pathsFor(t)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	ff := strings.TrimSpace(m.getSettings().FFmpeg)
	if ff == "" {
		ff, _ = exec.LookPath("ffmpeg")
	}
	if ff == "" {
		return errors.New("HLS 下载需要 ffmpeg：请在「设置 → FFmpeg 路径」里填写，或加入 PATH")
	}
	err := m.hlsOnce(ctx, t, ff, part)
	if err != nil && ctx.Err() == nil && t.M.RefreshOn410 {
		if rerr := m.refresh(ctx, t); rerr == nil {
			err = m.hlsOnce(ctx, t, ff, part)
		}
	}
	if err != nil {
		return err
	}
	st, err := os.Stat(part)
	if err != nil || st.Size() == 0 {
		return errors.New("ffmpeg 没有产出有效文件")
	}
	if err := os.Rename(part, final); err != nil {
		return err
	}
	m.mu.Lock()
	t.M.Bytes = st.Size()
	t.M.Total = st.Size()
	m.mu.Unlock()
	return nil
}

func (m *Manager) hlsOnce(ctx context.Context, t *Task, ff, part string) error {
	headers := parseHeaders(t.M.Headers)
	if t.M.UseAuth && m.cookie != nil {
		if c := m.cookie(t.M.Source); c != "" && headers["Cookie"] == "" {
			headers["Cookie"] = c
		}
	}
	var hdr strings.Builder
	for k, v := range headers {
		hdr.WriteString(k + ": " + v + "\r\n")
	}
	args := []string{"-y", "-nostdin", "-loglevel", "error"}
	if hdr.Len() > 0 {
		args = append(args, "-headers", hdr.String())
	}
	args = append(args, "-i", t.M.URL, "-c", "copy", "-bsf:a", "aac_adtstoasc",
		"-movflags", "+faststart", "-f", "mp4", part)

	cmd := exec.CommandContext(ctx, ff, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		last, lastAt := int64(0), time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if st, err := os.Stat(part); err == nil {
					m.mu.Lock()
					t.M.Bytes = st.Size()
					if d := time.Since(lastAt); d >= 500*time.Millisecond {
						t.speed = int64(float64(st.Size()-last) / d.Seconds())
						last, lastAt = st.Size(), time.Now()
					}
					m.mu.Unlock()
				}
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		return fmt.Errorf("ffmpeg 失败: %s", tail)
	}
	return nil
}

// refresh 通过源重新解析这条记录（qise 的签名地址过期）。
func (m *Manager) refresh(ctx context.Context, t *Task) error {
	row, ok, err := m.st.GetRow(t.M.Token)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("台账里没有条目 %s", t.M.Token)
	}
	src, ok := m.reg.Get(t.M.Source)
	if !ok {
		return fmt.Errorf("源 %s 未启用", t.M.Source)
	}
	if r, ok := src.(source.Refresher); ok {
		nr, err := r.Refresh(ctx, row)
		if err != nil {
			return err
		}
		row = nr
	}
	if dr, ok := src.(source.DownloadResolver); ok {
		spec, err := dr.Resolve(ctx, row)
		if err != nil {
			return err
		}
		m.mu.Lock()
		t.M.URL = spec.URL
		t.M.Mode = spec.Mode
		t.M.RefreshOn410 = spec.RefreshOn410
		t.M.UseAuth = spec.UseAuth
		if b, err := json.Marshal(spec.Headers); err == nil {
			t.M.Headers = string(b)
		}
		m.mu.Unlock()
		m.save(t)
	}
	return nil
}

func (m *Manager) applyHeaders(req *http.Request, t *Task) {
	for k, v := range parseHeaders(t.M.Headers) {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36")
	}
	if t.M.UseAuth && m.cookie != nil {
		if c := m.cookie(t.M.Source); c != "" && req.Header.Get("Cookie") == "" {
			req.Header.Set("Cookie", c)
		}
	}
}

func (m *Manager) updateProgress(t *Task, written int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.M.Bytes = written
	now := time.Now()
	if d := now.Sub(t.lastAt); d >= 500*time.Millisecond && d > 0 {
		t.speed = int64(float64(written-t.lastBytes) / d.Seconds())
		t.lastBytes = written
		t.lastAt = now
	}
}

func (m *Manager) save(t *Task) {
	m.mu.Lock()
	cp := t.M
	m.mu.Unlock()
	_ = m.st.SaveTask(cp)
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
