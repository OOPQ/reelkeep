package boot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/OOPQ/reelkeep/internal/config"
)

func makeProject(t *testing.T, root string) config.Paths {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"kind":"reelkeep-project"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := config.ForRoot(root)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestDifferentProjectsGetSeparatePorts(t *testing.T) {
	pathsA := makeProject(t, filepath.Join(t.TempDir(), "a"))
	pathsB := makeProject(t, filepath.Join(t.TempDir(), "b"))

	port := freePort(t)
	a, err := Start(context.Background(), Options{Paths: pathsA, Port: port, Version: "test", NoBrowser: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	b, err := Start(context.Background(), Options{Paths: pathsB, Port: port, Version: "test", NoBrowser: true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Existing {
		t.Fatal("different project must not reuse the existing instance")
	}
	if a.Port == b.Port {
		t.Fatalf("projects should use different ports: %d", a.Port)
	}
	if b.URL == "" {
		t.Fatal("second project URL is empty")
	}
}
