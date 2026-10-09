package gittoken

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseRemote(t *testing.T) {
	for remote, want := range map[string]Repo{
		"https://github.com/fluffynuts/vibe.git":       {Owner: "fluffynuts", Name: "vibe"},
		"https://github.com/fluffynuts/vibe":           {Owner: "fluffynuts", Name: "vibe"},
		"https://github.com/fluffynuts/vibe/\n":        {Owner: "fluffynuts", Name: "vibe"},
		"https://someone@github.com/fluffynuts/vibe":   {Owner: "fluffynuts", Name: "vibe"},
		"git@github.com:fluffynuts/vibe.git":           {Owner: "fluffynuts", Name: "vibe", SSH: true},
		"github.com:fluffynuts/vibe":                   {Owner: "fluffynuts", Name: "vibe", SSH: true},
		"ssh://git@github.com/fluffynuts/vibe.git":     {Owner: "fluffynuts", Name: "vibe", SSH: true},
		"ssh://git@github.com:22/fluffynuts/my.repo":   {Owner: "fluffynuts", Name: "my.repo", SSH: true},
		"https://github.com/fluffynuts/dotted.name.js": {Owner: "fluffynuts", Name: "dotted.name.js"},
	} {
		got, ok := ParseRemote(remote)
		if !ok || got != want {
			t.Errorf("ParseRemote(%q) = %+v, %v; want %+v", remote, got, ok, want)
		}
	}
	for _, remote := range []string{
		"", "https://gitlab.com/o/r.git", "git@bitbucket.org:o/r.git",
		"https://github.com/fluffynuts", "https://github.com/o/r/tree/main", "https://notgithub.com/o/r",
	} {
		if got, ok := ParseRemote(remote); ok {
			t.Errorf("ParseRemote(%q) = %+v, want no repository", remote, got)
		}
	}
}

func TestKindOf(t *testing.T) {
	for token, want := range map[string]Kind{
		"github_pat_11ABC": FineGrained,
		"ghp_abc":          Classic,
		"gho_abc":          OAuth,
		"ghs_abc":          App,
		"something":        Unknown,
	} {
		if got := KindOf(token); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", token, got, want)
		}
	}
	if !Classic.AccountWide() || !OAuth.AccountWide() || FineGrained.AccountWide() {
		t.Error("only classic and OAuth tokens are account-wide")
	}
}

func TestValid(t *testing.T) {
	if err := Valid("github_pat_abc123"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "two words", "tab\there", "née"} {
		if Valid(bad) == nil {
			t.Errorf("Valid(%q) accepted it", bad)
		}
	}
}

func TestNewTokenURL(t *testing.T) {
	u, err := url.Parse(NewTokenURL("vibe a-very-long-sandbox-name-that-goes-past-forty", "Lets the agent push & more.", "fluffynuts", 7))
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "github.com" || u.Path != "/settings/personal-access-tokens/new" {
		t.Errorf("wrong page: %s", u)
	}
	q := u.Query()
	if len(q.Get("name")) != tokenNameLimit {
		t.Errorf("name not cut to %d characters: %q", tokenNameLimit, q.Get("name"))
	}
	for k, want := range map[string]string{
		"description": "Lets the agent push & more.", "target_name": "fluffynuts", "expires_in": "7",
		"contents": "write", "pull_requests": "write", "workflows": "write",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if q := mustQuery(t, NewTokenURL("n", "d", "", 30)); q.Has("target_name") {
		t.Error("an empty owner should leave target_name out, so GitHub uses the user's own account")
	}
}

func mustQuery(t *testing.T, s string) url.Values {
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

// fakeGitHub answers /user for the token "good" (and "expiring", which says
// when it expires) and git-receive-pack for o/pushable (200), o/readonly
// (403) and anything else (404).
func fakeGitHub(t *testing.T) Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer good" && auth != "Bearer expiring" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if auth == "Bearer expiring" {
				w.Header().Set("GitHub-Authentication-Token-Expiration", "2030-01-02 03:04:05 UTC")
			}
			w.Write([]byte(`{"login":"someone"}`))
		case strings.HasSuffix(r.URL.Path, ".git/info/refs"):
			if r.URL.Query().Get("service") != "git-receive-pack" {
				t.Errorf("asked for %s, not git-receive-pack", r.URL)
			}
			if user, pass, ok := r.BasicAuth(); !ok || user != "x-access-token" || pass != "good" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch r.URL.Path {
			case "/o/pushable.git/info/refs":
				w.WriteHeader(http.StatusOK)
			case "/o/readonly.git/info/refs":
				w.WriteHeader(http.StatusForbidden)
			case "/o/moved.git/info/refs":
				http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	return Checker{APIBase: srv.URL, GitBase: srv.URL}
}

func TestCheck(t *testing.T) {
	c := fakeGitHub(t)

	rep := c.Check("bad", nil)
	if !rep.Rejected || len(rep.Problems()) != 1 {
		t.Errorf("a token GitHub turns down: %+v", rep)
	}

	rep = c.Check("good", nil)
	if rep.Rejected || rep.Login != "someone" || !rep.Expires.IsZero() || len(rep.Problems()) != 0 {
		t.Errorf("a good token, no repository: %+v %v", rep, rep.Problems())
	}

	rep = c.Check("expiring", nil)
	if want := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC); !rep.Expires.Equal(want) {
		t.Errorf("Expires = %v, want %v", rep.Expires, want)
	}

	for name, want := range map[string]struct{ sees, push bool }{
		"pushable": {true, true},
		"readonly": {true, false},
		"other":    {false, false},
	} {
		rep := c.Check("good", &Repo{Owner: "o", Name: name})
		if rep.SeesRepo != want.sees || rep.CanPush != want.push {
			t.Errorf("%s: sees %v, push %v; want %+v", name, rep.SeesRepo, rep.CanPush, want)
		}
		if got := len(rep.Problems()) == 0; got != want.push {
			t.Errorf("%s: problems %v", name, rep.Problems())
		}
	}

	rep = c.Check("good", &Repo{Owner: "o", Name: "moved"})
	if rep.RepoUnreachable == nil || rep.CanPush {
		t.Errorf("a redirect should be reported, not followed: %+v", rep)
	}
}

func TestCheckUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	rep := Checker{APIBase: srv.URL, GitBase: srv.URL}.Check("good", nil)
	if rep.Unreachable == nil || len(rep.Problems()) != 1 {
		t.Errorf("an unreachable GitHub: %+v", rep)
	}
}

func TestInstructions(t *testing.T) {
	plain := Instructions(nil, nil, time.Time{})
	if !strings.Contains(plain, "Never force-push") {
		t.Error("the agent must always be told never to force-push")
	}
	if strings.Contains(plain, "feature branch") || strings.Contains(plain, "default branch (main/master)") {
		t.Error("rules not asked for were written")
	}

	got := Instructions(&Repo{Owner: "o", Name: "r", SSH: true}, []string{RuleFeatureBranch, RuleNoDefaultBranch}, time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC))
	for _, want := range []string{"o/r", "git push https://github.com/o/r.git", "feature branch", "Never push the default branch"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions lack %q:\n%s", want, got)
		}
	}
	if !strings.Contains(KitInstructions, SandboxFile) {
		t.Error("the kit's instructions must point the agent at the file")
	}
}

func TestValidRules(t *testing.T) {
	got, err := ValidRules([]string{" no-default-branch", "", "feature-branch", "feature-branch"})
	if err != nil || strings.Join(got, ",") != "feature-branch,no-default-branch" {
		t.Errorf("ValidRules = %v, %v", got, err)
	}
	if _, err := ValidRules([]string{"yolo"}); err == nil {
		t.Error("an unknown rule was accepted")
	}
}

func TestSaveLoadRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git", "sandboxes", "x.token")
	if _, ok, err := Load(path); ok || err != nil {
		t.Fatalf("Load of nothing = %v, %v", ok, err)
	}
	if err := Save(path, "github_pat_one"); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, "github_pat_two"); err != nil {
		t.Fatal(err)
	}
	token, ok, err := Load(path)
	if token != "github_pat_two" || !ok || err != nil {
		t.Errorf("Load = %q, %v, %v", token, ok, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("token file mode = %v, %v; want 0600", info.Mode().Perm(), err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left temporary files behind: %v", entries)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Errorf("removing what's already gone: %v", err)
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	never := time.Time{}
	if Expired(never, now) || Expiring(never, now) || DescribeExpiry(never, now) != "" {
		t.Error("a token with no expiry is neither expired nor expiring, and has nothing to say")
	}
	for _, tt := range []struct {
		in                time.Duration
		expired, expiring bool
		says              string
	}{
		{-time.Hour, true, true, "expired "},
		{0, true, true, "expired "},
		{20 * time.Minute, false, true, "(in under an hour)"},
		{5 * time.Hour, false, true, "(in 5 hours)"},
		{30 * time.Hour, false, true, "(in 1 day)"},
		{2*24*time.Hour + 23*time.Hour, false, true, "(in 3 days)"},
		{3 * 24 * time.Hour, false, false, "(in 3 days)"},
		{7 * 24 * time.Hour, false, false, "(in 7 days)"},
	} {
		expires := now.Add(tt.in)
		if got := Expired(expires, now); got != tt.expired {
			t.Errorf("%v: Expired = %v", tt.in, got)
		}
		if got := Expiring(expires, now); got != tt.expiring {
			t.Errorf("%v: Expiring = %v", tt.in, got)
		}
		if got := DescribeExpiry(expires, now); !strings.Contains(got, tt.says) {
			t.Errorf("%v: DescribeExpiry = %q, want it to say %q", tt.in, got, tt.says)
		}
	}
}

func TestSaveLoadExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.token")
	if err := Save(path, "github_pat_x"); err != nil {
		t.Fatal(err)
	}
	if got := LoadExpiry(path); !got.IsZero() {
		t.Errorf("no expiry saved, but LoadExpiry = %v", got)
	}
	when := time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)
	if err := SaveExpiry(path, when); err != nil {
		t.Fatal(err)
	}
	if got := LoadExpiry(path); !got.Equal(when) {
		t.Errorf("LoadExpiry = %v, want %v", got, when)
	}
	if err := SaveExpiry(path, time.Time{}); err != nil || !LoadExpiry(path).IsZero() {
		t.Errorf("a zero expiry should forget the one kept: %v", err)
	}
	SaveExpiry(path, when)
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 0 {
		t.Errorf("Remove left %v behind", entries)
	}
}

func TestInstructionsTellTheAgentWhatToDoWhenTheTokenDies(t *testing.T) {
	got := Instructions(nil, nil, time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC))
	for _, want := range []string{"expires 2030-01-02 03:04 UTC", "vibe --git-token", "Don't look for other credentials"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions lack %q:\n%s", want, got)
		}
	}
	if strings.Contains(Instructions(nil, nil, time.Time{}), "The token expires") {
		t.Error("an unknown expiry was written")
	}
}

// TestCheckBreadth checks a fine-grained token that can push to its
// repository is checked for the owner's other repositories it was given:
// private ones it can see at all, public ones it can push to. The
// account's repository list holds every repository the token can read —
// every public one, other owners' included — so that alone is no measure.
func TestCheckBreadth(t *testing.T) {
	oldPages, oldLimit := listPages, probeLimit
	t.Cleanup(func() { listPages, probeLimit = oldPages, oldLimit })

	// The list is two pages: the first a full hundred (the target, an
	// organisation's public repository, and the owner's public pub0..pub97),
	// the second the last public one and a private one.
	page1 := []string{`{"name":"r","owner":{"login":"O"}}`, `{"name":"big","owner":{"login":"apache"}}`}
	for i := 0; i < 98; i++ {
		page1 = append(page1, fmt.Sprintf(`{"name":"pub%d","owner":{"login":"o"}}`, i))
	}
	page2 := `[{"name":"pub98","owner":{"login":"o"}},{"name":"secret","private":true,"owner":{"login":"o"}}]`
	// What each token can push to, and which private repositories it sees.
	pushes := map[string][]string{
		"github_pat_narrow":  {"o/r"},
		"github_pat_late":    {"o/r", "o/pub98"},
		"github_pat_private": {"o/r"},
		"github_pat_failing": {"o/r"},
	}
	seesSecret := map[string]bool{"github_pat_private": true}
	var mu sync.Mutex
	tried := map[string]int{}
	listed := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, pass, ok := r.BasicAuth(); ok {
			token = pass
		}
		switch {
		case r.URL.Path == "/user":
			w.Write([]byte(`{"login":"o"}`))
		case r.URL.Path == "/user/repos":
			mu.Lock()
			listed[token] = true
			mu.Unlock()
			switch {
			case token == "github_pat_failing":
				w.WriteHeader(http.StatusForbidden)
			case r.URL.Query().Get("page") == "1":
				w.Write([]byte("[" + strings.Join(page1, ",") + "]"))
			case seesSecret[token]:
				w.Write([]byte(page2))
			default:
				w.Write([]byte(`[{"name":"pub98","owner":{"login":"o"}}]`))
			}
		case strings.HasSuffix(r.URL.Path, ".git/info/refs"):
			repo := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".git/info/refs"))
			if !strings.HasPrefix(repo, "o/") {
				t.Errorf("%s: tried %s, which isn't the owner's", token, repo)
			}
			if repo == "o/secret" {
				t.Errorf("%s: tried pushing to a private repository, whose being seen says enough", token)
			}
			if repo != "o/r" {
				mu.Lock()
				tried[token]++
				mu.Unlock()
			}
			for _, p := range pushes[token] {
				if p == repo {
					return
				}
			}
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	}))
	defer srv.Close()
	c := Checker{APIBase: srv.URL, GitBase: srv.URL}
	repo := &Repo{Owner: "o", Name: "r"}

	for token, want := range map[string]string{
		"github_pat_narrow":  "",
		"github_pat_failing": "",
		"github_pat_late":    "the token reaches other repositories of o too (o/pub98)",
		"github_pat_private": "the token reaches other repositories of o too (o/secret)",
	} {
		rep := c.Check(token, repo)
		got := rep.TooBroad()
		if want == "" && got != "" || want != "" && !strings.HasPrefix(got, want) {
			t.Errorf("%s: TooBroad = %q, want %q", token, got, want)
		}
		if rep.Untried != 0 {
			t.Errorf("%s: %d left untried", token, rep.Untried)
		}
		if len(rep.Problems()) != 0 {
			t.Errorf("%s: breadth made a problem of it: %v", token, rep.Problems())
		}
	}
	if tried["github_pat_narrow"] != 99 {
		t.Errorf("tried %d of the owner's 99 other public repositories", tried["github_pat_narrow"])
	}

	probeLimit = 10
	if rep := c.Check("github_pat_narrow", repo); rep.Untried != 89 {
		t.Errorf("with a limit of 10, Untried = %d, want 89", rep.Untried)
	}

	c.Check("github_pat_cantpush", repo)
	c.Check("ghp_classic", repo)
	if listed["github_pat_cantpush"] || listed["ghp_classic"] {
		t.Error("looked for other repositories for a token that can't push, or isn't fine-grained")
	}
}

func TestTooBroadListsAFew(t *testing.T) {
	rep := Report{Repo: &Repo{Owner: "o", Name: "r"}, OtherRepos: []string{"o/a", "o/b", "o/c", "o/d", "o/e", "o/f", "o/g"}}
	if got, want := rep.TooBroad(), "the token reaches other repositories of o too (o/a, o/b, o/c, o/d, o/e and 2 more): only o/r should be selected"; got != want {
		t.Errorf("TooBroad = %q, want %q", got, want)
	}
}
