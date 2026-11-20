package review

import "testing"

// The page links "PR #110" to GitHub: the repository's address there is worked out from a remote's URL, in whichever form git was
// given it, and nothing else is taken for one.
func TestGitHubWebIsTheRepositorysAddressFromAnyFormOfItsURL(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/fluffynuts/vibe.git":            "https://github.com/fluffynuts/vibe",
		"https://github.com/fluffynuts/vibe":                "https://github.com/fluffynuts/vibe",
		"https://github.com/fluffynuts/vibe/":               "https://github.com/fluffynuts/vibe",
		"http://github.com/fluffynuts/vibe.git":             "https://github.com/fluffynuts/vibe",
		"https://user:token@github.com/fluffynuts/vibe.git": "https://github.com/fluffynuts/vibe",
		"git@github.com:fluffynuts/vibe.git":                "https://github.com/fluffynuts/vibe",
		"git@github.com:fluffynuts/vibe":                    "https://github.com/fluffynuts/vibe",
		"ssh://git@github.com/fluffynuts/vibe.git":          "https://github.com/fluffynuts/vibe",
		"ssh://git@github.com:22/fluffynuts/vibe.git":       "https://github.com/fluffynuts/vibe",
		"https://GitHub.com/Fluffy-Nuts/my.repo_1.git":      "https://github.com/Fluffy-Nuts/my.repo_1",
		"https://gitlab.com/fluffynuts/vibe.git":            "",
		"git@gitlab.com:fluffynuts/vibe.git":                "",
		"https://github.com.evil.example/a/b.git":           "",
		"https://github.com/onlyowner":                      "",
		"https://github.com/a/b/c":                          "",
		"https://github.com/a/b\"onclick=x":                 "",
		"/home/me/repos/vibe":                               "",
		"https://github.com/a/..":                           "",
		"https://github.com/a/.":                            "",
		"git@github.com:a/...git":                           "",
		"":                                                  "",
	} {
		if got := gitHubWeb(url); got != want {
			t.Errorf("gitHubWeb(%q) = %q, want %q", url, got, want)
		}
	}
}

// In a fork, origin is the fork and the pull requests are upstream's: upstream is the address, when it is on GitHub.
func TestTheStatusHasTheGitHubAddressUpstreamsFirstThenOrigins(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	if s := statusOfDir(t, dir); s.Web != "" {
		t.Errorf("with no remote, web = %q", s.Web)
	}
	git(t, dir, "remote", "add", "origin", "git@github.com:me/vibe.git")
	if s := statusOfDir(t, dir); s.Web != "https://github.com/me/vibe" {
		t.Errorf("with origin alone, web = %q", s.Web)
	}
	git(t, dir, "remote", "add", "upstream", "https://github.com/them/vibe.git")
	if s := statusOfDir(t, dir); s.Web != "https://github.com/them/vibe" {
		t.Errorf("with upstream too, web = %q", s.Web)
	}
	git(t, dir, "remote", "set-url", "upstream", "https://gitlab.com/them/vibe.git")
	if s := statusOfDir(t, dir); s.Web != "https://github.com/me/vibe" {
		t.Errorf("with upstream off GitHub, web = %q (want origin's)", s.Web)
	}
}
