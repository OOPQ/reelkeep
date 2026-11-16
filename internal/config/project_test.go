package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureProjectCreatesAndRejects(t *testing.T) {
	base := t.TempDir()
	root, err := EnsureProject(filepath.Join(base, "new-project"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if !IsProject(root) {
		t.Fatal("project.json/data should exist")
	}
	for _, p := range []string{"data", "media", filepath.Join("cache", "covers")} {
		if st, err := os.Stat(filepath.Join(root, p)); err != nil || !st.IsDir() {
			t.Fatalf("missing dir %s: %v", p, err)
		}
	}
	if again, err := EnsureProject(root); err != nil || again != root {
		t.Fatalf("reopen: %q %v", again, err)
	}

	bad := filepath.Join(base, "not-a-project")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "random.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureProject(bad); err == nil {
		t.Fatal("non-empty non-project dir should be rejected")
	}
}
