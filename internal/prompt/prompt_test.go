package prompt

import "testing"

func TestHasShiftModifier(t *testing.T) {
	cases := []struct {
		params string
		want   bool
	}{
		{"", false},    // plain arrow: "ESC [ A", no parameters at all
		{"1", false},   // malformed/incomplete: no modifier field
		{"1;2", true},  // Shift alone
		{"1;3", false}, // Alt alone
		{"1;4", true},  // Shift+Alt
		{"1;5", false}, // Ctrl alone
		{"1;6", true},  // Shift+Ctrl
		{"1;9", false}, // Meta alone
		{"1;10", true}, // Shift+Meta
		{"1;x", false}, // unparseable modifier field
	}
	for _, c := range cases {
		if got := hasShiftModifier([]byte(c.params)); got != c.want {
			t.Errorf("hasShiftModifier(%q) = %v, want %v", c.params, got, c.want)
		}
	}
}
