package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, running string
		want            bool
	}{
		{"0.1.57", "0.1.56", true},
		{"0.1.57", "0.1.57", false},
		{"0.1.57", "0.1.58", false},
		{"0.2.1", "0.1.99", true},
		{"0.1.5", "0.1", true}, // a local build is older than any release of its version
		{"0.1", "0.1.5", false},
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.running); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.latest, c.running, got, c.want)
		}
	}
}

func TestOnPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	dir := filepath.Join("home", "me", ".local", "bin")
	if !onPath(dir, filepath.Join("a", "b")+sep+dir+sep) {
		t.Error("should find dir in PATH")
	}
	if onPath(dir, filepath.Join("a", "b")) {
		t.Error("should not find dir in PATH")
	}
	if onPath(dir, "") {
		t.Error("empty PATH has nothing on it")
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("darwin", "arm64"); got != "{{NAME}}-macos-arm64.zip" {
		t.Errorf("got %s", got)
	}
}

// releaseServer stands in for GitHub: /latest redirects to a tag, and the
// tag's folder holds the platform's zip and a SHA256SUMS for it.
func releaseServer(t *testing.T, tag string, exe []byte, sum string) *httptest.Server {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("{{NAME}}-9.9.9-x/" + exeName())
	w.Write(exe)
	zw.Close()
	zipBytes := zbuf.Bytes()
	asset := assetName(runtime.GOOS, runtime.GOARCH)
	if sum == "" {
		h := sha256.Sum256(zipBytes)
		sum = hex.EncodeToString(h[:])
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+sumsFile, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sum + "  " + asset + "\n"))
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadAndOpenExe(t *testing.T) {
	srv := releaseServer(t, "v9.9.9", []byte("new binary"), "")
	old := releasesURL
	releasesURL = srv.URL + "/releases"
	defer func() { releasesURL = old }()

	tag, err := latestTag()
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("latestTag = %q, %v", tag, err)
	}
	zipPath, err := downloadAsset(tag, assetName(runtime.GOOS, runtime.GOARCH), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe, err := openExe(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), exeName())
	if err := replaceFile(dest, exe); err != nil {
		t.Fatal(err)
	}
	exe.Close()
	got, _ := os.ReadFile(dest)
	if string(got) != "new binary" {
		t.Errorf("replaced file holds %q", got)
	}
}

func TestDownloadRefusesABadChecksum(t *testing.T) {
	srv := releaseServer(t, "v9.9.9", []byte("new binary"), "deadbeef")
	old := releasesURL
	releasesURL = srv.URL + "/releases"
	defer func() { releasesURL = old }()

	if _, err := downloadAsset("v9.9.9", assetName(runtime.GOOS, runtime.GOARCH), t.TempDir()); err == nil {
		t.Fatal("expected a checksum error")
	}
}
