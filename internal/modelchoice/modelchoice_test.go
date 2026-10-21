package modelchoice

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func modelCommandResult(t *testing.T) string {
	t.Helper()
	var out struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(fixture(t, "model-command.json")), &out); err != nil {
		t.Fatal(err)
	}
	return out.Result
}

func TestTheModelListingOfTheRealClaudeIsRead(t *testing.T) {
	got, err := ParseModels(modelCommandResult(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"fable", "opus", "sonnet", "haiku"}; !reflect.DeepEqual(got.Plain, want) {
		t.Errorf("plain = %v, want %v", got.Plain, want)
	}
	if want := []string{"fable[1m]", "opus[1m]", "sonnet[1m]", "best", "opusplan"}; !reflect.DeepEqual(got.Other, want) {
		t.Errorf("other = %v, want %v", got.Other, want)
	}
	if got.Current != "Opus 5.5 (default)" {
		t.Errorf("current = %q", got.Current)
	}
}

func TestAListingWithoutModelsIsAnErrorNotAnEmptyList(t *testing.T) {
	for _, text := range []string{"", "Current model: `Opus`", "something else entirely", "Available: , or a full model ID."} {
		if _, err := ParseModels(text); err == nil {
			t.Errorf("%q: no error", text)
		}
	}
}

func TestTheEffortsTheFlagTakesAreReadFromItsHelp(t *testing.T) {
	got, err := ParseEfforts(fixture(t, "claude-help-effort.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"low", "medium", "high", "xhigh", "max"}; !reflect.DeepEqual(got, want) {
		t.Errorf("efforts = %v, want %v", got, want)
	}
}

func TestHelpWithoutAnEffortFlagIsAnError(t *testing.T) {
	if _, err := ParseEfforts("  --model <model>   the model\n"); err == nil {
		t.Error("no error")
	}
}

func TestAModelNameIsOnlyAcceptedWhenSafeToPutInACommand(t *testing.T) {
	for _, ok := range []string{"", "opus", "sonnet[1m]", "claude-opus-5-5", "us.anthropic.claude-sonnet-4:0", "a"} {
		if err := CheckModel(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"-rm", "a b", "a;b", "$(x)", "a'b", "a\"b", "a\nb", "`x`", "a|b", "a&b", "é", strings.Repeat("a", 101)} {
		if err := CheckModel(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAnEffortMustBeOneTheAgentTakes(t *testing.T) {
	allowed := []string{"low", "high"}
	if err := CheckEffort("", allowed); err != nil {
		t.Errorf("default refused: %v", err)
	}
	if err := CheckEffort("high", allowed); err != nil {
		t.Errorf("high refused: %v", err)
	}
	err := CheckEffort("max", allowed)
	if err == nil || !strings.Contains(err.Error(), "low, high") {
		t.Errorf("max: %v, want a refusal saying what is allowed", err)
	}
}

func TestAStoredEffortMustBeAPlainName(t *testing.T) {
	for _, ok := range []string{"", "high", "xhigh"} {
		if err := CheckEffortName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"High", "a b", "-x", "a;b", "$(x)", "x1"} {
		if err := CheckEffortName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestModelsAreOrderedBiggestFirstWhereTheSizeIsKnownAndAlphabeticallyWhereItIsNot(t *testing.T) {
	for _, tc := range []struct{ in, want []string }{
		{[]string{"haiku", "sonnet", "opus", "fable"}, []string{"fable", "opus", "sonnet", "haiku"}},
		{[]string{"sonnet[1m]", "opus[1m]"}, []string{"opus[1m]", "sonnet[1m]"}},
		{[]string{"zeta", "alpha", "haiku", "opus"}, []string{"opus", "haiku", "alpha", "zeta"}},
		{[]string{"claude-fable-5-1", "claude-haiku-5-5"}, []string{"claude-fable-5-1", "claude-haiku-5-5"}},
		{[]string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5"}, []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}},
		{[]string{"b", "a"}, []string{"a", "b"}},
		{nil, nil},
	} {
		if got := BySize(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("BySize(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestOrderingDoesntChangeWhatItIsGiven(t *testing.T) {
	in := []string{"haiku", "opus"}
	BySize(in)
	if in[0] != "haiku" {
		t.Errorf("the list given was reordered in place: %v", in)
	}
}
