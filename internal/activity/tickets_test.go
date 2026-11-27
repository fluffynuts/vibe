package activity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vibe/internal/review"
)

// links are the links tk keeps, which must be readable.
func links(t *testing.T, tk *Tickets) map[string]string {
	t.Helper()
	all, err := tk.All()
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func TestATicketLinkIsKeptByRepositoryAndBranchAndCanBeCleared(t *testing.T) {
	tk := Tickets{Path: filepath.Join(t.TempDir(), "tickets.json")}
	if err := tk.Set("yumbi-web", "fix/x", "https://trello.com/c/abc"); err != nil {
		t.Fatal(err)
	}
	if got := links(t, &tk)[TicketKey("yumbi-web", "fix/x")]; got != "https://trello.com/c/abc" {
		t.Fatalf("want the link kept, got %q", got)
	}
	again := Tickets{Path: tk.Path}
	if got := links(t, &again)[TicketKey("yumbi-web", "fix/x")]; got != "https://trello.com/c/abc" {
		t.Fatalf("want it read back by another vibe, got %q", got)
	}
	if err := tk.Set("yumbi-web", "fix/x", ""); err != nil {
		t.Fatal(err)
	}
	if len(links(t, &tk)) != 0 {
		t.Fatalf("want an empty link to clear it, got %v", links(t, &tk))
	}
}

func TestATicketLinkIsAWebAddress(t *testing.T) {
	tk := Tickets{Path: filepath.Join(t.TempDir(), "tickets.json")}
	for _, bad := range []string{"javascript:alert(1)", "trello.com/c/abc", "ftp://x/y", "https://", "https://x/" + strings.Repeat("a", 2100), "https://x/ y"} {
		if err := tk.Set("r", "b", bad); err == nil {
			t.Errorf("want %q refused", bad)
		}
	}
	for _, good := range []string{"https://trello.com/c/abc/12-name", "http://jira.local/browse/ABC-1", "https://github.com/o/r/issues/3"} {
		if err := tk.Set("r", "b", good); err != nil {
			t.Errorf("want %q kept: %v", good, err)
		}
	}
}

// ticketDesk is a desk over one repository's checkouts (each a folder with its local and origin branches, or an error), whose log
// mentions mentioned this week.
func ticketDesk(t *testing.T, checkouts map[string]review.GitHistory, errs map[string]error, mentioned ...string) (*Desk, *Tickets) {
	t.Helper()
	l := Log{Dir: t.TempDir()}
	for _, b := range mentioned {
		l.Append("s", Mark{At: at("09:00"), Folder: "/elsewhere", Repo: "yumbi-web", Branch: b, Kind: KindAgent})
	}
	var folders []string
	for f := range checkouts {
		folders = append(folders, f)
	}
	for f := range errs {
		folders = append(folders, f)
	}
	tk := &Tickets{Path: filepath.Join(t.TempDir(), "tickets.json")}
	f := &fakeGit{histories: checkouts, errs: errs}
	d := f.desk(l, folders...)
	d.Tickets = tk
	return d, tk
}

func aCheckout(local, remote []string) review.GitHistory {
	return review.GitHistory{Origin: "git@github.com:codeo/yumbi-web.git", Branches: local, RemoteBranches: remote}
}

func TestATicketLinkIsForgottenOnlyWhenItsBranchIsGoneEverywhereAndUnmentionedAllWeek(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	d, tk := ticketDesk(t, map[string]review.GitHistory{
		a: aCheckout([]string{"master", "stalled"}, []string{"master", "on-origin"}),
		b: aCheckout([]string{"master", "in-the-other-checkout"}, nil),
	}, nil, "deleted-but-worked-on-this-week")
	for _, branch := range []string{"gone", "stalled", "on-origin", "in-the-other-checkout", "deleted-but-worked-on-this-week"} {
		tk.Set("yumbi-web", branch, "https://trello.com/c/"+strings.ReplaceAll(branch, "-", ""))
	}
	tk.Set("phoenix", "gone", "https://trello.com/c/phoenix")
	if _, err := d.Day(context.Background(), "", 30*60e9); err != nil {
		t.Fatal(err)
	}
	var kept []string
	for k := range links(t, tk) {
		kept = append(kept, k)
	}
	want := []string{TicketKey("yumbi-web", "stalled"), TicketKey("yumbi-web", "on-origin"), TicketKey("yumbi-web", "in-the-other-checkout"),
		TicketKey("yumbi-web", "deleted-but-worked-on-this-week"), TicketKey("phoenix", "gone")}
	if !sameSet(kept, want) {
		t.Fatalf("want only yumbi-web's gone branch forgotten (phoenix has no checkout to tell by), kept %q", kept)
	}
}

func TestATicketLinkIsKeptWhenACheckoutCouldntBeRead(t *testing.T) {
	a, broken := t.TempDir(), t.TempDir()
	d, tk := ticketDesk(t, map[string]review.GitHistory{a: aCheckout([]string{"master"}, nil)}, map[string]error{broken: errors.New("damaged")})
	// The broken checkout's repository can't be told from its folder: it is named by the history it couldn't give.
	d.Folders = func() []string { return []string{a, broken} }
	tk.Set("yumbi-web", "gone", "https://trello.com/c/x")
	d.Day(context.Background(), "", 30*60e9)
	if len(links(t, tk)) != 1 {
		t.Fatalf("want the link kept while a checkout can't be read, got %v", links(t, tk))
	}
}

func TestTheDaysBranchesCarryTheirTicketLinks(t *testing.T) {
	a := t.TempDir()
	d, tk := ticketDesk(t, map[string]review.GitHistory{a: aCheckout([]string{"fix/x"}, nil)}, nil, "fix/x")
	tk.Set("yumbi-web", "fix/x", "https://trello.com/c/abc")
	r, err := d.Day(context.Background(), "", 30*60e9)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range r.Day.Branches {
		got = append(got, b.Branch+" "+b.Ticket)
	}
	if !reflect.DeepEqual(got, []string{"fix/x https://trello.com/c/abc"}) {
		t.Fatalf("got %v", got)
	}
}

func TestATicketsFileThatCantBeReadIsNeverWrittenOverAndIsSaidToBe(t *testing.T) {
	tk := Tickets{Path: filepath.Join(t.TempDir(), "tickets.json")}
	broken := []byte(`[{"repo": "vibe", "branch": "feat/a", "url": "https://trello.com/c/a"}, {"repo": `)
	if err := os.WriteFile(tk.Path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tk.Set("vibe", "feat/b", "https://trello.com/c/b"); err == nil || !strings.Contains(err.Error(), "tickets.json") {
		t.Fatalf("want a link not kept over a file that can't be read, saying which, got %v", err)
	}
	if err := tk.forget(func(string, string) bool { return true }); err == nil {
		t.Fatal("want nothing forgotten from a file that can't be read")
	}
	if data, _ := os.ReadFile(tk.Path); string(data) != string(broken) {
		t.Fatalf("want the file left as it was, got %q", data)
	}
	if _, err := tk.All(); err == nil {
		t.Fatal("want reading it to say it couldn't")
	}
	d := (&fakeGit{}).desk(Log{Dir: t.TempDir()})
	d.Tickets = &tk
	r, err := d.Day(context.Background(), "", 30*time.Minute)
	if err != nil || !strings.Contains(strings.Join(r.Problems, "\n"), "ticket links") {
		t.Fatalf("want the day to say the links couldn't be read, got %v, %q", err, r.Problems)
	}
}

func TestNoTicketsFileIsNoLinks(t *testing.T) {
	tk := Tickets{Path: filepath.Join(t.TempDir(), "none", "tickets.json")}
	if all, err := tk.All(); err != nil || len(all) != 0 {
		t.Fatalf("want no links and no trouble, got %v, %v", all, err)
	}
}
