package prompt

import (
	"reflect"
	"strings"
	"testing"
)

func typing(m *multiFilterState, s string) {
	for i := 0; i < len(s); i++ {
		m.typeKey(s[i])
	}
}

var sandboxes = []string{"yumbi-api", "yumbi-web", "phoenix", "pheonix-old"}

func TestMultiFilterTypingNarrowsAndBackspaceWidens(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	typing(m, "YUM")
	if !reflect.DeepEqual(m.matches, []int{0, 1}) {
		t.Errorf("matches for YUM = %v, want [0 1]", m.matches)
	}
	m.backspace()
	m.backspace()
	m.backspace()
	if len(m.matches) != len(sandboxes) {
		t.Errorf("backspacing everything left %v", m.matches)
	}
}

func TestMultiFilterNothingIsTickedToBeginWith(t *testing.T) {
	if got := newMultiFilterState(sandboxes).ticked(); len(got) != 0 {
		t.Errorf("ticked at the start: %v", got)
	}
}

func TestMultiFilterTabTicksTheHighlightedOneAndMovesDown(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	m.tickHighlighted()
	m.tickHighlighted()
	if got := m.ticked(); !reflect.DeepEqual(got, []int{0, 1}) || m.cursor != 2 {
		t.Errorf("ticked %v, cursor %d; want [0 1], 2", got, m.cursor)
	}
	m.move(-1)
	m.tickHighlighted()
	if got := m.ticked(); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("ticking a ticked one should untick it: %v", got)
	}
}

func TestMultiFilterTicksSurviveAFilterThatHidesThem(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	typing(m, "yumbi")
	m.tickShown()
	m.clearTyped()
	typing(m, "phoenix")
	m.tickShown()
	if got := m.ticked(); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Errorf("ticked = %v, want [0 1 2]: both filters' ticks, none lost", got)
	}
	if !strings.Contains(m.lines()[0], "(3 ticked)") {
		t.Errorf("the header should count every tick, shown or not: %q", m.lines()[0])
	}
}

func TestMultiFilterCtrlATicksOnlyWhatIsShownThenUnticksIt(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	typing(m, "yumbi")
	m.tickShown()
	if got := m.ticked(); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("ticked = %v, want only the two shown", got)
	}
	m.tickShown()
	if got := m.ticked(); len(got) != 0 {
		t.Errorf("a second ctrl-a should untick what is shown: %v", got)
	}
}

func TestMultiFilterCtrlATicksTheRestWhenSomeShownAreTicked(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	m.tickHighlighted()
	m.tickShown()
	if got := m.ticked(); len(got) != len(sandboxes) {
		t.Errorf("ticked = %v, want all", got)
	}
}

func TestMultiFilterClearTypedSaysWhetherThereWasAnything(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	if m.clearTyped() {
		t.Error("nothing typed, so escape should cancel, not clear")
	}
	typing(m, "ph")
	if !m.clearTyped() || m.typed != "" || len(m.matches) != len(sandboxes) {
		t.Errorf("escape should clear what is typed: %q, %v", m.typed, m.matches)
	}
}

func TestMultiFilterSpaceIsTypedNotATick(t *testing.T) {
	m := newMultiFilterState([]string{"a b", "ab"})
	typing(m, "a b")
	if !reflect.DeepEqual(m.matches, []int{0}) || len(m.ticked()) != 0 {
		t.Errorf("matches %v, ticked %v", m.matches, m.ticked())
	}
}

func TestMultiFilterWithNothingMatchingTicksNothingAndSaysSo(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	typing(m, "zzz")
	m.tickHighlighted()
	m.tickShown()
	if len(m.ticked()) != 0 || !strings.Contains(strings.Join(m.lines(), "\n"), "(no matches)") {
		t.Errorf("ticked %v, lines %q", m.ticked(), m.lines())
	}
}

func TestMultiFilterShowsEachLabelWithItsBoxAndTheHighlight(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	m.tickHighlighted()
	got := plain(m.lines())
	for _, want := range []string{"[x] yumbi-api", "❯ [ ] yumbi-web", "[ ] phoenix"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMultiFilterEscapeHintSaysWhatEscapeWillDo(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	if got := plain(m.lines()); !strings.Contains(got, "esc to quit") {
		t.Errorf("nothing typed: %s", got)
	}
	typing(m, "y")
	if got := plain(m.lines()); !strings.Contains(got, "esc to clear the filter") {
		t.Errorf("something typed: %s", got)
	}
}
