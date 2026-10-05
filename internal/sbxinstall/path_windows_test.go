package sbxinstall

import "testing"

// AddToUserPath must not add a folder that's already there under another
// spelling, or every --install would add it again.
func TestHasEntryMatchesHoweverTheFolderIsSpelled(t *testing.T) {
	t.Setenv("VIBE_TEST_HOME", `C:\Users\Me`)
	dir := `C:\Users\Me\.local\bin`
	for _, tt := range []struct {
		list string
		want bool
	}{
		{`C:\Windows;C:\Users\Me\.local\bin`, true},
		{`C:\Windows;c:\users\me\.LOCAL\bin\`, true},
		{`%VIBE_TEST_HOME%\.local\bin;C:\Windows`, true},
		{`C:\Windows;;`, false},
		{``, false},
		{`C:\Users\Me\.local\bin2`, false},
	} {
		if got := hasEntry(tt.list, dir); got != tt.want {
			t.Errorf("hasEntry(%q, %q) = %v, want %v", tt.list, dir, got, tt.want)
		}
	}
}
