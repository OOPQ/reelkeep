package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerAsset(t *testing.T) {
	rel := Release{Assets: []Asset{
		{Name: "reelkeep-1.0.0-linux-amd64.tar.gz"},
		{Name: "reelkeep_1.0.0_amd64.deb"},
		{Name: "reelkeep_1.0.0_arm64.deb"},
		{Name: "Reelkeep-1.0.0-macos-arm64.dmg"},
		{Name: "Reelkeep-1.0.0-macos-amd64.dmg"},
		{Name: "Reelkeep-Setup-1.0.0.exe"},
	}}
	if a, ok := rel.InstallerAsset("windows", "amd64"); !ok || a.Name != "Reelkeep-Setup-1.0.0.exe" {
		t.Fatalf("windows: %+v %v", a, ok)
	}
	if a, ok := rel.InstallerAsset("darwin", "arm64"); !ok || a.Name != "Reelkeep-1.0.0-macos-arm64.dmg" {
		t.Fatalf("mac arm: %+v %v", a, ok)
	}
	if a, ok := rel.InstallerAsset("linux", "amd64"); !ok || a.Name != "reelkeep_1.0.0_amd64.deb" {
		t.Fatalf("linux: %+v %v", a, ok)
	}
	if _, ok := rel.InstallerAsset("freebsd", "amd64"); ok {
		t.Fatal("unsupported OS should not pick an asset")
	}
}

func TestDownloadAndChecksums(t *testing.T) {
	body := []byte("installer-bytes")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/installer.exe", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(want + "  Reelkeep-Setup-1.0.0.exe\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "setup.exe")
	got, err := Download(context.Background(), srv.Client(), srv.URL+"/installer.exe", dest, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("sha = %s want %s", got, want)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != string(body) {
		t.Fatalf("downloaded content wrong: %v", err)
	}

	sums, err := Checksums(context.Background(), srv.Client(), []Asset{{Name: "SHA256SUMS", URL: srv.URL + "/SHA256SUMS"}})
	if err != nil {
		t.Fatal(err)
	}
	if sums["Reelkeep-Setup-1.0.0.exe"] != want {
		t.Fatalf("checksums = %v", sums)
	}
}
