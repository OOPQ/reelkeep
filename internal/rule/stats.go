package rule

import (
	"errors"
	"strings"
	"sync"
)

// SourceStats 是单个源的请求计数（进程内累计，重启清零）。
type SourceStats struct {
	OK        int64 `json:"ok"`
	Blocked   int64 `json:"blocked"`
	Challenge int64 `json:"challenge"`
	Failed    int64 `json:"failed"`
}

var (
	statsMu  sync.Mutex
	statsMap = map[string]*SourceStats{}
)

func recordStat(id string, kind string) {
	statsMu.Lock()
	defer statsMu.Unlock()
	st := statsMap[id]
	if st == nil {
		st = &SourceStats{}
		statsMap[id] = st
	}
	switch kind {
	case "ok":
		st.OK++
	case "blocked":
		st.Blocked++
	case "challenge":
		st.Challenge++
	default:
		st.Failed++
	}
}

// StatsSnapshot 返回所有源的计数副本。
func StatsSnapshot() map[string]SourceStats {
	statsMu.Lock()
	defer statsMu.Unlock()
	out := make(map[string]SourceStats, len(statsMap))
	for k, v := range statsMap {
		out[k] = *v
	}
	return out
}

// recordRequest 按错误类型归类一次请求结果。
func (f *HTTPFetcher) recordRequest(err error) {
	id := f.pkgID()
	switch {
	case err == nil:
		recordStat(id, "ok")
	case errors.Is(err, ErrChallenge):
		recordStat(id, "challenge")
	case isBlockedErr(err):
		recordStat(id, "blocked")
	default:
		recordStat(id, "failed")
	}
}

func isBlockedErr(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "HTTP 403") || strings.HasPrefix(msg, "HTTP 429") ||
		strings.HasPrefix(msg, "HTTP 503") || strings.HasPrefix(msg, "HTTP 302")
}
