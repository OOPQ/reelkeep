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
	"strconv"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/proc"
	"github.com/OOPQ/reelkeep/internal/source"
)

// runHTTP 下载普通 http(s) 地址：大文件优先走多连接分片，失败/不支持时回退单连接。
func (m *Manager) runHTTP(ctx context.Context, t *Task) error {
	return m.runHTTPDispatch(ctx, t)
}

// runHTTPSingle 是原来的单连接实现，支持断点续传；也是分片失败时的回退路径。
func (m *Manager) runHTTPSingle(ctx context.Context, t *Task) error {
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

	refreshed := false
	settings := m.getSettings()
	attempts := settings.RetryCount
	if attempts <= 0 {
		attempts = 4
	}
	if attempts > 8 {
		attempts = 8
	}
	delay := settings.RetryDelay
	if delay <= 0 {
		delay = time.Second
	}
	for attempt := 0; attempt < attempts; attempt++ {
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
			if attempt < attempts-1 {
				sleepCtx(ctx, retryWait(delay, attempt))
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
			if t.M.RefreshOn410 && !refreshed {
				refreshed = true
				if err := m.refresh(ctx, t); err != nil {
					return fmt.Errorf("地址已失效（410），刷新失败: %w", err)
				}
				_ = os.Remove(part)
				offset = 0
				continue
			}
			return errors.New("retry attempts exhausted")
		case code == http.StatusForbidden:
			_ = resp.Body.Close()
			// 403 有时是签名过期：源支持刷新时先重取一次地址再试。
			if t.M.RefreshOn410 && !refreshed {
				refreshed = true
				if err := m.refresh(ctx, t); err == nil {
					_ = os.Remove(part)
					offset = 0
					continue
				}
			}
			return errors.New("HTTP 403：源站拒绝（链接可能已过期或下架，或需要 Referer/登录）")
		case code >= 500:
			_ = resp.Body.Close()
			if attempt < attempts-1 {
				sleepCtx(ctx, retryWait(delay, attempt))
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
			if resp.ContentLength < 0 {
				m.mu.Lock()
				t.M.Total = 0
				m.mu.Unlock()
			}
		} else {
			// 206：必须确认服务器从我们要求的 offset 开始，否则会拼出坏文件
			if cr := resp.Header.Get("Content-Range"); cr != "" {
				start, total, perr := parseContentRange(cr)
				if perr != nil || start != offset {
					_ = resp.Body.Close()
					_ = os.Remove(part)
					offset = 0
					continue
				}
				if total > 0 {
					m.mu.Lock()
					t.M.Total = total
					m.mu.Unlock()
				}
			}
			flags |= os.O_APPEND
		}
		f, err := os.OpenFile(part, flags, 0o644)
		if err != nil {
			_ = resp.Body.Close()
			return err
		}
		err = m.copyBody(ctx, t, resp, f, offset, part, final)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt < attempts-1 {
			if st, serr := os.Stat(part); serr == nil {
				offset = st.Size()
			}
			sleepCtx(ctx, retryWait(delay, attempt))
			continue
		}
		return err
	}
	return errors.New("重试次数用尽")
}

func retryWait(base time.Duration, attempt int) time.Duration {
	d := base * time.Duration(attempt+1)
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// parseContentRange 解析 "bytes 100-199/1234"。
func parseContentRange(v string) (start, total int64, err error) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "bytes ") {
		return 0, 0, errors.New("bad Content-Range")
	}
	spec := strings.TrimPrefix(v, "bytes ")
	parts := strings.SplitN(spec, "/", 2)
	rng := strings.SplitN(parts[0], "-", 2)
	if len(rng) != 2 {
		return 0, 0, errors.New("bad Content-Range")
	}
	start, err = strconv.ParseInt(strings.TrimSpace(rng[0]), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	if len(parts) == 2 && parts[1] != "*" {
		total, _ = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	}
	return start, total, nil
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
			if werr := m.waitHTTPLimit(ctx, n); werr != nil {
				_ = f.Close()
				return werr
			}
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
	ff := proc.FindFFmpeg(strings.TrimSpace(m.getSettings().FFmpeg))
	if ff == "" {
		return errors.New("HLS 下载需要 ffmpeg：已自动查找 PATH、程序目录和旧项目配置；也可以把 ffmpeg.exe 放到程序目录，或在「设置 → FFmpeg 路径」里填写")
	}
	err := m.hlsOnce(ctx, t, ff, part)
	// 签名 HLS 地址过期时 ffmpeg 通常只报 403，没有状态码；失败后先重解一次详情。
	if err != nil && ctx.Err() == nil {
		if rerr := m.refresh(ctx, t); rerr == nil {
			err = m.hlsOnce(ctx, t, ff, part)
		}
	}
	if err != nil {
		_ = os.Remove(part) // HLS 的 .part 不能断点续传，失败即清理
		return err
	}
	st, err := os.Stat(part)
	if err != nil || st.Size() == 0 {
		_ = os.Remove(part)
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
	proc.HideWindow(cmd)
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

// refresh 通过源重新解析这条记录（源可签发会过期的地址）。
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

// waitHTTPLimit 按限速器 burst 分段等待，避免单次读取大于 burst 时 WaitN 直接报错。
func (m *Manager) waitHTTPLimit(ctx context.Context, n int) error {
	m.mu.Lock()
	lim := m.httpLimiter
	m.mu.Unlock()
	if lim == nil || n <= 0 {
		return nil
	}
	burst := lim.Burst()
	if burst <= 0 {
		burst = n
	}
	for n > 0 {
		chunk := n
		if chunk > burst {
			chunk = burst
		}
		if err := lim.WaitN(ctx, chunk); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
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
