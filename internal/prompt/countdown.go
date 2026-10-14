package prompt

// CountdownAnswer is how a countdown ended.
type CountdownAnswer int

const (
	// CountdownGo: the time ran out, or the user pressed Enter to go now.
	CountdownGo CountdownAnswer = iota
	// CountdownSkip: the user pressed s, to skip what was counted down to.
	CountdownSkip
	// CountdownQuit: the user pressed Ctrl-C or q.
	CountdownQuit
)

// Countdown shows line(left) on rw, once a second for seconds seconds, until
// the time runs out or the user answers: Enter goes now, s skips, Ctrl-C or
// q quits. ok is false when the terminal couldn't be put into raw mode, so
// nothing was shown and nobody could answer.
func Countdown(rw *Terminal, line func(left int) string, seconds int) (answer CountdownAnswer, ok bool) {
	f, err := openFrame(rw)
	if err != nil {
		return CountdownGo, false
	}
	defer f.close()
	answer = countdown(func(s string) { f.draw([]string{s}) }, rw.waitByte, line, seconds)
	f.clear()
	return answer, true
}

// countdown is Countdown's loop. key waits up to ms milliseconds for a
// keypress, reporting false when none came.
func countdown(draw func(string), key func(ms int) (byte, bool), line func(left int) string, seconds int) CountdownAnswer {
	for left := seconds; left > 0; left-- {
		draw(line(left))
		for {
			b, ok := key(1000)
			if !ok {
				break
			}
			switch b {
			case '\r', '\n':
				return CountdownGo
			case 's', 'S':
				return CountdownSkip
			case 3, 'q', 'Q':
				return CountdownQuit
			}
		}
	}
	return CountdownGo
}
