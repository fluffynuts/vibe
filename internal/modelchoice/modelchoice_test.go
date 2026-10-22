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

func TestTheNameAnAliasStandsForIsReadFromWhatTheAgentSaid(t *testing.T) {
	for in, want := range map[string]string{
		"Set model to `Fable 5.1` for this session only":                      "Fable 5.1",
		"Set model to `Opus 5.5 (1M context)` for this session only":          "Opus 5.5 (1M context)",
		"Set model to `Opus in plan mode, else Sonnet` for this session only": "Opus in plan mode, else Sonnet",
		"Set model to `Opus 5.5 (default)` for this session only":             "Opus 5.5 (default)",
	} {
		got, err := ParseModelName(in)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestAnAnswerThatNamesNoModelIsAnError(t *testing.T) {
	for _, in := range []string{"", "Unknown model: nope", "Set model to `` for this session only", "Set model to Opus"} {
		if _, err := ParseModelName(in); err == nil {
			t.Errorf("%q: no error", in)
		}
	}
}

// help shapes a --help with the flags given, one per line as claude prints them.
func help(flags ...string) string { return "Options:\n" + strings.Join(flags, "\n") + "\n" }

func TestTheEffortListIsOnlyReadFromTheEffortFlagsOwnDescription(t *testing.T) {
	for name, tc := range map[string]struct {
		help string
		want []string
	}{
		"the list in parentheses, wrapped onto the next line": {
			help("  --effort <level>     Effort level for the current session", "                       (low, high)", "  --other <x>          Another flag (a, b)"),
			[]string{"low", "high"}},
		"it is the last flag": {
			help("  --model <m>          the model", "  --effort <level>     Effort (low, medium, high)"),
			[]string{"low", "medium", "high"}},
		"another note in parentheses beside the list": {
			help("  --effort <level>     Effort (experimental) (low, high)", "  --x <y>              x"),
			[]string{"low", "high"}},
		"a later flag's list isn't taken for the effort's": {
			help("  --effort <level>     Effort: low, medium or high", "  --pick <p>           which (one, two, three)"),
			nil},
		"a later flag's single word in parentheses isn't taken either": {
			help("  --effort <level>     how hard to think", "  --environment <id>   where it runs (default)"),
			nil},
		"a lone word in parentheses isn't a list of efforts": {
			help("  --effort <level>     how hard to think (max)"),
			nil},
		"two lists are ambiguous, not guessed at": {
			help("  --effort <level>     Effort (low, high) or (fast, slow)"),
			nil},
		"upper case is not what the flag takes": {
			help("  --effort <level>     Effort (Low, High)"),
			nil},
		"nothing in parentheses": {
			help("  --effort <level>     how hard to think"),
			nil},
	} {
		got, err := ParseEfforts(tc.help)
		if tc.want == nil {
			if err == nil {
				t.Errorf("%s: no error, got %v", name, got)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, %v; want %v", name, got, err, tc.want)
		}
	}
}

func TestAnAbsurdlyLongListOfEffortsIsRefused(t *testing.T) {
	var many []string
	for i := 0; i < 40; i++ {
		many = append(many, strings.Repeat("a", 1+i%5)+strings.Repeat("b", i/5))
	}
	if got, err := ParseEfforts(help("  --effort <level>     Effort (" + strings.Join(many, ", ") + ")")); err == nil {
		t.Errorf("accepted %d efforts", len(got))
	}
}

// names makes n distinct lower-case names (letters only, as plain ones are).
func names(n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, strings.Repeat(string(rune('a'+i%26)), 1+i/26))
	}
	return out
}

func TestOnlyAReasonableNumberOfModelsIsTakenFromAList(t *testing.T) {
	got, err := ParseModels("Current model: `X`\nUsage: /model <name>. Available: " + strings.Join(names(200), ", ") + ", or a full model ID.")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.Plain) + len(got.Other); n != maxModels {
		t.Errorf("%d models taken, want %d", n, maxModels)
	}
	if got.Dropped != 200-maxModels {
		t.Errorf("dropped = %d, want %d", got.Dropped, 200-maxModels)
	}
}

func TestAListWithinTheLimitDropsNothing(t *testing.T) {
	got, err := ParseModels(modelCommandResult(t))
	if err != nil || got.Dropped != 0 {
		t.Errorf("dropped %d, %v", got.Dropped, err)
	}
}

func TestAModelNameFarLongerThanAnyRealOneIsLeftOut(t *testing.T) {
	long := strings.Repeat("a", maxModel+1)
	got, err := ParseModels("Available: opus, " + long + ", sonnet[" + strings.Repeat("1", maxModel) + "], haiku, or a full model ID.")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"opus", "haiku"}; !reflect.DeepEqual(got.Plain, want) {
		t.Errorf("plain = %v, want %v", got.Plain, want)
	}
	if len(got.Other) != 0 || got.Dropped != 2 {
		t.Errorf("other = %v, dropped %d; want none, and 2", got.Other, got.Dropped)
	}
}

func TestTheNameOfTheDefaultModelIsKeptToALengthThatCanBeShown(t *testing.T) {
	got, err := ParseModels("Current model: `" + strings.Repeat("x", 500) + "`\nAvailable: opus, or a full model ID.")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got.Current)) > maxCurrent {
		t.Errorf("current is %d characters", len([]rune(got.Current)))
	}
}

func TestWhichListedNamesPickByARuleAndAreNotOneModel(t *testing.T) {
	got, err := ParseModels(modelCommandResult(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"best", "opusplan"}; !reflect.DeepEqual(got.Rules, want) {
		t.Errorf("rules = %v, want %v", got.Rules, want)
	}
	for name, want := range map[string]bool{"best": true, "opusplan": true, "opus": false, "opus[1m]": false, "fable[1m]": false, "default": false} {
		if IsRule(name) != want {
			t.Errorf("IsRule(%q) = %v, want %v", name, !want, want)
		}
	}
	if got := RulesAmong([]string{"opus[1m]", "best", "x", "opusplan"}); !reflect.DeepEqual(got, []string{"best", "opusplan"}) {
		t.Errorf("RulesAmong = %v", got)
	}
	if got := RulesAmong(nil); len(got) != 0 {
		t.Errorf("RulesAmong(nil) = %v", got)
	}
}
