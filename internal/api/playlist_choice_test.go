package api

import (
	"context"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
)

type fakeURLScraper struct{}

func (fakeURLScraper) ScrapeURL(context.Context, string) (model.Row, error) {
	return model.Row{}, nil
}

type fakePlaylistScraper struct{ fakeURLScraper }

func (fakePlaylistScraper) ScrapePlaylist(context.Context, string) ([]model.Row, string, error) {
	return nil, "", nil
}

type fakePlaylistSupporter struct{ fakePlaylistScraper }

func (fakePlaylistSupporter) SupportsPlaylist() bool { return false }

type fakePlaylistSupporterYes struct{ fakePlaylistScraper }

func (fakePlaylistSupporterYes) SupportsPlaylist() bool { return true }

// 规则源都实现了 ScrapePlaylist，但没写 playlist 段时必须退回单条链接抓取。
func TestUsePlaylistScraper(t *testing.T) {
	cases := []struct {
		name string
		src  interface{}
		want bool
	}{
		{"仅 URLScraper", fakeURLScraper{}, false},
		{"实现了 Playlist 但声明不支持", fakePlaylistSupporter{}, false},
		{"声明支持 playlist", fakePlaylistSupporterYes{}, true},
		{"没有声明接口但能抓清单", fakePlaylistScraper{}, true},
	}
	for _, c := range cases {
		if got := usePlaylistScraper(c.src.(interface {
			ScrapeURL(context.Context, string) (model.Row, error)
		})); got != c.want {
			t.Errorf("%s: usePlaylistScraper=%v want %v", c.name, got, c.want)
		}
	}
}
