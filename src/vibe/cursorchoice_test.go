package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"vibe/internal/companion"
	"vibe/internal/userprefs"
)

// cursorSays is a sandbox's Cursor asked for its models: it prints the real
// list (docs/samples/cursor/list-models.txt), and records what it was asked.
func cursorSays(t *testing.T, said string) (ask askAgent, scripts *[]string) {
	t.Helper()
	if said == "" {
		data, err := os.ReadFile("../../docs/samples/cursor/list-models.txt")
		if err != nil {
			t.Fatal(err)
		}
		said = string(data)
	}
	scripts = &[]string{}
	return func(_ context.Context, script string) (string, error) {
		*scripts = append(*scripts, script)
		return said, nil
	}, scripts
}

type cursorFiles struct {
	kept    profileChoice
	options string
	list    string
	plan    *cursorPlan
	now     *time.Time
}

func newCursorChoice(t *testing.T, ask askAgent) (*companion.Choice, cursorFiles) {
	t.Helper()
	kept, settings := choiceHome(t, "")
	dir := filepath.Dir(settings)
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	plan := newCursorPlan(filepath.Join(dir, "plan.json"))
	plan.now = func() time.Time { return now }
	files := cursorFiles{kept: kept, options: filepath.Join(dir, "options.json"), list: filepath.Join(dir, "models.txt"), plan: plan, now: &now}
	return cursorChoice(ask, "/work/box", kept, files.options, files.list, plan), files
}

func askedFor(t *testing.T, c *companion.Choice) companion.Options {
	t.Helper()
	opts, err := c.Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return opts
}

func TestCursorsListIsAskedForInTheWorkspaceAndOffersFamiliesWithTheirEfforts(t *testing.T) {
	ask, scripts := cursorSays(t, "")
	c, _ := newCursorChoice(t, ask)
	opts := askedFor(t, c)
	if len(*scripts) != 1 || !strings.Contains((*scripts)[0], "cd '/work/box' && ") || !strings.Contains((*scripts)[0], "--list-models") {
		t.Errorf("asked: %q", *scripts)
	}
	if opts.Models[0] != "auto" || len(opts.Models) < 40 || len(opts.OtherModels) != 0 {
		t.Errorf("models: %v (%d), others %v", opts.Models[:3], len(opts.Models), opts.OtherModels)
	}
	if !opts.OnlyListed || opts.Note == "" {
		t.Errorf("onlyListed %v, note %q", opts.OnlyListed, opts.Note)
	}
	if got := opts.EffortsByModel["auto"]; got == nil || len(got) != 0 {
		t.Errorf("auto's efforts should be an empty list, not missing: %#v", got)
	}
	if got := opts.EffortsByModel["gpt-5.3-codex"]; len(got) != 7 || got[0] != "fast" {
		t.Errorf("codex's efforts: %v", got)
	}
	if opts.Names["gpt-5.3-codex"] != "Codex 5.3" || opts.EffortNames["highfast"] != "High effort · Fast" {
		t.Errorf("names: %q, %q", opts.Names["gpt-5.3-codex"], opts.EffortNames["highfast"])
	}
}

func TestAListWithNoModelInItIsSaidNotOfferedAsEmpty(t *testing.T) {
	ask, _ := cursorSays(t, "Not logged in")
	c, files := newCursorChoice(t, ask)
	if _, err := c.Options(context.Background()); err == nil || !strings.Contains(err.Error(), "no model") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(files.list); err == nil {
		t.Error("a list with no model in it was kept")
	}
}

func TestAChoiceKeepsCursorsOwnIdAndThePageGetsFamilyAndEffortBack(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	if err := c.Set("gpt-5.6-sol", "highfast"); err != nil {
		t.Fatal(err)
	}
	prefs, _ := userprefs.Load(filepath.Dir(files.options))
	if prefs.Model != "gpt-5.6-sol-high-fast" || prefs.Effort != "" {
		t.Errorf("kept %q and %q", prefs.Model, prefs.Effort)
	}
	if model, effort, err := c.Get(); err != nil || model != "gpt-5.6-sol" || effort != "highfast" {
		t.Errorf("got %q, %q, %v", model, effort, err)
	}
	if model, effort, _ := c.Own(); model != "gpt-5.6-sol" || effort != "highfast" {
		t.Errorf("own %q, %q", model, effort)
	}
}

func TestADefaultEffortIsKeptAsTheIdItMeans(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	if err := c.Set("claude-opus-5-thinking", ""); err != nil {
		t.Fatal(err)
	}
	if prefs, _ := userprefs.Load(filepath.Dir(files.options)); prefs.Model != "claude-opus-5-thinking-high" {
		t.Errorf("kept %q", prefs.Model)
	}
	if err := c.Set("auto", ""); err != nil {
		t.Fatal(err)
	}
	if prefs, _ := userprefs.Load(filepath.Dir(files.options)); prefs.Model != "auto" {
		t.Errorf("kept %q", prefs.Model)
	}
}

func TestAChoiceCursorHasNoSuchModelForIsRefusedAndNothingIsKept(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	for _, tc := range [][2]string{{"gpt-5.3-codex", "max"}, {"auto", "high"}, {"opus", ""}} {
		if err := c.Set(tc[0], tc[1]); err == nil {
			t.Errorf("%v was kept", tc)
		}
	}
	if prefs, _ := userprefs.Load(filepath.Dir(files.options)); prefs.Model != "" {
		t.Errorf("kept %q", prefs.Model)
	}
}

func TestNothingCanBeChosenBeforeCursorsListHasBeenRead(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, _ := newCursorChoice(t, ask)
	if err := c.Set("auto", ""); err == nil || !strings.Contains(err.Error(), "list of models") {
		t.Errorf("err = %v", err)
	}
}

func TestChoosingDefaultTakesTheChoiceAway(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	c.Set("grok-4.7", "high")
	if err := c.Set("", ""); err != nil {
		t.Fatal(err)
	}
	if prefs, _ := userprefs.Load(filepath.Dir(files.options)); prefs.Model != "" {
		t.Errorf("kept %q", prefs.Model)
	}
}

func TestAModelCursorHasNoneOfIsNotShownAsChosenNorRun(t *testing.T) {
	// Left by another agent (a claude alias, say, from before the profile was
	// switched to cursor), or by a hand.
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	if err := files.kept.set("opus", "high"); err != nil {
		t.Fatal(err)
	}
	if model, effort, _ := c.Get(); model != "" || effort != "" {
		t.Errorf("shown as chosen: %q, %q", model, effort)
	}
	if id, err := cursorTurnModel(files.kept, files.list)(); id != "" || err != nil {
		t.Errorf("run with %q, %v", id, err)
	}
}

func TestTheModelAChoiceMeansIsTheOneATurnIsRunWith(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	c.Set("gemini-3.7-flash", "low")
	if id, err := cursorTurnModel(files.kept, files.list)(); id != "gemini-3.7-flash-low" || err != nil {
		t.Errorf("got %q, %v", id, err)
	}
}

func TestWithoutCursorsListNoModelIsNamedForATurn(t *testing.T) {
	// A model that can't be vouched for is better left to Cursor.
	_, files := newCursorChoice(t, nil)
	files.kept.set("gpt-5.2-fast", "")
	if id, err := cursorTurnModel(files.kept, files.list)(); id != "" || err != nil {
		t.Errorf("got %q, %v", id, err)
	}
}

func TestAModelThatCouldBeShellIsRefusedForATurnNamingTheFileToFix(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	files.kept.set("x; touch /pwned", "")
	if _, err := cursorTurnModel(files.kept, files.list)(); err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Errorf("err = %v", err)
	}
}

func TestCursorsListsAreKeptInTheOrderCursorGaveThem(t *testing.T) {
	// Not claude's order (by the size of its families).
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	opts := askedFor(t, c)
	if err := c.Remember(opts); err != nil {
		t.Fatal(err)
	}
	cached, _, ok := c.Cached()
	if !ok || cached.Models[0] != "auto" || cached.Models[1] != opts.Models[1] {
		t.Errorf("kept: %v", cached.Models)
	}
	_ = files
}

func TestAFailureToAskCursorForItsListIsPassedOn(t *testing.T) {
	c, _ := newCursorChoice(t, func(context.Context, string) (string, error) { return "", errors.New("sandbox is stopped") })
	if _, err := c.Options(context.Background()); err == nil || !strings.Contains(err.Error(), "sandbox is stopped") {
		t.Errorf("err = %v", err)
	}
}

func TestSettingTheChoiceBackToAutoIsWhatTheSelectorShowsAndTheNextTurnAsksFor(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	if err := c.Set("claude-opus-5-5", "high"); err != nil {
		t.Fatal(err)
	}
	// What a turn that fell back to Auto does (see conversationFor).
	if err := files.kept.set(cursorAuto, ""); err != nil {
		t.Fatal(err)
	}
	if model, effort, err := c.Get(); err != nil || model != "auto" || effort != "" {
		t.Errorf("the selector shows %q, %q, %v", model, effort, err)
	}
	if id, err := cursorTurnModel(files.kept, files.list)(); id != "auto" || err != nil {
		t.Errorf("the next turn asks for %q, %v", id, err)
	}
}

func TestNothingIsOutOfReachUntilThePlanHasRefusedAModel(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, _ := newCursorChoice(t, ask)
	askedFor(t, c)
	if limits := c.Limits(); limits != nil {
		t.Errorf("limits = %+v", limits)
	}
}

func TestOncePlanHasRefusedAModelEveryModelButAutoIsOutOfReachAndWhy(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	opts := askedFor(t, c)
	files.plan.learnedOnlyAuto()
	limits := c.Limits()
	if limits == nil || limits.Why == "" || limits.Since != files.now.UnixMilli() {
		t.Fatalf("limits = %+v", limits)
	}
	if len(limits.Unavailable) != len(opts.Models)-1 {
		t.Errorf("%d out of reach, of %d models", len(limits.Unavailable), len(opts.Models))
	}
	for _, m := range limits.Unavailable {
		if m == "auto" {
			t.Error("Auto, which every plan runs, was put out of reach")
		}
	}
	if !slices.Contains(limits.Unavailable, "gpt-5.3-codex") {
		t.Error("a model that a free plan can't run isn't out of reach")
	}
}

func TestWhatWasLearnedOfThePlanExpiresAndThenEverythingIsOfferedAgain(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	files.plan.learnedOnlyAuto()
	*files.now = files.now.Add(cursorPlanFreshFor + time.Minute)
	if limits := c.Limits(); limits != nil {
		t.Errorf("limits an hour old: %+v", limits)
	}
}

func TestSayingThePlanHasChangedTakesTheLimitsAwayAndKeepsNothingOfThem(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	files.plan.learnedOnlyAuto()
	if err := c.Unlock(); err != nil {
		t.Fatal(err)
	}
	if limits := c.Limits(); limits != nil {
		t.Errorf("limits = %+v", limits)
	}
}

func TestWithoutCursorsListNothingCanBeNamedAsOutOfReach(t *testing.T) {
	// The page has no models then either.
	_, files := newCursorChoice(t, nil)
	c := cursorChoice(nil, "/w", files.kept, files.options, files.list, files.plan)
	files.plan.learnedOnlyAuto()
	if limits := c.Limits(); limits != nil {
		t.Errorf("limits = %+v", limits)
	}
}

const reworkedRefusal = "exit status 1: ActionRequiredError: Model unavailable on Free Free includes Grok 4.7, Grok 4.6, and Auto with fixed settings. Switch to an included model to continue."

func TestTheModelsARefusalSaysThePlanIncludesAreNotOutOfReach(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	files.plan.learned(reworkedRefusal, "gpt-5.5-high")
	limits := c.Limits()
	if limits == nil {
		t.Fatal("no limits")
	}
	for _, included := range []string{"grok-4.7", "cursor-grok-4.6", "auto"} {
		if slices.Contains(limits.Unavailable, included) {
			t.Errorf("%s, which the plan includes, is out of reach", included)
		}
	}
	for _, refused := range []string{"gpt-5.5", "gpt-5.3-codex"} {
		if !slices.Contains(limits.Unavailable, refused) {
			t.Errorf("%s is not out of reach", refused)
		}
	}
}

func TestAModelTheRefusalNamesIsStillOutOfReachWhenItWasTheOneRefused(t *testing.T) {
	// However Cursor words it ("you can't use X on Free. Free includes A, B and Auto"), the model it refused is not included.
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	files.plan.learned("ActionRequiredError: you can't use Grok 4.7 on Free. Free includes Grok 4.6 and Auto. Unavailable.", "grok-4.7-high")
	limits := c.Limits()
	if limits == nil || !slices.Contains(limits.Unavailable, "grok-4.7") || slices.Contains(limits.Unavailable, "cursor-grok-4.6") {
		t.Errorf("limits = %+v", limits)
	}
}

func TestARefusalNamingNoModelLeavesOnlyAuto(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	opts := askedFor(t, c)
	files.plan.learned(namedModelFailure, "gpt-5.5-high")
	if limits := c.Limits(); limits == nil || len(limits.Unavailable) != len(opts.Models)-1 {
		t.Errorf("limits = %+v", limits)
	}
}

func TestEveryModelRefusedIsOutOfReachNotOnlyTheLastWhateverTheRefusalNames(t *testing.T) {
	// Cursor says the plan includes Grok, and then refuses it (seen 2026-10-28), so what it names is only a hope.
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	files.plan.learned(reworkedRefusal, "gpt-5.5-high")
	files.plan.learned(reworkedRefusal, "cursor-grok-4.6-high")
	files.plan.learned(reworkedRefusal, "grok-4.7-medium-fast")
	limits := c.Limits()
	if limits == nil {
		t.Fatal("no limits")
	}
	for _, refused := range []string{"gpt-5.5", "cursor-grok-4.6", "grok-4.7"} {
		if !slices.Contains(limits.Unavailable, refused) {
			t.Errorf("%s, which was refused, is not out of reach", refused)
		}
	}
	if slices.Contains(limits.Unavailable, "auto") {
		t.Error("Auto is out of reach")
	}
}

func TestWhatWasRefusedBeforeTheMemoryExpiredIsForgottenWithIt(t *testing.T) {
	ask, _ := cursorSays(t, "")
	c, files := newCursorChoice(t, ask)
	askedFor(t, c)
	files.plan.learned(reworkedRefusal, "cursor-grok-4.6-high")
	*files.now = files.now.Add(cursorPlanFreshFor + time.Minute)
	files.plan.learned(reworkedRefusal, "grok-4.7-high")
	limits := c.Limits()
	if limits == nil || slices.Contains(limits.Unavailable, "cursor-grok-4.6") || !slices.Contains(limits.Unavailable, "grok-4.7") {
		t.Errorf("limits = %+v", limits)
	}
}
