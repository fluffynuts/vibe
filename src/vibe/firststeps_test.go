package main

import (
	"strings"
	"testing"
)

func TestAFirstInstallSaysHowToStartAndWhatToExpect(t *testing.T) {
	said := strings.Join(notesOf(func() { sayFirstSteps(true) }), "\n")
	for _, want := range []string{"vibe is installed", "vibe /path/to/folder", "folder", "companion page"} {
		if !strings.Contains(said, want) {
			t.Errorf("it doesn't say %q:\n%s", want, said)
		}
	}
	if !strings.Contains(said, "folder and run: vibe") {
		t.Errorf("it doesn't give the bare command:\n%s", said)
	}
}

func TestALaterInstallSaysNothingOfIt(t *testing.T) {
	if said := notesOf(func() { sayFirstSteps(false) }); len(said) != 0 {
		t.Errorf("said %q", said)
	}
}
