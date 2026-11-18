package rule

import (
	"context"
	"errors"
	"testing"
)

type fakePostFetcher struct {
	body []byte
	got  string
}

func (f *fakePostFetcher) Get(context.Context, string, map[string]string) ([]byte, error) {
	return nil, errors.New("unused")
}

func (f *fakePostFetcher) Post(_ context.Context, _ string, _ map[string]string, body string) ([]byte, error) {
	f.got = body
	return f.body, nil
}

func TestScrapeViaResolve(t *testing.T) {
	p := &Package{
		Schema: 1, ID: "xvideo", Name: "X", Version: "1.0.0",
		BaseURL: "https://x.com", Capabilities: []string{"scrape", "download"},
		List: List{URL: "/", Item: "article", Fields: map[string]Field{
			"id": {Selector: "a", Attr: "href", Regex: `status/(\d+)`},
		}},
		Resolve: &Resolve{
			URL:    "https://pektino.com/api/media",
			Method: "POST",
			Body:   `{"url":"{pageUrl}"}`,
			Fields: map[string]Field{
				"title":    {JSONPath: "$.data.url_cd"},
				"cover":    {JSONPath: "$.data.thumbnail"},
				"playable": {JSONPath: "$.data.url"},
			},
		},
	}
	p.normalize()
	f := &fakePostFetcher{body: []byte(`{"data":{"url_cd":"abc","thumbnail":"https://pbs.twimg.com/t.jpg","url":"https://video.twimg.com/v.mp4"}}`)}
	rs := New(p, f)
	// /status/<id> 和 /status/<id>/video/1 都要能识别成同一个条目。
	for _, rawURL := range []string{
		"https://x.com/u/status/12345?a=1",
		"https://x.com/u/status/12345/video/1",
	} {
		row, err := rs.ScrapeURL(context.Background(), rawURL)
		if err != nil {
			t.Fatalf("%s: %v", rawURL, err)
		}
		if row.Token != "xvideo@12345" || row.Title != "abc" || row.CoverURL == "" || row.URL == "" {
			t.Fatalf("%s 解析结果不对: %+v", rawURL, row)
		}
		if row.PageURL == "" {
			t.Fatalf("%s 缺少 pageUrl", rawURL)
		}
	}
	if f.got != `{"url":"https://x.com/u/status/12345/video/1"}` {
		t.Fatalf("POST body 不对: %s", f.got)
	}
}
