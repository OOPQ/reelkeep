package download

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultPerSourceConcurrent = 4
	maxPerSourceConcurrent     = 8
	// hostConnectionCap 是同一 CDN host 同时允许的分片连接总数，避免多个任务叠加把站点打爆。
	hostConnectionCap = 24
	hostCooldown      = 45 * time.Second
	adaptiveStart     = 4
	adaptiveStep      = 2
	adaptiveSlowdown  = 0.90
)

type hostState struct {
	conns     int // 下一个任务使用的连接数
	bestConns int
	bestSpeed float64
	settled   bool
	coolUntil time.Time
	active    int
}

// hostPool 记录每个下载 host 的连接上限、冷却时间和自适应连接数。
// 只保留进程内状态，重启后重新学习，不影响正确性。
type hostPool struct {
	mu         sync.Mutex
	states     map[string]*hostState
	maxPerHost int
	maxConns   int
}

func newHostPool(maxConns int) *hostPool {
	p := &hostPool{states: map[string]*hostState{}, maxPerHost: hostConnectionCap}
	p.setMaxConns(maxConns)
	return p
}

func (p *hostPool) setMaxConns(n int) {
	if n < 1 {
		n = 1
	}
	if n > maxParallelConnections {
		n = maxParallelConnections
	}
	p.mu.Lock()
	p.maxConns = n
	for _, s := range p.states {
		if s.conns > n {
			s.conns = n
		}
	}
	p.mu.Unlock()
}

func hostOf(rawURL, fallback string) string {
	if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	return strings.ToLower(fallback)
}

func (p *hostPool) stateLocked(host string) *hostState {
	if s := p.states[host]; s != nil {
		return s
	}
	start := adaptiveStart
	if start > p.maxConns {
		start = p.maxConns
	}
	if start < 2 {
		start = 2
	}
	s := &hostState{conns: start}
	p.states[host] = s
	return s
}

// Conn 返回该 host 当前建议的分片连接数。
func (p *hostPool) Conn(host string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stateLocked(host)
	n := s.conns
	if n > p.maxConns {
		n = p.maxConns
	}
	if n < 2 {
		n = 2
	}
	return n
}

func sleepCtxErr(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// acquire 获取一个 host 连接名额；host 冷却期间会等待，ctx 取消可中断。
func (p *hostPool) acquire(ctx context.Context, host string) error {
	for {
		p.mu.Lock()
		s := p.stateLocked(host)
		now := time.Now()
		if now.Before(s.coolUntil) {
			wait := time.Until(s.coolUntil)
			p.mu.Unlock()
			if err := sleepCtxErr(ctx, wait); err != nil {
				return err
			}
			continue
		}
		if s.active < p.maxPerHost {
			s.active++
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		if err := sleepCtxErr(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

func (p *hostPool) release(host string) {
	p.mu.Lock()
	if s := p.states[host]; s != nil && s.active > 0 {
		s.active--
	}
	p.mu.Unlock()
}

func (p *hostPool) cooling(host string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.states[host]
	return s != nil && time.Now().Before(s.coolUntil)
}

// throttle 在 429 等限流信号后降低该 host 的连接数并冷却。
func (p *hostPool) throttle(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stateLocked(host)
	s.coolUntil = time.Now().Add(hostCooldown)
	s.conns = 2
	s.settled = true
	s.bestConns = 0
	s.bestSpeed = 0
}

// report 记录一次完成的分片下载，用实测速度决定下次是否增加连接数。
// 从 4 条开始，逐步试探 6/8/...；如果更多连接反而更慢，就回退到实测最快的档位。
func (p *hostPool) report(host string, conns int, bytes int64, d time.Duration) {
	if conns < 2 || bytes <= 0 || d <= 0 {
		return
	}
	speed := float64(bytes) / d.Seconds()
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stateLocked(host)
	if time.Now().Before(s.coolUntil) {
		return
	}
	if s.bestConns == 0 || speed > s.bestSpeed {
		s.bestConns, s.bestSpeed = conns, speed
	} else if conns > s.bestConns && speed < s.bestSpeed*adaptiveSlowdown {
		// 更多连接反而明显更慢：回到最快档，结束试探。
		s.settled = true
		s.conns = s.bestConns
		return
	}
	if s.settled {
		return
	}
	if conns >= p.maxConns {
		s.settled = true
		s.conns = s.bestConns
		return
	}
	next := conns + adaptiveStep
	if next > p.maxConns {
		next = p.maxConns
	}
	s.conns = next
}
