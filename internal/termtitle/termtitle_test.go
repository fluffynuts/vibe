package termtitle

import (
	"bytes"
	"testing"
)

func TestSequence(t *testing.T) {
	tests := []struct{ title, want string }{
		{"VIBE: ~/code/vibe", "\x1b]0;VIBE: ~/code/vibe\a"},
		{"VIBE: C:\\Users\\me\\proj", "\x1b]0;VIBE: C:\\Users\\me\\proj\a"},
		{"a\x07b\x1b]0;evil\nc\u009bd", "\x1b]0;ab]0;evilcd\a"},
		{"VIBE: ~/проект", "\x1b]0;VIBE: ~/проект\a"},
	}
	for _, tt := range tests {
		if got := sequence(tt.title); got != tt.want {
			t.Errorf("sequence(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestRestorePopsOnceAfterSet(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	Restore()
	Restore()
	if got := buf.String(); got != pop {
		t.Errorf("Restore wrote %q, want one %q", got, pop)
	}
}
