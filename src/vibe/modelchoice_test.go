package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vibe/internal/companion"
	"vibe/internal/layout"
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

func TestAChoiceIsKeptInTheProfilesSettingsWithTheRestOfThemLeftAlone(t *testing.T) {
	c, path := choiceHome(t, "# mine\nmemory: 8g\n")
	if err := c.set("opus", "high"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.get(); got != (turnChoice{Model: "opus", Effort: "high"}) {
		t.Errorf("got %+v", got)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# mine") || !strings.Contains(string(data), "memory: 8g") {
		t.Errorf("the rest of the file was lost:\n%s", data)
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
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "model") || strings.Contains(string(data), "effort") {
		t.Errorf("keys left behind:\n%s", data)
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
	if want := []string{"sonnet", "opus", "haiku", "fable"}; !reflect.DeepEqual(got.Models, want) {
		t.Errorf("models = %v", got.Models)
	}
	if len(got.OtherModels) != 5 || got.OtherModels[0] != "best" {
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
	want := companion.Options{Models: []string{"sonnet", "opus"}, OtherModels: []string{"best"}, Efforts: []string{"low", "high"}, Current: "Opus 5.5 (default)"}
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
