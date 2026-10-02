package sbxinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeTarGz builds a .tar.gz in memory: name → content, with modes; a name
// ending in "/" is a folder.
func makeTarGz(t *testing.T, files map[string]string, modes map[string]int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)
	for name, content := range files {
		h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			h.Typeflag, h.Mode, h.Size = tar.TypeDir, 0o755, 0
		}
		if m, ok := modes[name]; ok {
			h.Mode = m
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeGitHub serves a releases page whose latest is tag, with the given
// assets.
func fakeGitHub(t *testing.T, tag string, assets map[string][]byte) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/", func(w http.ResponseWriter, r *http.Request) {
		if content, ok := assets[filepath.Base(r.URL.Path)]; ok {
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

func TestStable(t *testing.T) {
	for tag, want := range map[string]bool{
		"v0.46.0":                      true,
		"v0.45.1":                      true,
		"0.46.0":                       true,
		"v1.12.103":                    true,
		"v0.47.0-rc2":                  false,
		"nightly":                      false,
		"nightly-202610020319-66723b5": false,
		"dev-85f8946":                  false,
		"v0.46":                        false,
		"v0.46.0.1":                    false,
	} {
		if got := Stable(tag); got != want {
			t.Errorf("Stable(%q) = %v, want %v", tag, got, want)
		}
	}
}

func TestLatestStableTag(t *testing.T) {
	fakeGitHub(t, "v0.46.0", nil)
	if tag, err := LatestStableTag(); err != nil || tag != "v0.46.0" {
		t.Errorf("LatestStableTag = %q, %v; want v0.46.0", tag, err)
	}
}

func TestLatestStableTagRefusesAPrerelease(t *testing.T) {
	fakeGitHub(t, "v0.47.0-rc2", nil)
	if tag, err := LatestStableTag(); err == nil {
		t.Errorf("LatestStableTag = %q, want an error for an RC", tag)
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("amd64"); got != "DockerSandboxes-linux-amd64.tar.gz" {
		t.Errorf("AssetName(amd64) = %q", got)
	}
	if got := AssetName("arm64"); got != "DockerSandboxes-linux-arm64.tar.gz" {
		t.Errorf("AssetName(arm64) = %q", got)
	}
}

func TestDownloadAndUntar(t *testing.T) {
	asset := AssetName("amd64")
	fakeGitHub(t, "v0.46.0", map[string][]byte{asset: makeTarGz(t, map[string]string{
		"docker-sbx/":           "",
		"docker-sbx/sbx":        "binary",
		"docker-sbx/install.sh": "#!/bin/sh\n",
		"docker-sbx/LICENSE":    "license",
	}, map[string]int64{"docker-sbx/sbx": 0o755, "docker-sbx/install.sh": 0o755})})

	got, err := Download("v0.46.0", asset, t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	dir := t.TempDir()
	bundle, err := Untar(got, dir)
	if err != nil {
		t.Fatalf("Untar: %v", err)
	}
	if bundle != filepath.Join(dir, "docker-sbx") {
		t.Errorf("Untar's folder = %s", bundle)
	}
	if data, _ := os.ReadFile(filepath.Join(bundle, "sbx")); string(data) != "binary" {
		t.Errorf("sbx holds %q", data)
	}
	if runtime.GOOS == "windows" {
		return // no Unix file modes to check
	}
	if info, err := os.Stat(filepath.Join(bundle, "install.sh")); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("install.sh: %v, %v; want mode 0755", info, err)
	}
	if info, err := os.Stat(filepath.Join(bundle, "LICENSE")); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("LICENSE: %v, %v; want mode 0644", info, err)
	}
}

func TestDownloadOfAMissingAssetFails(t *testing.T) {
	fakeGitHub(t, "v0.46.0", nil)
	if _, err := Download("v0.46.0", AssetName("amd64"), t.TempDir()); err == nil {
		t.Error("Download of a missing asset succeeded")
	}
}

func TestUntarRefusesEscapingEntries(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.tar.gz")
	os.WriteFile(p, makeTarGz(t, map[string]string{"docker-sbx/../../evil": "x"}, nil), 0o644)
	if _, err := Untar(p, t.TempDir()); err == nil {
		t.Error("Untar followed a ../ entry")
	}
}

func TestUntarRefusesMoreThanOneTopLevelFolder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "two.tar.gz")
	os.WriteFile(p, makeTarGz(t, map[string]string{"a/x": "x", "b/y": "y"}, nil), 0o644)
	if _, err := Untar(p, t.TempDir()); err == nil {
		t.Error("Untar accepted two top-level folders")
	}
}

func TestPatchInstallerMatchesTheHandFixedScript(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("testdata", "install-original.sh"))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := os.ReadFile(filepath.Join("testdata", "install-fixed.sh"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := PatchInstaller(string(original))
	if err != nil {
		t.Fatalf("PatchInstaller: %v", err)
	}
	if got != string(fixed) {
		t.Errorf("PatchInstaller's script differs from testdata/install-fixed.sh:\n%s", got)
	}
}

func TestPatchInstallerLeavesAPatchedScriptAlone(t *testing.T) {
	fixed, _ := os.ReadFile(filepath.Join("testdata", "install-fixed.sh"))
	got, err := PatchInstaller(string(fixed))
	if err != nil || got != string(fixed) {
		t.Errorf("PatchInstaller of the fixed script: changed = %v, err = %v", got != string(fixed), err)
	}
}

func TestPatchInstallerRefusesAnUnrecognisedAppArmorStep(t *testing.T) {
	script := "#!/bin/sh\nload_apparmor_profile /etc/apparmor.d/x\n"
	if _, err := PatchInstaller(script); err == nil {
		t.Error("PatchInstaller ran a script whose AppArmor steps it couldn't patch")
	}
}

func TestPatchInstallerWithoutAppArmor(t *testing.T) {
	script := "#!/bin/sh\ninstall -m 755 sbx \"${bin_dir}/sbx\"\n"
	if got, err := PatchInstaller(script); err != nil || got != script {
		t.Errorf("PatchInstaller = %q, %v; want it unchanged", got, err)
	}
}

func TestNeedsSudo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AppArmor is Linux-only, and a Windows PATH needs an .exe")
	}
	bundle, fs, etc := t.TempDir(), t.TempDir(), t.TempDir()
	oldFS, oldDir := apparmorFS, apparmorDir
	t.Cleanup(func() { apparmorFS, apparmorDir = oldFS, oldDir })
	apparmorFS, apparmorDir = fs, etc
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "apparmor_parser"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", bin)

	if NeedsSudo(bundle) {
		t.Error("NeedsSudo without a bundled profile")
	}
	os.WriteFile(filepath.Join(bundle, "apparmor-profile"), []byte("profile"), 0o644)
	if !NeedsSudo(bundle) {
		t.Error("NeedsSudo = false with a profile, AppArmor and apparmor_parser")
	}
	apparmorFS = filepath.Join(fs, "missing")
	if NeedsSudo(bundle) {
		t.Error("NeedsSudo with AppArmor inactive")
	}
	apparmorFS = fs
	t.Setenv("PATH", t.TempDir())
	if NeedsSudo(bundle) {
		t.Error("NeedsSudo without apparmor_parser")
	}
}

func TestParseVersion(t *testing.T) {
	for out, want := range map[string]string{
		"sbx version: v0.46.0 991967dc90ce0d9a440cd1df1bdf3e395c5a2693\n": "v0.46.0",
		"sbx version: v0.46.0\n": "v0.46.0",
		"something else\n":       "something else",
		"sbx version:   \n":      "sbx version:",
	} {
		if got := ParseVersion(out); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", out, got, want)
		}
	}
}
