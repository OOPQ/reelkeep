package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/source"
)

// StartLegacyPektinoRepair re-fetches legacy pektino rows that only have a bare
// native id and no usable title/cover/play URL. It runs in the background so it
// never blocks startup.
func (s *Server) StartLegacyPektinoRepair() {
	src, ok := s.reg.Get("pektino")
	if !ok {
		return
	}
	detailer, ok := src.(source.Detailer)
	if !ok {
		return
	}
	s.mu.RLock()
	var targets []model.Row
	var dirty []model.Row
	for _, r := range s.rows {
		if !strings.EqualFold(r.Source, "pektino") {
			continue
		}
		if cleaned := cleanPektinoTitle(r.Title, r.SourceID); cleaned != r.Title {
			r.Title = cleaned
			dirty = append(dirty, r)
		}
		if r.SourceID == "" || r.PageURL == "" {
			continue
		}
		if r.Title != "" && r.CoverURL != "" && r.URL != "" {
			continue
		}
		targets = append(targets, r)
	}
	s.mu.RUnlock()
	for i := range dirty {
		_ = s.st.UpsertRows([]model.Row{dirty[i]})
		s.ReloadRow(dirty[i].Token)
	}
	if len(targets) == 0 {
		return
	}
	s.logbuf.Add("repair", "开始刷新 pektino 旧数据 "+itoa(len(targets))+" 条")
	go func() {
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		done := 0
		var mu sync.Mutex
		for _, row := range targets {
			row := row
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
				defer cancel()
				d, err := detailer.Detail(ctx, row.SourceID)
				if err != nil {
					return
				}
				d.Token = "pektino@" + row.SourceID
				d.Source = "pektino"
				d.SourceID = row.SourceID
				mergeScraped(&row, d, time.Now().Format("2006-01-02 15:04:05"))
				row.Title = cleanPektinoTitle(row.Title, row.SourceID)
				if err := s.st.UpsertRows([]model.Row{row}); err != nil {
					return
				}
				s.ReloadRow(row.Token)
				mu.Lock()
				done++
				n := done
				mu.Unlock()
				if n%25 == 0 {
					s.logbuf.Add("repair", "pektino 旧数据已刷新 "+itoa(n)+"/"+itoa(len(targets)))
				}
			}()
		}
		wg.Wait()
		s.logbuf.Add("repair", "pektino 旧数据刷新完成 "+itoa(done)+"/"+itoa(len(targets)))
	}()
}

func cleanPektinoTitle(title, sourceID string) string {
	t := strings.TrimSpace(title)
	for _, suffix := range []string{" - 免费视频", " - 免費視頻", " - 免费视频网", " - 免費視頻網"} {
		if i := strings.Index(t, suffix); i > 0 {
			t = strings.TrimSpace(t[:i])
			break
		}
	}
	if t == sourceID {
		return ""
	}
	return t
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
