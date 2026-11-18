package api

import "testing"

func TestRankPathPrefix(t *testing.T) {
	cases := map[string]string{
		"/videos/latest-updates/{page}": "/videos/latest-updates",
		"/video?page={page}":            "/video",
		"/list-{page}.htm":              "/list-",
		"/{page}":                       "",
		"/":                             "",
		"":                              "",
	}
	for in, want := range cases {
		if got := rankPathPrefix(in); got != want {
			t.Errorf("rankPathPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPathHasPrefix(t *testing.T) {
	cases := []struct {
		path, prefix string
		want         bool
	}{
		{"/videos/latest/2", "/videos/latest", true},
		{"/videos/latest", "/videos/latest", true},
		{"/video", "/video", true},
		{"/video/123", "/video", true},
		{"/video?x=1", "/video", true},
		{"/videos/123", "/video", false},
		{"/video123", "/video", false},
		{"/VIDEO/2", "/video", true},
	}
	for _, c := range cases {
		if got := pathHasPrefix(c.path, c.prefix); got != c.want {
			t.Errorf("pathHasPrefix(%q, %q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}
