// Package releasetest stands in for a GitHub project's releases in tests.
package releasetest

import (
	"net/http"
	"net/http/httptest"
	"path"
	"testing"
)

// Serve serves, for the rest of the test, releases whose latest is tag,
// with assets (name → content), as GitHub does: /releases/latest redirects
// to the tag, and each asset is at /releases/download/<tag>/<name>. It
// returns the releases' URL.
func Serve(t testing.TB, tag string, assets map[string][]byte) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/", func(w http.ResponseWriter, r *http.Request) {
		if content, ok := assets[path.Base(r.URL.Path)]; ok {
			w.Write(content)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/releases"
}
