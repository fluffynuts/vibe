package companion

import (
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func errIn(why string) error { return errors.New(why) }

// newRepo is a repository just made with git init, on master: no remote,
// no commits.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "new")
	gitOut(t, filepath.Dir(dir), "init", "-q", "-b", "master", dir)
	return dir
}

// remembered stands in for vibe's own settings: the name suggested for a
// default branch, and what the page last set.
type remembered struct {
	name string
	set  []string
}

func withMemory(s *Server, m *remembered) *Server {
	s.cfg.PreferredDefaultBranch = func() string { return m.name }
	s.cfg.RememberDefaultBranch = func(name string) error {
		m.set = append(m.set, name)
		m.name = name
		return nil
	}
	return s
}

type defaultBranchState struct {
	Name      string   `json:"name"`
	Set       bool     `json:"set"`
	Why       string   `json:"why"`
	Branches  []string `json:"branches"`
	Preferred string   `json:"preferred"`
}

func defaultBranchNow(t *testing.T, s *Server) (d defaultBranchState) {
	t.Helper()
	rec := do(t, s, req{path: "/api/default-branch", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("default branch: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func configured(t *testing.T, dir string) string {
	out, _ := exec.Command("git", "-C", dir, "config", "vibe.defaultBranch").Output()
	return strings.TrimSpace(string(out))
}

func TestWhenOriginCantSayThePageIsToldWhyWithSuggestions(t *testing.T) {
	work := newRepo(t)
	s := withMemory(freshServer(t, freshSetup{work: work}), &remembered{name: "master"})
	d := defaultBranchNow(t, s)
	if d.Name != "" || !strings.Contains(d.Why, "no origin remote") || d.Preferred != "master" {
		t.Errorf("default branch = %+v", d)
	}
}

func TestSettingTheDefaultBranchKeepsItInTheRepositoryAndRemembersIt(t *testing.T) {
	work := newRepo(t)
	m := &remembered{name: "master"}
	s := withMemory(freshServer(t, freshSetup{work: work}), m)
	if code, out := send(t, s, "/api/default-branch", `{"name":"main"}`); code != http.StatusOK {
		t.Fatalf("set: %d %v", code, out)
	}
	if got := configured(t, work); got != "main" {
		t.Errorf("the repository's config says %q", got)
	}
	if strings.Join(m.set, ",") != "main" {
		t.Errorf("remembered %v", m.set)
	}
	if d := defaultBranchNow(t, s); d.Name != "main" || !d.Set || d.Preferred != "main" {
		t.Errorf("default branch = %+v", d)
	}
}

func TestADefaultBranchThatIsntABranchNameIsRefusedAndNothingIsKept(t *testing.T) {
	for _, name := range []string{"", "a b", "-x", "feat/", "a..b"} {
		t.Run(name, func(t *testing.T) {
			work := newRepo(t)
			m := &remembered{name: "master"}
			s := withMemory(freshServer(t, freshSetup{work: work}), m)
			body, _ := json.Marshal(map[string]string{"name": name})
			if code, out := send(t, s, "/api/default-branch", string(body)); code != http.StatusBadRequest || out["error"] == "" {
				t.Errorf("set: %d %v", code, out)
			}
			if got := configured(t, work); got != "" || len(m.set) != 0 {
				t.Errorf("kept %q, remembered %v", got, m.set)
			}
		})
	}
}

// What origin says wins: setting one then isn't offered, and is refused.
func TestTheDefaultBranchCantBeSetWhenOriginSays(t *testing.T) {
	work, _ := repoWithOrigin(t)
	m := &remembered{}
	s := withMemory(freshServer(t, freshSetup{work: work}), m)
	if code, out := send(t, s, "/api/default-branch", `{"name":"trunk"}`); code != http.StatusConflict || !strings.Contains(out["error"].(string), "master") {
		t.Errorf("set: %d %v", code, out)
	}
	if got := configured(t, work); got != "" || len(m.set) != 0 {
		t.Errorf("kept %q, remembered %v", got, m.set)
	}
}

func TestASandboxThatWontWakeCantSetTheDefaultBranch(t *testing.T) {
	work := newRepo(t)
	m := &remembered{}
	s := withMemory(freshServer(t, freshSetup{work: work, wake: errIn("the sandbox didn't start")}), m)
	if code, out := send(t, s, "/api/default-branch", `{"name":"main"}`); code != http.StatusConflict || !strings.Contains(out["error"].(string), "didn't start") {
		t.Errorf("set: %d %v", code, out)
	}
	if len(m.set) != 0 {
		t.Errorf("remembered %v", m.set)
	}
}

func TestTheDialogOffersToBranchFromASetDefaultBranch(t *testing.T) {
	work := newRepo(t)
	commitIn(t, work, "a.txt", "one\n")
	gitOut(t, work, "config", "vibe.defaultBranch", "master")
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	s := freshServer(t, freshSetup{work: work})
	o := options(t, s)
	if o.Branch.Default != "master" || !o.Branch.DefaultSet || o.Branch.DefaultEmpty {
		t.Errorf("branch = %+v", o.Branch)
	}
}

func TestASetDefaultBranchWithNoCommitsIsSaidToBeEmpty(t *testing.T) {
	work := newRepo(t)
	gitOut(t, work, "config", "vibe.defaultBranch", "main")
	s := freshServer(t, freshSetup{work: work})
	if o := options(t, s); o.Branch.Default != "main" || !o.Branch.DefaultEmpty {
		t.Errorf("branch = %+v", o.Branch)
	}
}

func TestBranchingFromASetDefaultBranchStartsAtTheLocalOne(t *testing.T) {
	work := newRepo(t)
	commitIn(t, work, "a.txt", "one\n")
	gitOut(t, work, "config", "vibe.defaultBranch", "master")
	gitOut(t, work, "switch", "-q", "-c", "feat/a")
	commitIn(t, work, "a-only.txt", "a\n")
	s := freshServer(t, freshSetup{work: work})
	if code, out := send(t, s, "/api/conversation/new", `{"branch":{"name":"feat/x","from":"default"}}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if head, want := gitOut(t, work, "rev-parse", "HEAD"), gitOut(t, work, "rev-parse", "master"); head != want {
		t.Errorf("feat/x is at %s, want master's %s", head, want)
	}
	said := strings.Join(eventsAfterReset(t, s), "\n")
	if !strings.Contains(said, "new branch feat/x, from master.") {
		t.Errorf("the conversation says:\n%s", said)
	}
}

func TestBranchingFromASetDefaultBranchWithNoCommitsIsRefused(t *testing.T) {
	work := newRepo(t)
	gitOut(t, work, "config", "vibe.defaultBranch", "main")
	s := freshServer(t, freshSetup{work: work})
	code, out := send(t, s, "/api/conversation/new", `{"branch":{"name":"feat/x","from":"default"}}`)
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "no commits yet") {
		t.Errorf("new: %d %v", code, out)
	}
}

func TestASetDefaultBranchHasNoOriginToCheck(t *testing.T) {
	work := newRepo(t)
	gitOut(t, work, "config", "vibe.defaultBranch", "master")
	s := freshServer(t, freshSetup{work: work})
	if code, _ := send(t, s, "/api/conversation/new/fetch", `{}`); code != http.StatusConflict {
		t.Errorf("fetch: %d", code)
	}
}

// git init's repository has no commits, and its branch isn't one yet:
// branching from here names the branch the first commit will make.
func TestANewBranchCanBeStartedInANewRepository(t *testing.T) {
	work := newRepo(t)
	s := freshServer(t, freshSetup{work: work})
	if code, out := send(t, s, "/api/conversation/new", `{"branch":{"name":"feat/x","from":"here"}}`); code != http.StatusOK {
		t.Fatalf("new: %d %v", code, out)
	}
	if b := currentBranch(t, work); b != "feat/x" {
		t.Errorf("on %q", b)
	}
}
