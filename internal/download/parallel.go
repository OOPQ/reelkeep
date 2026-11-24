package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// 多连接分片下载：只对支持 Range 的 HTTP 直链生效，服务器不支持时自动回退单连接。
// 文件直接写进预分配的 .part，每个分片记录自己的完成字节数（.part.json），
// 因此暂停/重启后可以按分片续传，不需要合并临时文件。
const (
	minParallelSize        = int64(8 << 20) // 小于 8MB 不值得分片
	minSegmentSize         = int64(2 << 20) // 单个分片至少 2MB
	maxParallelConnections = 16
	segmentBufferSize      = 256 << 10
)

var (
	errRangeUnsupported = errors.New("服务器不支持分片下载")
	errRangeMismatch    = errors.New("服务器返回的 Range 不匹配")
	errRangeRefresh     = errors.New("分片下载需要刷新地址")
)

type parallelSegment struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"` // 含
	Done  int64 `json:"done"`
}

type parallelState struct {
	Version      int               `json:"version"`
	Total        int64             `json:"total"`
	ETag         string            `json:"etag,omitempty"`
	LastModified string            `json:"lastModified,omitempty"`
	Segments     []parallelSegment `json:"segments"`
}

type segRuntime struct {
	start, end int64
	done       atomic.Int64
}

// runHTTPDispatch 决定走分片还是单连接。
func (m *Manager) runHTTPDispatch(ctx context.Context, t *Task) error {
	final, part := m.pathsFor(t)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	if st, err := os.Stat(final); err == nil && st.Size() > 0 {
		return nil // 文件已经存在
	}

	// 已有分片状态：恢复。
	if st, ok := loadParallelState(part); ok {
		return m.runHTTPParallel(ctx, t, final, part, st, true)
	}
	// 旧版留下的 .part 没有分片状态，继续走原来的单连接续传。
	if st, err := os.Stat(part); err == nil && st.Size() > 0 {
		return m.runHTTPSingle(ctx, t)
	}

	if m.getSettings().HTTPConnections <= 1 {
		return m.runHTTPSingle(ctx, t)
	}
	st, ok := m.probeParallel(ctx, t, m.getSettings().HTTPConnections)
	if !ok {
		return m.runHTTPSingle(ctx, t)
	}
	if err := saveParallelState(part, st); err != nil {
		return m.runHTTPSingle(ctx, t)
	}
	return m.runHTTPParallel(ctx, t, final, part, st, false)
}

// probeRange 用 Range: bytes=0-0 探出总大小、ETag 和 Last-Modified。
// ok=false 表示服务器不支持分片；err 表示网络/服务端临时错误（保留已有分片状态）。
func (m *Manager) probeRange(ctx context.Context, t *Task) (total int64, etag, lastModified string, ok bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.M.URL, nil)
	if err != nil {
		return 0, "", "", false, err
	}
	m.applyHeaders(req, t)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, "", "", false, err
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	if resp.StatusCode != http.StatusPartialContent {
		return 0, "", "", false, nil
	}
	start, size, perr := parseContentRange(resp.Header.Get("Content-Range"))
	if perr != nil || start != 0 || size <= 0 {
		return 0, "", "", false, nil
	}
	return size, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), true, nil
}

func (m *Manager) probeParallel(ctx context.Context, t *Task, conns int) (parallelState, bool) {
	total, etag, lastModified, ok, err := m.probeRange(ctx, t)
	if err != nil || !ok || total < minParallelSize {
		return parallelState{}, false
	}
	if conns > maxParallelConnections {
		conns = maxParallelConnections
	}
	if maxN := int(total / minSegmentSize); conns > maxN {
		conns = maxN
	}
	if conns < 2 {
		return parallelState{}, false
	}
	segs := splitSegments(total, conns)
	if len(segs) < 2 {
		return parallelState{}, false
	}
	return parallelState{Version: 1, Total: total, ETag: etag, LastModified: lastModified, Segments: segs}, true
}

func splitSegments(total int64, n int) []parallelSegment {
	if n < 1 {
		n = 1
	}
	base := total / int64(n)
	segs := make([]parallelSegment, 0, n)
	var start int64
	for i := 0; i < n; i++ {
		end := start + base - 1
		if i == n-1 {
			end = total - 1
		}
		if end >= start {
			segs = append(segs, parallelSegment{Start: start, End: end})
		}
		start = end + 1
	}
	return segs
}

func parallelStatePath(part string) string { return part + ".json" }

func (st parallelState) valid() bool {
	if st.Version != 1 || st.Total <= 0 || len(st.Segments) < 2 {
		return false
	}
	var next int64
	for _, s := range st.Segments {
		if s.Start != next || s.End < s.Start {
			return false
		}
		size := s.End - s.Start + 1
		if s.Done < 0 || s.Done > size {
			return false
		}
		next = s.End + 1
	}
	return next == st.Total
}

func loadParallelState(part string) (parallelState, bool) {
	b, err := os.ReadFile(parallelStatePath(part))
	if err != nil {
		return parallelState{}, false
	}
	var st parallelState
	if json.Unmarshal(b, &st) != nil || !st.valid() {
		return parallelState{}, false
	}
	return st, true
}

func saveParallelState(part string, st parallelState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := parallelStatePath(part) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, parallelStatePath(part))
}

func compatibleIdentity(st parallelState, etag, lastModified string) bool {
	if st.ETag != "" && etag != "" && st.ETag != etag {
		return false
	}
	if st.LastModified != "" && lastModified != "" && st.LastModified != lastModified {
		return false
	}
	return true
}

func (m *Manager) clearParallel(part string) {
	_ = os.Remove(part)
	_ = os.Remove(parallelStatePath(part))
}

// runHTTPParallel 执行/恢复一次分片下载。resume 表示状态来自磁盘，需要重新校验远端。
func (m *Manager) runHTTPParallel(ctx context.Context, t *Task, final, part string, st parallelState, resume bool) error {
	if !st.valid() {
		m.clearParallel(part)
		return m.runHTTPSingle(ctx, t)
	}
	if resume {
		total, etag, lastModified, ok, perr := m.probeRange(ctx, t)
		if perr != nil {
			return perr // 网络/服务端临时错误：保留进度，下次重试验证
		}
		if !ok || total != st.Total || !compatibleIdentity(st, etag, lastModified) {
			m.clearParallel(part)
			return m.runHTTPSingle(ctx, t)
		}
	}

	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	closed := false
	closeFile := func() error {
		if closed {
			return nil
		}
		closed = true
		return f.Close()
	}
	defer func() { _ = closeFile() }()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() > st.Total {
		_ = closeFile()
		m.clearParallel(part)
		return m.runHTTPSingle(ctx, t)
	}
	if fi.Size() != st.Total {
		// .part 丢了或被截断：不能相信 sidecar 里的 Done（否则会跳过数据留下空洞），
		// 重新分配文件并按 0 进度下载。
		if err := f.Truncate(st.Total); err != nil {
			return err
		}
		for i := range st.Segments {
			st.Segments[i].Done = 0
		}
		if err := saveParallelState(part, st); err != nil {
			return err
		}
	}

	var done int64
	for _, s := range st.Segments {
		done += s.Done
	}
	m.mu.Lock()
	t.M.Total = st.Total
	t.M.Bytes = done
	t.lastBytes = done
	t.lastAt = time.Now()
	m.mu.Unlock()

	refreshed := false
	for {
		next, err := m.downloadParallelChunks(ctx, t, f, part, st)
		st = next
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errRangeRefresh) && t.M.RefreshOn410 && !refreshed {
			refreshed = true
			if rerr := m.refresh(ctx, t); rerr == nil {
				total, etag, lastModified, ok, perr := m.probeRange(ctx, t)
				if perr == nil && ok && total == st.Total && compatibleIdentity(st, etag, lastModified) {
					continue
				}
			}
		}
		if errors.Is(err, errRangeUnsupported) || errors.Is(err, errRangeMismatch) {
			_ = closeFile()
			m.clearParallel(part)
			return m.runHTTPSingle(ctx, t)
		}
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 校验每个分片都完整，再落盘改名。
	for _, s := range st.Segments {
		size := s.End - s.Start + 1
		if s.Done != size {
			return fmt.Errorf("分片未完成：%d/%d 字节", s.Done, size)
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := closeFile(); err != nil {
		return err
	}
	_ = os.Remove(parallelStatePath(part))
	if err := os.Rename(part, final); err != nil {
		return err
	}
	m.mu.Lock()
	t.M.Total = st.Total
	t.M.Bytes = st.Total
	m.mu.Unlock()
	return nil
}

// downloadParallelChunks 启动所有未完成分片，并定期把进度写回 .part.json。
func (m *Manager) downloadParallelChunks(ctx context.Context, t *Task, f *os.File, part string, st parallelState) (parallelState, error) {
	runtimes := make([]segRuntime, len(st.Segments))
	var progress atomic.Int64
	for i, s := range st.Segments {
		runtimes[i].start, runtimes[i].end = s.Start, s.End
		runtimes[i].done.Store(s.Done)
		progress.Add(s.Done)
	}

	attempts := m.getSettings().RetryCount
	if attempts <= 0 {
		attempts = 4
	}
	if attempts > 8 {
		attempts = 8
	}
	delay := m.getSettings().RetryDelay
	if delay <= 0 {
		delay = time.Second
	}

	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(runtimes))
	for i := range runtimes {
		rt := &runtimes[i]
		if rt.done.Load() >= rt.end-rt.start+1 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.downloadSegment(ctx2, t, f, rt, st.Total, &progress, attempts, delay); err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
			}
		}()
	}

	stopTicker := make(chan struct{})
	tickerDone := make(chan struct{})
	go func() {
		defer close(tickerDone)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		lastSave := time.Now()
		for {
			select {
			case <-stopTicker:
				return
			case <-ticker.C:
				m.updateProgress(t, progress.Load())
				if time.Since(lastSave) >= time.Second {
					lastSave = time.Now()
					_ = saveParallelState(part, snapshotParallelState(st, runtimes))
					m.save(t)
				}
			}
		}
	}()

	wg.Wait()
	close(errCh)
	close(stopTicker)
	<-tickerDone
	m.updateProgress(t, progress.Load())
	snapshot := snapshotParallelState(st, runtimes)
	_ = saveParallelState(part, snapshot)

	var firstErr error
	for err := range errCh {
		if firstErr == nil && !errors.Is(err, context.Canceled) {
			firstErr = err
		}
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if firstErr != nil {
		return snapshot, firstErr
	}
	return snapshot, nil
}

func snapshotParallelState(st parallelState, runtimes []segRuntime) parallelState {
	out := st
	out.Segments = make([]parallelSegment, len(runtimes))
	for i := range runtimes {
		out.Segments[i] = parallelSegment{
			Start: runtimes[i].start,
			End:   runtimes[i].end,
			Done:  runtimes[i].done.Load(),
		}
	}
	return out
}

func (m *Manager) downloadSegment(ctx context.Context, t *Task, f *os.File, rt *segRuntime, total int64, progress *atomic.Int64, attempts int, delay time.Duration) error {
	buf := make([]byte, segmentBufferSize)
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		off := rt.start + rt.done.Load()
		if off > rt.end {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.M.URL, nil)
		if err != nil {
			return err
		}
		m.applyHeaders(req, t)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, rt.end))
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
		case code == http.StatusPartialContent:
			start, gotTotal, perr := parseContentRange(resp.Header.Get("Content-Range"))
			if perr != nil || start != off || (gotTotal > 0 && gotTotal != total) {
				_ = resp.Body.Close()
				return errRangeMismatch
			}
			err := m.readSegment(ctx, t, f, resp, rt, off, progress, buf)
			_ = resp.Body.Close()
			if err == nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, errRangeMismatch) {
				return err
			}
			if attempt < attempts-1 {
				sleepCtx(ctx, retryWait(delay, attempt))
				continue
			}
			return err
		case code == http.StatusOK:
			_ = resp.Body.Close()
			return errRangeUnsupported
		case code == http.StatusForbidden || code == http.StatusGone:
			_ = resp.Body.Close()
			if t.M.RefreshOn410 {
				return errRangeRefresh
			}
			if code == http.StatusForbidden {
				return errors.New("HTTP 403：源站拒绝（链接可能已过期或下架，或需要 Referer/登录）")
			}
			return errors.New("retry attempts exhausted")
		case code == http.StatusRequestedRangeNotSatisfiable:
			_ = resp.Body.Close()
			if rt.start+rt.done.Load() > rt.end {
				return nil
			}
			return fmt.Errorf("HTTP 416：范围 %d-%d 不可用", off, rt.end)
		case code == http.StatusTooManyRequests:
			_ = resp.Body.Close()
			if attempt < attempts-1 {
				sleepCtx(ctx, retryWait(delay, attempt))
				continue
			}
			return errors.New("HTTP 429：请求过多，可把「单文件连接数」调小后重试")
		case code >= 500:
			_ = resp.Body.Close()
			if attempt < attempts-1 {
				sleepCtx(ctx, retryWait(delay, attempt))
				continue
			}
			return fmt.Errorf("HTTP %d", code)
		default:
			_ = resp.Body.Close()
			return fmt.Errorf("HTTP %d", code)
		}
	}
	return errors.New("重试次数用尽")
}

func (m *Manager) readSegment(ctx context.Context, t *Task, f *os.File, resp *http.Response, rt *segRuntime, off int64, progress *atomic.Int64, buf []byte) error {
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if off+int64(n)-1 > rt.end {
				return errRangeMismatch
			}
			if err := m.waitHTTPLimit(ctx, n); err != nil {
				return err
			}
			if _, err := f.WriteAt(buf[:n], off); err != nil {
				return err
			}
			off += int64(n)
			rt.done.Add(int64(n))
			progress.Add(int64(n))
		}
		if rerr == io.EOF {
			if off <= rt.end {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
