package api

import (
	"testing"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
	"github.com/OOPQ/reelkeep/internal/scan"
)

// TestStatsExcludeRecycled 回收站条目不应计入总数/时长/未下载等统计。
func TestStatsExcludeRecycled(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	rows := []model.Row{
		{Token: "keep", Status: "done", Seconds: "60", FirstSeen: today, FileName: "keep.mp4", FileSize: "1024", Res: "1280x720", LastPlay: "2026-11-19 20:00:00"},
		{Token: "trash", Status: "recycled", Seconds: "120", FirstSeen: today, FileName: "trash.mp4", FileSize: "2048", Res: "1920x1080", Fav: "1"},
	}
	st := statsFrom(rows, scan.Index{}, 0, "", "")
	if st.Total != 1 {
		t.Fatalf("Total = %d，期望 1", st.Total)
	}
	if st.Recycled != 1 {
		t.Fatalf("Recycled = %d，期望 1", st.Recycled)
	}
	if st.Hours != 0.02 {
		t.Fatalf("Hours = %v，期望 0.02（只算未回收条目）", st.Hours)
	}
	if st.TodayAdded != 1 || st.TodayHours != 0.02 {
		t.Fatalf("TodayAdded/Hours = %d/%v，期望 1/0.02", st.TodayAdded, st.TodayHours)
	}
	if st.Downloaded != 1 {
		t.Fatalf("Downloaded = %d，期望 1", st.Downloaded)
	}
	if st.Missing != 0 {
		t.Fatalf("Missing = %d，期望 0", st.Missing)
	}
	if st.Favorites != 0 {
		t.Fatalf("Favorites = %d，期望 0", st.Favorites)
	}
	if len(st.ByRes) != 1 || st.ByRes[0].Res != "1280x720" {
		t.Fatalf("ByRes = %+v，期望只包含 1280x720", st.ByRes)
	}
}
