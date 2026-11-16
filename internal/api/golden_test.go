package api

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
)

func goldenKeys(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

func compareKeys(t *testing.T, label string, obj map[string]any, golden string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	want := goldenKeys(t, golden)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s 字段基线不一致\n got: %v\nwant: %v", label, got, want)
	}
}

// TestGoldenStateShape 固定 /api/state 的行/统计/项目结构，以及 /api/download 结构。
func TestGoldenStateShape(t *testing.T) {
	s, st, ts, _ := newAcceptanceServer(t)
	row := model.Row{Token: "golden", Status: "new", Source: "local", Title: "Golden"}
	if err := st.UpsertRows([]model.Row{row}); err != nil {
		t.Fatal(err)
	}
	reloadRows(t, s)

	state := getJSON(t, ts, "/api/state")
	rows, _ := state["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows 数量 = %d", len(rows))
	}
	rowObj, _ := rows[0].(map[string]any)
	compareKeys(t, "row", rowObj, "row_keys.golden")

	stats, _ := state["stats"].(map[string]any)
	compareKeys(t, "stats", stats, "stats_keys.golden")

	project, _ := state["project"].(map[string]any)
	compareKeys(t, "project", project, "project_keys.golden")

	dl := getJSON(t, ts, "/api/download")
	download, _ := dl["download"].(map[string]any)
	compareKeys(t, "download", download, "download_keys.golden")

	if rowObj["token"] != "golden" {
		t.Fatalf("token = %v", rowObj["token"])
	}
	if _, ok := stats["total"].(float64); !ok {
		t.Fatalf("stats.total 类型不是 number: %T", stats["total"])
	}
}
