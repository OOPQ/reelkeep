package rule

import (
	"strings"
	"testing"
	"time"
)

func TestLooksLikeChallenge(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{200, "<html><title>Just a moment...</title>", true},
		{200, `<div id="cf-challenge-running">`, true},
		{200, "Verify you are human by completing the action below", true},
		{200, "请完成验证后继续访问", true},
		{403, "denied", true},
		{429, "too many requests", true},
		{503, "", true},
		{200, "<html><body><a class='card'>normal</a></body></html>", false},
		{200, strings.Repeat("x", 2000), false},
	}
	for _, c := range cases {
		if got := looksLikeChallenge(c.status, []byte(c.body)); got != c.want {
			t.Errorf("looksLikeChallenge(%d, %q) = %v, want %v", c.status, c.body, got, c.want)
		}
	}
}

func TestHostLimiterAdaptive(t *testing.T) {
	l := newHostLimiter(200 * time.Millisecond)
	if got := l.Interval(); got != 200*time.Millisecond {
		t.Fatalf("初始间隔 = %v", got)
	}
	l.Penalize()
	if got := l.Interval(); got != 400*time.Millisecond {
		t.Fatalf("Penalize 后 = %v，期望 400ms", got)
	}
	for i := 0; i < 10; i++ {
		l.Penalize()
	}
	if got := l.Interval(); got > 2*time.Second {
		t.Fatalf("上限应钳到 2s，实际 %v", got)
	}
	for i := 0; i < 100; i++ {
		l.Reward()
	}
	if got := l.Interval(); got != 200*time.Millisecond {
		t.Fatalf("Reward 后应回到基础值 200ms，实际 %v", got)
	}
}
