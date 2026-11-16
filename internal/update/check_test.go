package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.9.5", "0.9.4", true},
		{"0.9.4", "0.9.4", false},
		{"0.10.0", "0.9.9", true},
		{"v1.0.0", "0.9.9", true},
		{"1.0.0-beta", "0.9.9", true},
		{"0.9.3", "0.9.4", false},
	}
	for _, c := range cases {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%q,%q)=%v want %v", c.latest, c.current, got, c.want)
		}
	}
}
