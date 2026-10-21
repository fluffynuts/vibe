package sbxfind

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A bare "sbx" is looked up on PATH only, which a fresh install isn't on.
func TestNothingRunsABareSbx(t *testing.T) {
	bare := regexp.MustCompile(`exec\.Command(Context)?\((ctx, )?"sbx"`)
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bare.Match(src) {
			t.Errorf("%s runs a bare \"sbx\": use sbxfind.Path()", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
