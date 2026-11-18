package rule

import "testing"

func TestMergeCookie(t *testing.T) {
	cases := []struct {
		cur, in, want string
	}{
		{"", "a=1", "a=1"},
		{"a=1", "", "a=1"},
		{"a=1; b=2", "b=3", "a=1; b=3"},
		{"a=1", "c=3; a=2", "a=2; c=3"},
		{"PHPSESSID=x", "cf_clearance=y", "PHPSESSID=x; cf_clearance=y"},
		{"bad; a=1", "a=2", "a=2"},
	}
	for _, c := range cases {
		if got := mergeCookie(c.cur, c.in); got != c.want {
			t.Errorf("mergeCookie(%q,%q)=%q want %q", c.cur, c.in, got, c.want)
		}
	}
}
