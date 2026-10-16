package companion

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"vibe/internal/state"
)

// tokenPattern is what a page's token looks like: the page takes its URL's
// fragment for one only when it matches, so an in-page link can never
// replace the token it keeps.
const tokenPattern = `^[0-9a-f]{64}$`

func TestThePageKnowsATokenWhenItSeesOne(t *testing.T) {
	home := t.TempDir()
	for _, get := range []func() (string, error){
		func() (string, error) { return state.CompanionToken(home, "box") },
		func() (string, error) { return state.ManageToken(home) },
	} {
		tok, err := get()
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(tokenPattern).MatchString(tok) {
			t.Errorf("a token vibe made, %q, doesn't look like one to the page", tok)
		}
	}
	app, err := os.ReadFile(filepath.Join("ui", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(app), "/"+tokenPattern+"/") {
		t.Errorf("app.js doesn't check its URL's fragment against %s", tokenPattern)
	}
}

// The page's Content-Security-Policy (style-src 'self') has the browser
// drop inline styles: a style attribute does nothing, silently, though a
// test page without the policy (jsdom) shows it working. Styles go in
// app.css, or are set from script through element.style, which the policy
// allows.
func TestThePageUsesNoStyleTheBrowserWouldDrop(t *testing.T) {
	inline := regexp.MustCompile(`[{,]\s*style\s*:|\sstyle="|<style[\s>]`)
	for _, name := range []string{"app.js", "manage.js", "review.js", "markdown.js", "index.html"} {
		data, err := os.ReadFile(filepath.Join("ui", name))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if inline.MatchString(line) {
				t.Errorf("ui/%s:%d sets an inline style, which the page's policy drops: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
