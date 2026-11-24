package download

import (
	"context"
	"testing"
	"time"
)

func TestHostPoolRampAndSettle(t *testing.T) {
	p := newHostPool(8)
	h := "cdn.example.com"
	if got := p.Conn(h); got != 4 {
		t.Fatalf("初始连接数 = %d，期望 4", got)
	}
	p.report(h, 4, 100<<20, 10*time.Second) // 10MB/s
	if got := p.Conn(h); got != 6 {
		t.Fatalf("4 连接成功后应试探 6，实际 %d", got)
	}
	p.report(h, 6, 100<<20, 5*time.Second) // 20MB/s
	if got := p.Conn(h); got != 8 {
		t.Fatalf("6 连接更快时应试探 8，实际 %d", got)
	}
	p.report(h, 8, 100<<20, 10*time.Second) // 10MB/s，明显慢于 6
	if got := p.Conn(h); got != 6 {
		t.Fatalf("8 连接更慢时应回退到 6，实际 %d", got)
	}
}

func TestHostPoolThrottle(t *testing.T) {
	p := newHostPool(8)
	h := "cdn.example.com"
	p.throttle(h)
	if got := p.Conn(h); got != 2 {
		t.Fatalf("429 后连接数 = %d，期望 2", got)
	}
	if !p.cooling(h) {
		t.Fatal("429 后应处于冷却状态")
	}
}

func TestHostPoolAcquireLimit(t *testing.T) {
	p := newHostPool(8)
	p.maxPerHost = 2
	h := "cdn.example.com"
	ctx := context.Background()
	if err := p.acquire(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := p.acquire(ctx, h); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if err := p.acquire(waitCtx, h); err == nil {
		t.Fatal("超过 host 连接上限时 acquire 应阻塞到 ctx 超时")
	}
	p.release(h)
	okCtx, cancel2 := context.WithTimeout(ctx, time.Second)
	defer cancel2()
	if err := p.acquire(okCtx, h); err != nil {
		t.Fatalf("释放名额后应能获取，实际 %v", err)
	}
}
