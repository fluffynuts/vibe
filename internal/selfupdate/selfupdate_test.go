package selfupdate

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
	"strings"
	"testing"
)

// makeZip builds a zip in memory: name → content, with modes.
func makeZip(t *testing.T, files map[string]string, modes map[string]os.FileMode) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		mode := os.FileMode(0o644)
		if m, ok := modes[name]; ok {
			mode = m
		}
		h.SetMode(mode)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeGitHub serves a releases page whose latest is tag, with the given
// assets and a SHA256SUMS covering sums (name → content to hash).
func fakeGitHub(t *testing.T, tag string, assets map[string][]byte, sums map[string][]byte) {
	t.Helper()
	var sumLines strings.Builder
	for name, content := range sums {
		h := sha256.Sum256(content)
		sumLines.WriteString(hex.EncodeToString(h[:]) + "  " + name + "\n")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == SumsFile {
			w.Write([]byte(sumLines.String()))
			return
		}
		if content, ok := assets[name]; ok {
			w.Write(content)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := Releases
	Releases = srv.URL + "/releases"
	t.Cleanup(func() { Releases = old })
}

func TestAssetName(t *testing.T) {
	for _, tt := range []struct{ goos, goarch, want string }{
		{"linux", "amd64", "vibe-linux-amd64.zip"},
		{"darwin", "arm64", "vibe-macos-arm64.zip"},
		{"windows", "arm64", "vibe-windows-arm64.zip"},
	} {
		if got := AssetName(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("AssetName(%s, %s) = %s, want %s", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, tt := range []struct {
		latest, running string
		want            bool
	}{
		{"0.1.0.57", "0.1.0.56", true},
		{"0.1.0.10", "0.1.0.9", true}, // numbers, not text
		{"0.1.0.57", "0.1.0.57", false},
		{"0.1.0.56", "0.1.0.57", false},
		{"0.2.0.1", "0.1.0.99", true},
		{"0.1.0.1", "0.1.0", true}, // a local build, with no build number
		{"v0.1.0.1", "0.1.0.1", false},
		// VERSION became major.minor, the build number the third part: the
		// first such release has to count as newer than the old four-part ones.
		{"0.1.11", "0.1.0.10", true},
		{"0.1.12", "0.1.11", true},
		{"0.1.11", "0.1", true}, // a local build under the new scheme
		{"0.2.13", "0.1.99", true},
	} {
		if got := Newer(tt.latest, tt.running); got != tt.want {
			t.Errorf("Newer(%s, %s) = %v, want %v", tt.latest, tt.running, got, tt.want)
		}
	}
}

func TestLatestTagFollowsTheRedirect(t *testing.T) {
	fakeGitHub(t, "v0.1.0.57", nil, nil)
	if tag, err := LatestTag(); err != nil || tag != "v0.1.0.57" {
		t.Errorf("LatestTag = %q, %v; want v0.1.0.57", tag, err)
	}
}

func TestDownloadChecksTheChecksum(t *testing.T) {
	good := []byte("the real zip")
	fakeGitHub(t, "v1.0.0.1",
		map[string][]byte{"vibe-linux-amd64.zip": good, "vibe-macos-arm64.zip": []byte("tampered")},
		map[string][]byte{"vibe-linux-amd64.zip": good, "vibe-macos-arm64.zip": []byte("what was built")})

	got, err := Download("v1.0.0.1", "vibe-linux-amd64.zip", t.TempDir())
	if err != nil {
		t.Fatalf("Download of a matching zip: %v", err)
	}
	if data, _ := os.ReadFile(got); !bytes.Equal(data, good) {
		t.Errorf("downloaded %q", data)
	}

	if _, err := Download("v1.0.0.1", "vibe-macos-arm64.zip", t.TempDir()); err == nil || !strings.Contains(err.Error(), "doesn't match its checksum") {
		t.Errorf("Download of a zip that doesn't match = %v, want a checksum error", err)
	}
	if _, err := Download("v1.0.0.1", "vibe-plan9-386.zip", t.TempDir()); err == nil || !strings.Contains(err.Error(), "has no vibe-plan9-386.zip") {
		t.Errorf("Download of a platform the release lacks = %v", err)
	}
}

func TestUnzipKeepsTheLayoutAndTheExecutableBit(t *testing.T) {
	data := makeZip(t, map[string]string{
		"vibe-1.0.0.1-linux-amd64/vibe":                  "#!/bin/sh\n",
		"vibe-1.0.0.1-linux-amd64/config.yaml":           "name: vibe\n",
		"vibe-1.0.0.1-linux-amd64/defaults/scripts/01-a": "echo a\n",
	}, map[string]os.FileMode{"vibe-1.0.0.1-linux-amd64/vibe": 0o755})
	zipPath := filepath.Join(t.TempDir(), "vibe.zip")
	os.WriteFile(zipPath, data, 0o644)

	dir := t.TempDir()
	top, err := Unzip(zipPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	if top != filepath.Join(dir, "vibe-1.0.0.1-linux-amd64") {
		t.Errorf("top = %s", top)
	}
	if _, err := os.Stat(filepath.Join(top, "defaults", "scripts", "01-a")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}
	info, err := os.Stat(filepath.Join(top, "vibe"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the binary lost its executable bit: %v", info.Mode())
	}
}

func TestUnzipRefusesEntriesOutsideTheFolder(t *testing.T) {
	for label, files := range map[string]map[string]string{
		"zip slip":    {"vibe/ok": "x", "../escaped": "x"},
		"two folders": {"vibe-a/x": "x", "vibe-b/y": "y"},
	} {
		zipPath := filepath.Join(t.TempDir(), "bad.zip")
		os.WriteFile(zipPath, makeZip(t, files, nil), 0o644)
		dir := t.TempDir()
		if _, err := Unzip(zipPath, dir); err == nil {
			t.Errorf("%s: Unzip accepted it", label)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped")); err == nil {
			t.Errorf("%s: a file was written outside the folder", label)
		}
	}
}
