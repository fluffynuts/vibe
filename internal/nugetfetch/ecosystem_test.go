package nugetfetch

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// What goes back to the agent is the end of what dotnet said, cut by bytes: never through the middle of a character.
func TestTheEndOfWhatDotnetSaidIsNeverCutThroughACharacter(t *testing.T) {
	for _, char := range []string{"é", "€", "日", "😀"} {
		for pad := 0; pad < 4; pad++ {
			text := strings.Repeat(char, maxReported) + strings.Repeat("x", pad)
			got := reportable(text)
			if !utf8.ValidString(got) {
				t.Errorf("%q repeated, then %d plain bytes: not valid UTF-8", char, pad)
			}
			if len(got) > maxReported+len("…") {
				t.Errorf("%q repeated: %d bytes, over the limit", char, len(got))
			}
		}
	}
}

func TestWhatDotnetSaidWithinTheLimitIsAsItWasSaid(t *testing.T) {
	if got := reportable("  NU1101: nothing  \n"); got != "NU1101: nothing" {
		t.Errorf("got %q", got)
	}
}
