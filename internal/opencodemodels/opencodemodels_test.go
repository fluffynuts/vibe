package opencodemodels

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func sample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "samples", "opencode", "models-verbose.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// paid is a model of a provider that charges, in the shape OpenCode prints (cost per token above zero).
const paid = `anthropic/claude-haiku-4-5
{
  "id": "claude-haiku-4-5",
  "providerID": "anthropic",
  "name": "Claude Haiku 4.5",
  "status": "active",
  "cost": {"input": 1, "output": 5, "cache": {"read": 0.1, "write": 1.25}},
  "capabilities": {"toolcall": true, "input": {"text": true, "image": true}},
  "variants": {"high": {}, "max": {}}
}
`

const noTools = `local/tiny
{
  "id": "tiny",
  "providerID": "local",
  "name": "Tiny",
  "status": "active",
  "cost": {"input": 0, "output": 0},
  "capabilities": {"toolcall": false}
}
`

func TestTheRealListIsReadModelByModel(t *testing.T) {
	entries, err := Parse(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("%d entries", len(entries))
	}
	byKey := map[string]Entry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	beam := byKey["opencode/beam-free"]
	if beam.Name != "Beam Free" || beam.Provider != "opencode" || !beam.Free || !beam.Tools || !slices.Equal(beam.Variants, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("beam-free: %+v", beam)
	}
	if got := byKey["opencode/big-pickle"]; len(got.Variants) != 0 || !got.Free {
		t.Errorf("big-pickle: %+v", got)
	}
}

func TestAPaidModelIsNotFree(t *testing.T) {
	entries, err := Parse(paid)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v, %v", entries, err)
	}
	if entries[0].Free || entries[0].Provider != "anthropic" || entries[0].Key != "anthropic/claude-haiku-4-5" {
		t.Errorf("entry: %+v", entries[0])
	}
}

func TestVariantsKeepTheOrderOpenCodeGaveThem(t *testing.T) {
	entries, err := Parse(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Key == "opencode/nemotron-3.5-super-vl-free" && !slices.Equal(e.Variants, []string{"none", "minimal", "low", "medium", "high", "xhigh"}) {
			t.Errorf("variants: %v", e.Variants)
		}
	}
}

func TestAModelThatCantUseToolsIsLeftOutOfWhatIsOffered(t *testing.T) {
	entries, err := Parse(paid + noTools)
	if err != nil {
		t.Fatal(err)
	}
	cat := Build(entries)
	if cat.Has("local/tiny") || !cat.Has("anthropic/claude-haiku-4-5") {
		t.Errorf("models: %v", cat.Keys())
	}
}

func TestACatalogueSaysWhatTheEffortsAreByModelAndWhichModelsAreFree(t *testing.T) {
	entries, err := Parse(sample(t) + paid)
	if err != nil {
		t.Fatal(err)
	}
	cat := Build(entries)
	if got := cat.Efforts("opencode/big-pickle"); got == nil || len(got) != 0 {
		t.Errorf("a model with no efforts has %v (want an empty list, not none)", got)
	}
	if got := cat.Efforts("anthropic/claude-haiku-4-5"); !slices.Equal(got, []string{"high", "max"}) {
		t.Errorf("haiku efforts: %v", got)
	}
	all := cat.AllEfforts()
	for _, want := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if !slices.Contains(all, want) {
			t.Errorf("all efforts lack %s: %v", want, all)
		}
	}
	if !cat.Free("opencode/big-pickle") || cat.Free("anthropic/claude-haiku-4-5") {
		t.Error("free is wrong")
	}
	if cat.Name("anthropic/claude-haiku-4-5") != "Claude Haiku 4.5" {
		t.Errorf("name: %q", cat.Name("anthropic/claude-haiku-4-5"))
	}
}

func TestAnythingThatIsntAListOfModelsIsAnError(t *testing.T) {
	for _, text := range []string{"", "   \n", "Error: not signed in\n", "opencode/x\nnot json\n"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%q was taken as a list", text)
		}
	}
}

func TestTheEffortsHaveNamesForThePage(t *testing.T) {
	for effort, want := range map[string]string{"low": "Low", "xhigh": "Extra high", "max": "Max", "none": "None", "minimal": "Minimal", "brand-new": "Brand-new"} {
		if got := EffortLabel(effort); got != want {
			t.Errorf("%s: %q, want %q", effort, got, want)
		}
	}
}

func TestAProvidersModelsAreFoundByTheStartOfTheKey(t *testing.T) {
	entries, err := Parse(sample(t) + paid)
	if err != nil {
		t.Fatal(err)
	}
	cat := Build(entries)
	if got := cat.Providers(); !slices.Equal(got, []string{"opencode", "anthropic"}) {
		t.Errorf("providers: %v", got)
	}
}
