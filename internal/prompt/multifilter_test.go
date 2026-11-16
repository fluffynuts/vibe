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

func TestMultiFilterSpaceTicksTheHighlightedOneAndStaysPut(t *testing.T) {
	m := newMultiFilterState(sandboxes)
	m.tickHighlighted()
	if got := m.ticked(); !reflect.DeepEqual(got, []int{0}) || m.cursor != 0 {
		t.Errorf("ticked %v, cursor %d; want [0], 0: as in every checklist, the highlight stays", got, m.cursor)
	}
	m.tickHighlighted()
	if got := m.ticked(); len(got) != 0 {
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
	m.move(1)
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

// pressed drives m with keys as the terminal would give them: an Esc is
// followed by what is in escapes, in turn, as readEscape would read.
type keyboard struct {
	m       *multiFilterState
	escapes []escKey
	last    keyResult
}

func newKeyboard(labels []string, escapes ...escKey) *keyboard {
	return &keyboard{m: newMultiFilterState(labels), escapes: escapes}
}

func (k *keyboard) press(keys ...byte) *keyboard {
	for _, key := range keys {
		k.last = k.m.press(key, func() escKey {
			next := k.escapes[0]
			k.escapes = k.escapes[1:]
			return next
		})
	}
	return k
}

func (k *keyboard) type_(s string) *keyboard { return k.press([]byte(s)...) }

const (
	keySpace = ' '
	keyCtrlA = 0x01
	keyCtrlU = 0x15
	keyEsc   = 0x1b
	keyEnter = '\r'
)

func TestMultiFilterKeysSpaceTicksAndIsNotTyped(t *testing.T) {
	k := newKeyboard(sandboxes).press(keySpace)
	if !reflect.DeepEqual(k.m.ticked(), []int{0}) || k.m.typed != "" {
		t.Errorf("ticked %v, typed %q: space ticks the highlighted one and is not part of the filter", k.m.ticked(), k.m.typed)
	}
}

func TestMultiFilterKeysLettersAreTypedNotCommands(t *testing.T) {
	k := newKeyboard(sandboxes).type_("jkq")
	if k.m.typed != "jkq" || k.last != keyChanged {
		t.Errorf("typed %q, last %v: j, k and q are letters here, as in Filter", k.m.typed, k.last)
	}
}

func TestMultiFilterKeysArrowsMoveTheHighlight(t *testing.T) {
	k := newKeyboard(sandboxes, escDown, escDown, escUp).press(keyEsc, keyEsc, keyEsc)
	if k.m.cursor != 1 {
		t.Errorf("cursor %d, want 1 after down, down, up", k.m.cursor)
	}
	k.press(keySpace)
	if !reflect.DeepEqual(k.m.ticked(), []int{1}) {
		t.Errorf("ticked %v: space ticks where the arrows left the highlight", k.m.ticked())
	}
}

func TestMultiFilterKeysCtrlATicksShownAndCtrlUClears(t *testing.T) {
	k := newKeyboard(sandboxes).type_("yumbi").press(keyCtrlA)
	if !reflect.DeepEqual(k.m.ticked(), []int{0, 1}) {
		t.Errorf("ticked %v, want the two shown", k.m.ticked())
	}
	k.press(keyCtrlU)
	if k.m.typed != "" || len(k.m.matches) != len(sandboxes) {
		t.Errorf("ctrl-u left %q, %v", k.m.typed, k.m.matches)
	}
}

func TestMultiFilterKeysBackspaceWidens(t *testing.T) {
	k := newKeyboard(sandboxes).type_("yz").press(0x7f)
	if k.m.typed != "y" || len(k.m.matches) != 2 {
		t.Errorf("typed %q, matches %v", k.m.typed, k.m.matches)
	}
}

func TestMultiFilterKeysEscapeClearsWhatIsTypedAndThenCancels(t *testing.T) {
	k := newKeyboard(sandboxes, escBare, escBare).type_("ph").press(keyEsc)
	if k.last != keyChanged || k.m.typed != "" {
		t.Errorf("first escape: %v, typed %q: it should clear the filter and go on", k.last, k.m.typed)
	}
	k.press(keyEsc)
	if k.last != keyCancel {
		t.Errorf("second escape: %v, want it to cancel", k.last)
	}
}

func TestMultiFilterKeysEnterAnswersWithEveryTickIncludingOnesTheFilterHides(t *testing.T) {
	k := newKeyboard(sandboxes).type_("yumbi").press(keySpace)
	k.press(keyCtrlU).type_("phoenix").press(keyCtrlA)
	k.type_("zzz") // nothing matches now: the answer is still what was ticked
	k.press(keyEnter)
	if k.last != keyDone || !reflect.DeepEqual(k.m.ticked(), []int{0, 2}) {
		t.Errorf("last %v, ticked %v, want done with [0 2]", k.last, k.m.ticked())
	}
}

func TestMultiFilterKeysEnterWithNothingTickedIsAnAnswerOfNone(t *testing.T) {
	if k := newKeyboard(sandboxes).press(keyEnter); k.last != keyDone || len(k.m.ticked()) != 0 {
		t.Errorf("last %v, ticked %v", k.last, k.m.ticked())
	}
}

func TestMultiFilterKeysCtrlCCancelsAndTabAndOtherKeysAreIgnored(t *testing.T) {
	if k := newKeyboard(sandboxes).press(3); k.last != keyCancel {
		t.Errorf("ctrl-c: %v", k.last)
	}
	for _, key := range []byte{'\t', 0x02} {
		if k := newKeyboard(sandboxes).press(key); k.last != keyIgnored || len(k.m.ticked()) != 0 || k.m.typed != "" {
			t.Errorf("key %#x: %v, ticked %v, typed %q: tab is no longer the tick key", key, k.last, k.m.ticked(), k.m.typed)
		}
	}
	if k := newKeyboard(sandboxes, escKey(99)).press(keyEsc); k.last != keyIgnored {
		t.Errorf("an escape sequence that is no arrow: %v, want it ignored", k.last)
	}
}
