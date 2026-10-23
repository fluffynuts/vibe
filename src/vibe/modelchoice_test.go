package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/userprefs"
)

// choiceHome is a ~/.vibe with a profile "p" in it, and a bundle with none.
func choiceHome(t *testing.T, profileSettings string) (profileChoice, string) {
	t.Helper()
	home, bundle := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, "profiles", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("name: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if profileSettings != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(profileSettings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return profileChoice{lay: layout.New(home, bundle), profile: "p"}, filepath.Join(dir, "settings.yaml")
}

func TestNothingChosenIsTheAgentsOwnDefault(t *testing.T) {
	c, _ := choiceHome(t, "")
	got, err := c.get()
	if err != nil || got != (turnChoice{}) {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestAChoiceIsKeptInTheProfilesPreferencesAndItsSettingsAreLeftAlone(t *testing.T) {
	c, path := choiceHome(t, "# mine\nmemory: 8g\n")
	if err := c.set("opus", "high"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "opus", Effort: "high"}) {
		t.Errorf("got %+v", got)
	}
	if data, _ := os.ReadFile(path); string(data) != "# mine\nmemory: 8g\n" {
		t.Errorf("settings.yaml was touched:\n%s", data)
	}
	prefs, _ := os.ReadFile(userprefs.Path(filepath.Dir(path)))
	if !strings.Contains(string(prefs), "model: opus") || !strings.Contains(string(prefs), "effort: high") {
		t.Errorf("the preferences hold:\n%s", prefs)
	}
}

func TestAChoiceEarlierVibesKeptInSettingsIsMovedWhenOneIsMadeAndChoosingDefaultTakesItAway(t *testing.T) {
	c, path := choiceHome(t, "memory: 8g\nmodel: haiku\n")
	if err := c.set("opus", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got.Model != "opus" {
		t.Errorf("after choosing: %+v", got)
	}
	if err := c.set("", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{}) {
		t.Errorf("after choosing Default the old settings.yaml key came back: %+v", got)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "memory: 8g") || strings.Contains(string(data), "model") {
		t.Errorf("settings.yaml:\n%s", data)
	}
}

func TestChoosingDefaultTakesAKeyThatWasLeftInSettingsAwayToo(t *testing.T) {
	c, path := choiceHome(t, "memory: 8g\n")
	writeFile(t, filepath.Join(filepath.Dir(path), "composed-hash"), "x\n")
	writeFile(t, path, "memory: 24g\nmodel: haiku\n")
	if err := c.set("", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{}) {
		t.Errorf("a key that migrating had to leave kept the choice alive: %+v", got)
	}
	if data, _ := os.ReadFile(path); string(data) != "memory: 24g\n" {
		t.Errorf("settings.yaml:\n%s", data)
	}
}

func TestAPageChoiceDoesNotMakeAComposedProfileLookEditedByHand(t *testing.T) {
	lay := updateLayout(t)
	if err := (profileChoice{lay: lay, profile: "p"}).set("opus", "high"); err != nil {
		t.Fatal(err)
	}
	changeLibrary(t, lay)
	if got := updated(t, lay); got.state != profileUpdated {
		t.Errorf("outcome = %+v", got)
	}
}

func TestChoosingTheDefaultTakesTheKeysOutRatherThanLeavingEmptyOnes(t *testing.T) {
	c, path := choiceHome(t, "memory: 8g\n")
	if err := c.set("opus", "high"); err != nil {
		t.Fatal(err)
	}
	if err := c.set("", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userprefs.Path(filepath.Dir(path))); err == nil {
		t.Error("a preferences file with nothing in it was left")
	}
	if got, _ := c.get(); got != (turnChoice{}) {
		t.Errorf("got %+v", got)
	}
}

func TestAProfileOverridesWhatTheGlobalSettingsChose(t *testing.T) {
	c, _ := choiceHome(t, "model: haiku\n")
	if err := os.WriteFile(filepath.Join(c.lay.Home, "settings.yaml"), []byte("model: opus\neffort: low\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "haiku", Effort: "low"}) {
		t.Errorf("got %+v", got)
	}
}

func TestAChoiceIsNotWrittenForAProfileThatIsNotThere(t *testing.T) {
	c, _ := choiceHome(t, "")
	c.profile = "nobody"
	err := c.set("opus", "")
	if err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Errorf("err = %v, want it to name the profile", err)
	}
	if _, statErr := os.Stat(filepath.Join(c.lay.Home, "profiles", "nobody")); statErr == nil {
		t.Error("a profile directory was made, which would hide the real one")
	}
}

func TestATurnRefusesAChoiceThatWasWrittenByHandAndIsntSafe(t *testing.T) {
	for _, settings := range []string{"model: \"x; rm -rf /\"\n", "effort: \"$(x)\"\n"} {
		c, _ := choiceHome(t, settings)
		if _, err := c.forTurn(); err == nil {
			t.Errorf("%q: no error", settings)
		} else if !strings.Contains(err.Error(), "settings.yaml") {
			t.Errorf("%q: %v doesn't say where to fix it", settings, err)
		}
	}
}

func realOutput(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "modelchoice", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// askingAgent stands in for running a script in the sandbox: it answers each
// of the two the page's lists are made from with what the real claude said.
func askingAgent(help, model string, err error) func(ctx context.Context, script string) (string, error) {
	return func(ctx context.Context, script string) (string, error) {
		if err != nil {
			return "", err
		}
		if strings.Contains(script, "--help") {
			return help, nil
		}
		return model, nil
	}
}

func TestTheAgentsListsAreMadeFromWhatTheRealClaudeSaid(t *testing.T) {
	ask := askingAgent(realOutput(t, "claude-help-effort.txt"), realOutput(t, "model-command.json"), nil)
	got, err := agentOptions(context.Background(), ask, "/work")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"fable", "opus", "sonnet", "haiku"}; !reflect.DeepEqual(got.Models, want) {
		t.Errorf("models = %v", got.Models)
	}
	if len(got.OtherModels) != 5 || got.OtherModels[0] != "fable[1m]" {
		t.Errorf("other models = %v", got.OtherModels)
	}
	if want := []string{"low", "medium", "high", "xhigh", "max"}; !reflect.DeepEqual(got.Efforts, want) {
		t.Errorf("efforts = %v", got.Efforts)
	}
	if got.Current != "Opus 5.5 (default)" {
		t.Errorf("current = %q", got.Current)
	}
}

func TestWhenTheAgentCantBeAskedTheErrorSaysSo(t *testing.T) {
	_, err := agentOptions(context.Background(), askingAgent("", "", errors.New("exec: sandbox is stopped")), "/work")
	if err == nil || !strings.Contains(err.Error(), "sandbox is stopped") {
		t.Errorf("err = %v", err)
	}
}

func TestAnAnswerThatIsntWhatWasExpectedIsAnErrorNotAnEmptyList(t *testing.T) {
	good := realOutput(t, "claude-help-effort.txt")
	for name, c := range map[string]struct{ help, model string }{
		"model answer not json":      {good, "Welcome to claude"},
		"model answer says an error": {good, `{"is_error":true,"result":"not logged in"}`},
		"model list missing":         {good, `{"is_error":false,"result":"Current model: ` + "`x`" + `"}`},
		"help has no effort flag":    {"  --model <model>   the model\n", realOutput(t, "model-command.json")},
	} {
		if _, err := agentOptions(context.Background(), askingAgent(c.help, c.model, nil), "/work"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestTheAgentIsAskedInTheFolderTheSandboxWorksIn(t *testing.T) {
	var scripts []string
	ask := func(ctx context.Context, script string) (string, error) {
		scripts = append(scripts, script)
		return "", errors.New("stop here")
	}
	agentOptions(context.Background(), ask, "/home/me/my project")
	if len(scripts) == 0 || !strings.HasPrefix(scripts[0], "cd '/home/me/my project' && ") {
		t.Errorf("scripts = %q", scripts)
	}
}

func TestTheListsTheAgentSaidAreKeptForNextTimeWithWhenTheyWereAsked(t *testing.T) {
	file := filepath.Join(t.TempDir(), "companion", "box.model-options.json")
	cached, remember := keptOptions(file, func() time.Time { return time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC) })
	if _, _, ok := cached(); ok {
		t.Fatal("something was kept before anything was remembered")
	}
	want := companion.Options{Models: []string{"opus", "sonnet"}, OtherModels: []string{"best"}, Rules: []string{"best"}, Efforts: []string{"low", "high"}, Current: "Opus 5.5 (default)"}
	if err := remember(want); err != nil {
		t.Fatal(err)
	}
	got, at, ok := cached()
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v; want %+v", got, ok, want)
	}
	if !at.Equal(time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("asked at %v", at)
	}
}

func TestKeptListsThatCantBeReadAreSaidAndAskedForAgainNotTrusted(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":        "{oops",
		"another version": `{"version": 99, "at": "2026-10-21T09:30:00Z", "options": {"models": ["x"]}}`,
		"no models":       `{"version": 1, "at": "2026-10-21T09:30:00Z", "options": {"models": []}}`,
	} {
		file := filepath.Join(t.TempDir(), "box.model-options.json")
		os.WriteFile(file, []byte(content), 0o600)
		cached, _ := keptOptions(file, time.Now)
		var ok bool
		said := notesOf(func() { _, _, ok = cached() })
		if ok {
			t.Errorf("%s: used", name)
		}
		if len(said) != 1 || !strings.Contains(said[0], file) {
			t.Errorf("%s: said %q, want one note naming the file", name, said)
		}
	}
}

func TestListsKeptInAnotherOrderComeBackBiggestFirst(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.model-options.json")
	os.WriteFile(file, []byte(`{"version": 1, "at": "2026-10-21T09:30:00Z", "options": {"models": ["sonnet", "opus", "haiku", "fable"], "otherModels": ["best", "sonnet[1m]", "opus[1m]"], "efforts": ["low"]}}`), 0o600)
	cached, _ := keptOptions(file, time.Now)
	got, _, ok := cached()
	if !ok || !reflect.DeepEqual(got.Models, []string{"fable", "opus", "sonnet", "haiku"}) || !reflect.DeepEqual(got.OtherModels, []string{"opus[1m]", "sonnet[1m]", "best"}) {
		t.Errorf("got %+v, %v", got, ok)
	}
}

// The real answers to /model <alias>, from claude 2.1.306.
var realNames = map[string]string{
	"fable": "Fable 5.1", "opus": "Opus 5.5", "sonnet": "Sonnet 5.5", "haiku": "Haiku 5.5",
	"fable[1m]": "Fable 5.1", "opus[1m]": "Opus 5.5 (1M context)", "sonnet[1m]": "Sonnet 5.5",
	"best": "Fable 5.1", "opusplan": "Opus in plan mode, else Sonnet",
}

// namesAnswer is what the one command that asks for each alias's name writes:
// a line naming the alias, then claude's answer to it.
func namesAnswer(aliases ...string) string {
	var out strings.Builder
	for _, a := range aliases {
		out.WriteString("=== " + a + "\n")
		out.WriteString(`{"is_error":false,"result":"Set model to ` + "`" + realNames[a] + "`" + ` for this session only"}` + "\n\n")
	}
	return out.String()
}

func askingWithNames(names string, namesErr error) askAgent {
	return func(ctx context.Context, script string) (string, error) {
		switch {
		case strings.Contains(script, "--help"):
			return realOutputNoT("claude-help-effort.txt"), nil
		case strings.Contains(script, "==="):
			return names, namesErr
		}
		return realOutputNoT("model-command.json"), nil
	}
}

func realOutputNoT(name string) string {
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "modelchoice", "testdata", name))
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestEachListedModelIsAskedWhatItIsCalledInOneCommand(t *testing.T) {
	var scripts []string
	ask := func(ctx context.Context, script string) (string, error) {
		scripts = append(scripts, script)
		return askingWithNames(namesAnswer("fable", "opus", "sonnet", "haiku", "fable[1m]", "opus[1m]", "sonnet[1m]", "best", "opusplan"), nil)(ctx, script)
	}
	got, err := agentOptions(context.Background(), ask, "/work")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Names, realNames) {
		t.Errorf("names = %v, want %v", got.Names, realNames)
	}
	if len(scripts) != 3 {
		t.Errorf("%d commands run in the sandbox, want 3 (the help, the list, the names): %q", len(scripts), scripts)
	}
	names := scripts[len(scripts)-1]
	if !strings.HasPrefix(names, "cd '/work' && ") || strings.Contains(names, "'default'") {
		t.Errorf("names command = %s", names)
	}
	for _, alias := range []string{"'fable'", "'opus[1m]'", "'opusplan'"} {
		if !strings.Contains(names, alias) {
			t.Errorf("%s isn't asked for: %s", alias, names)
		}
	}
}

func TestWhenTheNamesCantBeHadTheListsStillComeAndItIsSaid(t *testing.T) {
	var got companion.Options
	var err error
	said := notesOf(func() {
		got, err = agentOptions(context.Background(), askingWithNames("", errors.New("exec: sandbox is stopped")), "/work")
	})
	if err != nil || len(got.Models) != 4 {
		t.Fatalf("%v, %+v", err, got)
	}
	if len(got.Names) != 0 {
		t.Errorf("names = %v", got.Names)
	}
	if len(said) != 1 || !strings.Contains(said[0], "sandbox is stopped") {
		t.Errorf("said %q, want one note with why", said)
	}
}

func TestANameThatCantBeReadLeavesOnlyThatOneOut(t *testing.T) {
	answer := namesAnswer("fable", "opus") + "=== sonnet\nnot json\n\n" + namesAnswer("haiku")
	got, err := agentOptions(context.Background(), askingWithNames(answer, nil), "/work")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"fable": "Fable 5.1", "opus": "Opus 5.5", "haiku": "Haiku 5.5"}; !reflect.DeepEqual(got.Names, want) {
		t.Errorf("names = %v, want %v", got.Names, want)
	}
}

func TestAChoiceIsKeptInAFileWithTheModeItHadAndOneMadeNewIsReadableByOthers(t *testing.T) {
	c, path := choiceHome(t, "memory: 8g\n")
	prefs := userprefs.Path(filepath.Dir(path))
	writeFile(t, prefs, "theme: amber\n")
	if err := os.Chmod(prefs, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.set("opus", ""); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(prefs); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("a file the user made private became %v", info.Mode().Perm())
	}
	fresh, freshPath := choiceHome(t, "")
	if err := fresh.set("opus", ""); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(userprefs.Path(filepath.Dir(freshPath))); runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("new preferences are %v, want 0644", info.Mode().Perm())
	}
}

func TestChoosingLeavesNoTemporaryFileBeside(t *testing.T) {
	c, path := choiceHome(t, "")
	for i := 0; i < 5; i++ {
		if err := c.set("opus", "high"); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"config.yaml", "user-preferences.yaml"}; !reflect.DeepEqual(names, want) {
		t.Errorf("the profile holds %v, want %v", names, want)
	}
}

// A turn reads the choice as the page may be changing it: it gets one choice
// or the other whole, never an emptied file (which would mean the default
// model, quietly) or half of one.
func TestATurnStartingWhileTheChoiceIsBeingChangedGetsOneOfThemWhole(t *testing.T) {
	c, _ := choiceHome(t, "")
	one, other := turnChoice{Model: "opus", Effort: "high"}, turnChoice{Model: "sonnet[1m]", Effort: "low"}
	if err := c.set(one.Model, one.Effort); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			next := other
			if i%2 == 0 {
				next = one
			}
			if err := c.set(next.Model, next.Effort); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		got, err := c.forTurn()
		if err != nil {
			t.Fatalf("a turn couldn't read the choice: %v", err)
		}
		if got != one && got != other {
			t.Fatalf("a turn read %+v, which is neither choice", got)
		}
	}
}

// askWith answers the three commands the lists are made from with what's given.
func askWith(help, model, names string) askAgent {
	return func(ctx context.Context, script string) (string, error) {
		switch {
		case strings.Contains(script, "--help"):
			return help, nil
		case strings.Contains(script, "==="):
			return names, nil
		}
		return model, nil
	}
}

func jsonResult(text string) string {
	data, _ := json.Marshal(map[string]interface{}{"is_error": false, "result": text})
	return string(data)
}

func TestTheNamesThatPickByARuleAreSaidSoToThePage(t *testing.T) {
	got, err := agentOptions(context.Background(), askingWithNames(namesAnswer("fable"), nil), "/work")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"best", "opusplan"}; !reflect.DeepEqual(got.Rules, want) {
		t.Errorf("rules = %v, want %v", got.Rules, want)
	}
}

func TestListsKeptBeforeRulesWereSaidGetThemWhenTheyAreRead(t *testing.T) {
	file := filepath.Join(t.TempDir(), "box.model-options.json")
	os.WriteFile(file, []byte(`{"version": 1, "at": "2026-10-21T09:30:00Z", "options": {"models": ["opus"], "otherModels": ["best", "opus[1m]", "opusplan"], "efforts": ["low"]}}`), 0o600)
	cached, _ := keptOptions(file, time.Now)
	got, _, ok := cached()
	if !ok || !reflect.DeepEqual(got.Rules, []string{"best", "opusplan"}) {
		t.Errorf("got %+v, %v", got, ok)
	}
}

func TestAnAnswerFarLargerThanAListShouldBeIsRefusedNotKept(t *testing.T) {
	realHelp, realModel := realOutputNoT("claude-help-effort.txt"), realOutputNoT("model-command.json")
	huge := strings.Repeat("x", maxAnswer+1)
	for name, ask := range map[string]askAgent{
		"the help":         askWith(strings.Repeat("x", maxHelp+1), realModel, ""),
		"the models":       askWith(realHelp, jsonResult("Available: opus, "+huge), ""),
		"the models' json": askWith(realHelp, huge, ""),
	} {
		if _, err := agentOptions(context.Background(), ask, "/work"); err == nil || !strings.Contains(err.Error(), "larger") {
			t.Errorf("%s: err = %v, want a refusal saying it is too large", name, err)
		}
	}
}

func TestTooManyModelsAreCutAndSaid(t *testing.T) {
	var many []string
	for i := 0; i < 120; i++ {
		many = append(many, strings.Repeat(string(rune('a'+i%26)), 1+i/26))
	}
	ask := askWith(realOutputNoT("claude-help-effort.txt"), jsonResult("Current model: `X`\nAvailable: "+strings.Join(many, ", ")+", or a full model ID."), "")
	var got companion.Options
	said := notesOf(func() { got, _ = agentOptions(context.Background(), ask, "/work") })
	if n := len(got.Models) + len(got.OtherModels); n != 50 {
		t.Errorf("%d models taken, want 50", n)
	}
	var mentioned bool
	for _, line := range said {
		if strings.Contains(line, "70") && strings.Contains(line, "left out") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("the 70 left out aren't said: %q", said)
	}
}

func TestOnlyTheNamesAskedForAreTakenFromWhatTheAgentSaysTheyAre(t *testing.T) {
	hostile := namesAnswer("fable", "opus") +
		"=== nobody-asked\n" + jsonResult("Set model to `Smuggled` for this session only") + "\n\n" +
		"=== sonnet\n" + jsonResult("Set model to `"+strings.Repeat("N", 101)+"` for this session only") + "\n\n"
	got, err := agentOptions(context.Background(), askingWithNames(hostile, nil), "/work")
	if err != nil {
		t.Fatal(err)
	}
	if _, there := got.Names["nobody-asked"]; there {
		t.Error("a name for an alias nobody asked about was taken")
	}
	if _, there := got.Names["sonnet"]; there {
		t.Errorf("a name of %d characters was taken", len(got.Names["sonnet"]))
	}
	if got.Names["fable"] != "Fable 5.1" || got.Names["opus"] != "Opus 5.5" {
		t.Errorf("the real ones were lost: %v", got.Names)
	}
}

func TestANamesAnswerFarLargerThanItShouldBeLeavesTheListsAloneAndIsSaid(t *testing.T) {
	realHelp, realModel := realOutputNoT("claude-help-effort.txt"), realOutputNoT("model-command.json")
	var got companion.Options
	var err error
	said := notesOf(func() {
		got, err = agentOptions(context.Background(), askWith(realHelp, realModel, "=== fable\n"+strings.Repeat("x", maxNamesAnswer+1)), "/work")
	})
	if err != nil || len(got.Models) != 4 || len(got.Names) != 0 {
		t.Fatalf("%v, models %v, names %v", err, got.Models, got.Names)
	}
	if len(said) != 1 || !strings.Contains(said[0], "larger") {
		t.Errorf("said %q", said)
	}
}

// Pinned: a profile's Default only removes the profile's own choice, so what
// the global settings.yaml says stays in force. The page says so (see
// companion's inherited), so this is a decision and not an accident.
func TestChoosingDefaultInAProfileLeavesTheGlobalSettingsChoiceInForce(t *testing.T) {
	c, _ := choiceHome(t, "")
	if err := os.WriteFile(filepath.Join(c.lay.Home, "settings.yaml"), []byte("model: opus\neffort: low\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.set("sonnet", "high"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "sonnet", Effort: "high"}) {
		t.Fatalf("the profile's own choice should win: %+v", got)
	}
	if err := c.set("", ""); err != nil {
		t.Fatal(err)
	}
	own, _ := c.own()
	if own != (turnChoice{}) {
		t.Errorf("the profile still holds %+v", own)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "opus", Effort: "low"}) {
		t.Errorf("choosing Default gave %+v, want the global settings' opus and low", got)
	}
}

func TestOwnIsWhatTheProfileHoldsAndNotWhatItInherits(t *testing.T) {
	c, _ := choiceHome(t, "effort: high\n")
	os.WriteFile(filepath.Join(c.lay.Home, "settings.yaml"), []byte("model: opus\n"), 0o644)
	own, err := c.own()
	if err != nil || own != (turnChoice{Effort: "high"}) {
		t.Errorf("own = %+v, %v; want only the effort", own, err)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "opus", Effort: "high"}) {
		t.Errorf("get = %+v", got)
	}
}

func TestAChoiceIsNotHalfMadeWhenSettingsYamlCantBeChanged(t *testing.T) {
	c, path := choiceHome(t, "")
	if err := userprefs.Update(c.dir(), func(u *userprefs.Prefs) { u.Model = "opus" }); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "memory: [unclosed\n")
	if err := c.set("", ""); err == nil {
		t.Fatal("choosing Default said it was kept, though settings.yaml couldn't be read to take the old key out")
	}
	if got, _ := userprefs.Load(c.dir()); got.Model != "opus" {
		t.Errorf("the choice was changed (%+v) though it was said not to be kept", got)
	}
}
