package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"vibe/internal/companion"
	"vibe/internal/userprefs"
)

// opencodeSaysModels is a sandbox's OpenCode asked for its models: it prints the real list of free models
// (docs/samples/opencode/models-verbose.txt) and a paid one of a provider sbx can hold a key for, and records what it
// was asked.
func opencodeSaysModels(t *testing.T, extra string) (ask askAgent, scripts *[]string) {
	t.Helper()
	data, err := os.ReadFile("../../docs/samples/opencode/models-verbose.txt")
	if err != nil {
		t.Fatal(err)
	}
	said := string(data) + extra
	scripts = &[]string{}
	return func(_ context.Context, script string) (string, error) {
		*scripts = append(*scripts, script)
		return said, nil
	}, scripts
}

const paidHaiku = `anthropic/claude-haiku-4-5
{"id": "claude-haiku-4-5", "providerID": "anthropic", "name": "Claude Haiku 4.5", "status": "active",
 "cost": {"input": 1, "output": 5}, "capabilities": {"toolcall": true}, "variants": {"high": {}, "max": {}}}
`

const localModel = `ollama/qwen3
{"id": "qwen3", "providerID": "ollama", "name": "Qwen 3", "status": "active",
 "cost": {"input": 0, "output": 0}, "capabilities": {"toolcall": true}}
`

const paidZen = `opencode/big-zen
{"id": "big-zen", "providerID": "opencode", "name": "Big Zen", "status": "active",
 "cost": {"input": 2, "output": 8}, "capabilities": {"toolcall": true}}
`

type opencodeFiles struct {
	kept    profileChoice
	options string
	list    string
	keys    *opencodeKeys
	secrets *string
	reads   *int
}

func newOpencodeChoice(t *testing.T, ask askAgent) (*companion.Choice, opencodeFiles) {
	t.Helper()
	kept, settings := choiceHome(t, "")
	dir := filepath.Dir(settings)
	text := ""
	reads := 0
	keys := newOpencodeKeys("box", func(context.Context) (string, error) { reads++; return text, nil }, time.Now)
	files := opencodeFiles{kept: kept, options: filepath.Join(dir, "options.json"), list: filepath.Join(dir, "models.txt"), keys: keys, secrets: &text, reads: &reads}
	return opencodeChoice(ask, "/work/box", kept, files.options, files.list, keys), files
}

func TestOpencodesListIsAskedForInTheWorkspaceAndOffersEveryModelWithItsOwnEfforts(t *testing.T) {
	ask, scripts := opencodeSaysModels(t, paidHaiku)
	c, _ := newOpencodeChoice(t, ask)
	opts := opencodeAskedFor(t, c)
	if len(*scripts) != 1 || !strings.Contains((*scripts)[0], "cd '/work/box' && ") || !strings.Contains((*scripts)[0], "models --verbose") {
		t.Errorf("asked: %q", *scripts)
	}
	if !slices.Contains(opts.Models, "opencode/big-pickle") || !slices.Contains(opts.Models, "anthropic/claude-haiku-4-5") || len(opts.OtherModels) != 0 {
		t.Errorf("models: %v, others %v", opts.Models, opts.OtherModels)
	}
	if !opts.OnlyListed || opts.Note == "" || opts.Current != "opencode/big-pickle" {
		t.Errorf("onlyListed %v, note %q, current %q", opts.OnlyListed, opts.Note, opts.Current)
	}
	if got := opts.EffortsByModel["opencode/big-pickle"]; got == nil || len(got) != 0 {
		t.Errorf("big-pickle takes no effort: %#v", got)
	}
	if got := opts.EffortsByModel["opencode/beam-free"]; !slices.Equal(got, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("beam-free efforts: %v", got)
	}
	if opts.Names["opencode/beam-free"] != "Beam Free" || opts.Names["anthropic/claude-haiku-4-5"] != "Claude Haiku 4.5 (anthropic)" {
		t.Errorf("names: %q, %q", opts.Names["opencode/beam-free"], opts.Names["anthropic/claude-haiku-4-5"])
	}
	if opts.EffortNames["xhigh"] != "Extra high" {
		t.Errorf("effort names: %v", opts.EffortNames)
	}
}

func TestAListWithNoModelInItIsSaidNotOfferedAsEmptyAndNotKept(t *testing.T) {
	c, files := newOpencodeChoice(t, func(context.Context, string) (string, error) { return "Error: something broke", nil })
	if _, err := c.Options(context.Background()); err == nil || !strings.Contains(err.Error(), "no model") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(files.list); err == nil {
		t.Error("a list with no model in it was kept")
	}
}

func TestAChoiceIsKeptAsTheModelAndTheEffortAndGivenBack(t *testing.T) {
	ask, _ := opencodeSaysModels(t, "")
	c, files := newOpencodeChoice(t, ask)
	opencodeAskedFor(t, c)
	if err := c.Set("opencode/beam-free", "high"); err != nil {
		t.Fatal(err)
	}
	prefs, _ := userprefs.Load(filepath.Dir(files.options))
	if prefs.Model != "opencode/beam-free" || prefs.Effort != "high" {
		t.Errorf("kept %q and %q", prefs.Model, prefs.Effort)
	}
	if model, effort, err := c.Get(); err != nil || model != "opencode/beam-free" || effort != "high" {
		t.Errorf("got %q, %q, %v", model, effort, err)
	}
}

func TestAModelOrEffortOpencodeDoesntHaveIsNotKept(t *testing.T) {
	ask, _ := opencodeSaysModels(t, "")
	c, _ := newOpencodeChoice(t, ask)
	if err := c.Set("opencode/big-pickle", ""); err == nil {
		t.Error("a choice was kept before the list was read")
	}
	opencodeAskedFor(t, c)
	if err := c.Set("opencode/nonsense", ""); err == nil {
		t.Error("a model OpenCode doesn't list was kept")
	}
	if err := c.Set("opencode/big-pickle", "high"); err == nil {
		t.Error("an effort the model doesn't take was kept")
	}
	if err := c.Set("", ""); err != nil {
		t.Errorf("going back to the default failed: %v", err)
	}
}

func TestAKeptModelOpencodeNoLongerListsIsNeitherShownNorRun(t *testing.T) {
	ask, _ := opencodeSaysModels(t, "")
	c, files := newOpencodeChoice(t, ask)
	opencodeAskedFor(t, c)
	if err := files.kept.set("opencode/gone-free", "high"); err != nil {
		t.Fatal(err)
	}
	if model, effort, _ := c.Get(); model != "" || effort != "" {
		t.Errorf("shown %q, %q", model, effort)
	}
	turn, err := opencodeTurnChoice(files.kept, files.list)()
	if err != nil || turn.Model != opencodeFreeModel || turn.Variant != "" {
		t.Errorf("turn: %+v, %v", turn, err)
	}
}

func TestATurnRunsTheModelAndEffortThatWereChosen(t *testing.T) {
	ask, _ := opencodeSaysModels(t, "")
	c, files := newOpencodeChoice(t, ask)
	opencodeAskedFor(t, c)
	if err := c.Set("opencode/beam-free", "xhigh"); err != nil {
		t.Fatal(err)
	}
	turn, err := opencodeTurnChoice(files.kept, files.list)()
	if err != nil || turn.Model != "opencode/beam-free" || turn.Variant != "xhigh" {
		t.Errorf("turn: %+v, %v", turn, err)
	}
}

func TestNothingChosenRunsTheFreeModelAndNothingUnverifiedIsSent(t *testing.T) {
	_, files := newOpencodeChoice(t, nil)
	// No list kept yet, and a model in the profile: it can't be vouched for.
	if err := files.kept.set("opencode/beam-free", "high"); err != nil {
		t.Fatal(err)
	}
	turn, err := opencodeTurnChoice(files.kept, files.list)()
	if err != nil || turn.Model != opencodeFreeModel || turn.Variant != "" {
		t.Errorf("turn: %+v, %v", turn, err)
	}
}

// What a provider's models cost is the page's to show, and whether sbx has a key for the provider is vibe's to know:
// the models whose provider sbx could hold a key for, and has none, are listed apart.
func TestModelsOfAProviderSbxHasNoKeyForAreOutOfReachAndTheRestAreNot(t *testing.T) {
	ask, _ := opencodeSaysModels(t, paidHaiku+localModel+paidZen)
	c, files := newOpencodeChoice(t, ask)
	opencodeAskedFor(t, c)
	*files.secrets = "SCOPE TYPE NAME SECRET\n(global) service github (stored)\n"
	limits := c.Limits()
	if limits == nil || !slices.Equal(limits.Unavailable, []string{"anthropic/claude-haiku-4-5"}) {
		t.Fatalf("limits: %+v", limits)
	}
	if limits.Group != "Needs a key" || !strings.Contains(limits.Why, "sbx secret set") {
		t.Errorf("group %q, why %q", limits.Group, limits.Why)
	}
	// What is missing, and what vibe can see, are said: a mistaken reading of sbx can then be told from a real lack.
	if !strings.Contains(limits.Why, "anthropic") || !strings.Contains(limits.Why, "github") {
		t.Errorf("why doesn't say what is missing and what is there: %q", limits.Why)
	}
	if limits.Unavailable[0] == "ollama/qwen3" || slices.Contains(limits.Unavailable, "opencode/big-zen") {
		t.Errorf("a model whose key can't be told about is marked: %v", limits.Unavailable)
	}
	*files.secrets = "SCOPE TYPE NAME SECRET\n(global) service anthropic (stored)\n"
	// What was read stands for a while; the user stored a key and says to check again.
	if err := c.Unlock(); err != nil {
		t.Fatal(err)
	}
	if limits := c.Limits(); limits != nil {
		t.Errorf("with a key for anthropic, limits: %+v", limits)
	}
}

func TestWhenSbxCantBeAskedAboutKeysNothingIsMarked(t *testing.T) {
	ask, _ := opencodeSaysModels(t, paidHaiku)
	kept, settings := choiceHome(t, "")
	dir := filepath.Dir(settings)
	keys := newOpencodeKeys("box", func(context.Context) (string, error) { return "", os.ErrNotExist }, time.Now)
	c := opencodeChoice(ask, "/w", kept, filepath.Join(dir, "o.json"), filepath.Join(dir, "m.txt"), keys)
	opencodeAskedFor(t, c)
	if limits := c.Limits(); limits != nil {
		t.Errorf("limits with no way to know: %+v", limits)
	}
}

func TestTheKeysAreReadOnceForAWhileAndAgainWhenAskedToCheckAgain(t *testing.T) {
	ask, _ := opencodeSaysModels(t, paidHaiku)
	c, files := newOpencodeChoice(t, ask)
	opencodeAskedFor(t, c)
	c.Limits()
	c.Limits()
	if *files.reads != 1 {
		t.Errorf("sbx was asked %d times", *files.reads)
	}
	if err := c.Unlock(); err != nil {
		t.Fatal(err)
	}
	c.Limits()
	if *files.reads != 2 {
		t.Errorf("after checking again, sbx was asked %d times", *files.reads)
	}
}

func TestAnAskedForModelAnEffortAndAKeyAreAllOnTheCommandLineSafely(t *testing.T) {
	inner := opencodeCommand("/w", "", "/tmp/t.pid", "opencode/big-pickle'; touch /pwned; '", "high")[4]
	if !strings.Contains(inner, `'opencode/big-pickle'\''; touch /pwned; '\'''`) {
		t.Errorf("inner = %s", inner)
	}
}

func opencodeAskedFor(t *testing.T, c *companion.Choice) companion.Options {
	t.Helper()
	opts, err := c.Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return opts
}

func TestTheReasonSaysWhichKeysAreMissingAndWhichAreThere(t *testing.T) {
	more := `openai/gpt-x
{"id": "gpt-x", "providerID": "openai", "name": "GPT X", "status": "active",
 "cost": {"input": 1, "output": 2}, "capabilities": {"toolcall": true}}
`
	ask, _ := opencodeSaysModels(t, paidHaiku+more)
	c, files := newOpencodeChoice(t, ask)
	opencodeAskedFor(t, c)
	*files.secrets = "SCOPE TYPE NAME SECRET\n(global) service github (stored)\n(global) service xai (stored)\n"
	why := c.Limits().Why
	if !strings.Contains(why, "no key for anthropic, openai") || !strings.Contains(why, "github, xai") {
		t.Errorf("why: %q", why)
	}
	*files.secrets = ""
	if err := c.Unlock(); err != nil {
		t.Fatal(err)
	}
	if why := c.Limits().Why; !strings.Contains(why, "sbx has no keys stored") {
		t.Errorf("with none stored: %q", why)
	}
}

// What went wrong on the first real try: sbx had an anthropic secret, the user's Claude login (OAuth), and Anthropic
// refused it as a key ("invalid x-api-key").
func TestASecretThatIsALoginIsNotAKeyAndTheReasonSaysSo(t *testing.T) {
	ask, _ := opencodeSaysModels(t, paidHaiku)
	c, files := newOpencodeChoice(t, ask)
	opencodeAskedFor(t, c)
	*files.secrets = "SCOPE TYPE NAME SECRET\n(global) service anthropic (oauth configured)\n(global) service github (stored)\n"
	limits := c.Limits()
	if limits == nil || !slices.Equal(limits.Unavailable, []string{"anthropic/claude-haiku-4-5"}) {
		t.Fatalf("limits: %+v", limits)
	}
	if !strings.Contains(limits.Why, "anthropic is signed in with OAuth") || !strings.Contains(limits.Why, "can't use") {
		t.Errorf("why: %q", limits.Why)
	}
	if strings.Contains(limits.Why, "has keys for: anthropic") {
		t.Errorf("a login is said to be a key: %q", limits.Why)
	}
	// With a key stored as well, the model is within reach.
	*files.secrets += "(global) service anthropic (stored)\n"
	if err := c.Unlock(); err != nil {
		t.Fatal(err)
	}
	if limits := c.Limits(); limits != nil {
		t.Errorf("with a key stored too: %+v", limits)
	}
}
