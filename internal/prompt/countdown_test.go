package prompt

import (
	"reflect"
	"strconv"
	"testing"
)

// keys stands in for the user: each second of the countdown gets the next
// entry's keypresses, "" being a second that passes with none.
func keys(seconds ...string) func(int) (byte, bool) {
	return func(int) (byte, bool) {
		for len(seconds) > 0 {
			if seconds[0] == "" {
				seconds = seconds[1:]
				return 0, false
			}
			b := seconds[0][0]
			seconds[0] = seconds[0][1:]
			return b, true
		}
		return 0, false
	}
}

func TestCountdown(t *testing.T) {
	for _, tt := range []struct {
		name  string
		keys  []string
		want  CountdownAnswer
		drawn []string
	}{
		{"runs out", nil, CountdownGo, []string{"3", "2", "1"}},
		{"enter goes now", []string{"", "\r"}, CountdownGo, []string{"3", "2"}},
		{"s skips", []string{"s"}, CountdownSkip, []string{"3"}},
		{"ctrl-c quits", []string{"", "", "\x03"}, CountdownQuit, []string{"3", "2", "1"}},
		{"q quits", []string{"q"}, CountdownQuit, []string{"3"}},
		{"other keys are ignored", []string{"xy", "", ""}, CountdownGo, []string{"3", "2", "1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var drawn []string
			got := countdown(func(s string) { drawn = append(drawn, s) }, keys(tt.keys...), strconv.Itoa, 3)
			if got != tt.want {
				t.Errorf("answer = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(drawn, tt.drawn) {
				t.Errorf("drawn = %v, want %v", drawn, tt.drawn)
			}
		})
	}
}
