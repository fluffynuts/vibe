package cursormodels

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// realList is what cursor-agent --list-models said (docs/samples/cursor).
func realList(t *testing.T) Catalogue {
	t.Helper()
	text, err := os.ReadFile("../../docs/samples/cursor/list-models.txt")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := Parse(string(text))
	if err != nil {
		t.Fatal(err)
	}
	return Build(entries)
}

func family(t *testing.T, c Catalogue, key string) Family {
	t.Helper()
	f, ok := c.family(key)
	if !ok {
		t.Fatalf("no family %q", key)
	}
	return f
}

func TestEveryIdInTheRealListIsInExactlyOneFamilyAndFoundAgain(t *testing.T) {
	text, _ := os.ReadFile("../../docs/samples/cursor/list-models.txt")
	entries, err := Parse(string(text))
	if err != nil {
		t.Fatal(err)
	}
	c := Build(entries)
	for _, e := range entries {
		key, value := Split(e.ID)
		id, ok := c.ID(key, value)
		if !ok || id != e.ID {
			t.Errorf("%s is family %q value %q, which finds %q (%v)", e.ID, key, value, id, ok)
		}
	}
	if len(c.Families) < 40 || len(c.Families) > 70 {
		t.Errorf("%d families from %d ids: the split is not doing what it should", len(c.Families), len(entries))
	}
}

func TestAFamilyOffersItsEffortsInOrderAndTheirFastVariants(t *testing.T) {
	c := realList(t)
	if got := c.Efforts("gpt-5.3-codex"); !slices.Equal(got, []string{"fast", "low", "lowfast", "high", "highfast", "xhigh", "xhighfast"}) {
		t.Errorf("gpt-5.3-codex: %v", got)
	}
	if got := c.Efforts("composer-2.5"); !slices.Equal(got, []string{"fast"}) {
		t.Errorf("composer-2.5: %v", got)
	}
	if got := c.Efforts("auto"); len(got) != 0 {
		t.Errorf("auto has efforts: %v", got)
	}
}

func TestAnEffortBeforeThinkingIsTheSameModelAsOneAfterIt(t *testing.T) {
	c := realList(t)
	if f, v := Split("claude-4.6-opus-high-thinking"); f != "claude-4.6-opus-thinking" || v != "high" {
		t.Errorf("got %q, %q", f, v)
	}
	if f, v := Split("claude-opus-5-thinking-high-fast"); f != "claude-opus-5-thinking" || v != "highfast" {
		t.Errorf("got %q, %q", f, v)
	}
	if id, ok := c.ID("claude-4.6-opus-thinking", "max"); !ok || id != "claude-4.6-opus-max-thinking" {
		t.Errorf("got %q, %v", id, ok)
	}
}

func TestExtraHighIsTheXhighEffortOfItsModelNotAModelOfItsOwn(t *testing.T) {
	c := realList(t)
	if f, v := Split("gpt-5.5-extra-high"); f != "gpt-5.5" || v != "xhigh" {
		t.Errorf("got %q, %q", f, v)
	}
	if _, ok := c.family("gpt-5.5-extra"); ok {
		t.Error("gpt-5.5-extra is a family")
	}
	if id, ok := c.ID("gpt-5.5", "xhigh"); !ok || !strings.HasPrefix(id, "gpt-5.5-") {
		t.Errorf("got %q, %v", id, ok)
	}
}

func TestAFamilyIsNamedWithoutItsEffortOrFast(t *testing.T) {
	c := realList(t)
	for key, want := range map[string]string{
		"gpt-5.3-codex":           "Codex 5.3",
		"claude-fable-5-thinking": "Claude Fable 5 1M Thinking (NO ZDR)",
		"grok-4.7":                "Grok 4.7",
		"auto":                    "Auto",
		"cursor-grok-4.6":         "Grok 4.6",
	} {
		if got := family(t, c, key).Name; got != want {
			t.Errorf("%s is called %q, want %q", key, got, want)
		}
	}
}

func TestADefaultEffortIsThePlainIdOrElseAMiddlingOne(t *testing.T) {
	c := realList(t)
	if id, _ := c.ID("gpt-5.3-codex", ""); id != "gpt-5.3-codex" {
		t.Errorf("has a plain id, but got %q", id)
	}
	if id, _ := c.ID("claude-opus-5-thinking", ""); id != "claude-opus-5-thinking-high" {
		t.Errorf("no plain id: got %q", id)
	}
}

func TestAChoiceCursorHasNoSuchModelForIsNotFound(t *testing.T) {
	c := realList(t)
	for _, tc := range [][2]string{{"no-such", ""}, {"auto", "high"}, {"gpt-5.3-codex", "max"}} {
		if id, ok := c.ID(tc[0], tc[1]); ok {
			t.Errorf("%v found %q", tc, id)
		}
	}
	if c.Has("opus") || !c.Has("auto") {
		t.Error("Has is wrong")
	}
}

func TestTheCurrentModelIsMarkedByNameOnTheFamily(t *testing.T) {
	entries, err := Parse("Available models\n\nauto - Auto\ngpt-5.3-codex-high - Codex 5.3 High (current, default)\n")
	if err != nil {
		t.Fatal(err)
	}
	if c := Build(entries); c.Current != "Codex 5.3" {
		t.Errorf("current = %q", c.Current)
	}
	if entries[1].Name != "Codex 5.3 High" || !entries[1].Current {
		t.Errorf("entry = %+v", entries[1])
	}
}

func TestParseSkipsWhatIsNotAModelAndStripsWhatIsNotText(t *testing.T) {
	entries, err := Parse("\x1b[1mAvailable models\x1b[0m\n\nauto - Auto (default)\nfoo-bar - Foo Bar​​\n\nTip: use /model\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Name != "Foo Bar" || entries[0].Name != "Auto" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestAListWithNoModelIsAnErrorNotAnEmptyList(t *testing.T) {
	for _, said := range []string{"", "Not logged in", "Error: Authentication required"} {
		if _, err := Parse(said); err == nil || !strings.Contains(err.Error(), "no model") {
			t.Errorf("%q: %v", said, err)
		}
	}
}

func TestEffortsAreLabelledForReading(t *testing.T) {
	for value, want := range map[string]string{
		"high": "High effort", "xhigh": "Extra High effort", "none": "No effort", "fast": "Fast",
		"highfast": "High effort · Fast", "xhighfast": "Extra High effort · Fast",
	} {
		if got := EffortLabel(value); got != want {
			t.Errorf("%s is %q, want %q", value, got, want)
		}
	}
}

func TestEffortValuesAreOnlyLowerCaseLettersAsTheStoredChoiceRequires(t *testing.T) {
	for _, value := range realList(t).AllEfforts() {
		for _, r := range value {
			if r < 'a' || r > 'z' {
				t.Errorf("%q is not lower-case letters only", value)
			}
		}
	}
}

func TestTheModelsNamedInATextAreFoundByTheirWholeNames(t *testing.T) {
	c := realList(t)
	for name, tc := range map[string]struct {
		text string
		want []string
	}{
		"a list":              {"Free includes Grok 4.7, Grok 4.6, and Auto with fixed settings.", []string{"grok-4.7", "cursor-grok-4.6", "auto"}},
		"a name at the end":   {"you can use Grok 4.7.", []string{"grok-4.7"}},
		"any case":            {"free includes GROK 4.7 and auto", []string{"grok-4.7", "auto"}},
		"a prefix is no name": {"Free includes Grok 4 and Grok 4.", nil},
		"no model":            {"Free plans can only use Auto. Switch to Auto or upgrade plans to continue.", []string{"auto"}},
	} {
		got := c.NamedIn(tc.text)
		slices.Sort(got)
		slices.Sort(tc.want)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestAModelWhoseNameEndsAnothersIsNotTakenForIt(t *testing.T) {
	c := Build([]Entry{{ID: "gpt-5", Name: "GPT-5"}, {ID: "gpt-5-codex", Name: "GPT-5 Codex"}})
	if got := c.NamedIn("includes GPT-5 Codex only"); !slices.Equal(got, []string{"gpt-5-codex"}) {
		t.Errorf("%v", got)
	}
	if got := c.NamedIn("includes GPT-5 Codex and GPT-5"); len(got) != 2 {
		t.Errorf("%v", got)
	}
}
