package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"vibe/internal/prompt"
)

// stderrOf runs do with vibe's stderr captured, and says what it wrote.
func stderrOf(t *testing.T, do func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	was := os.Stderr
	os.Stderr = w
	do()
	os.Stderr = was
	w.Close()
	out, _ := io.ReadAll(r)
	r.Close()
	return string(out)
}

// styled makes vibe's output a terminal's (or not), for the test.
func styled(t *testing.T, on bool) {
	t.Helper()
	was := colourWanted
	colourWanted = func() bool { return on }
	t.Cleanup(func() { colourWanted = was })
}

// answering stands in for the user at the terminal: what they type is typed.
func typing(t *testing.T, typed string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, typed)
	was := openTerminalInput
	openTerminalInput = func() (*os.File, func(), bool) { return r, func() {}, true }
	t.Cleanup(func() { openTerminalInput = was; r.Close(); w.Close() })
}

const bright, plain = "\x1b[93m", "\x1b[0m"

func TestAQuestionIsBrightAndNothingAroundItIs(t *testing.T) {
	styled(t, true)
	typing(t, "\n")
	got := stderrOf(t, func() { confirmDefault(false, true, "Remove sandbox 'x'?") })
	if want := "vibe: " + bright + "Remove sandbox 'x'?" + plain + " [Y/n] "; got != want {
		t.Errorf("got %q, want %q: the prefix, the hint and the answer are not part of the question", got, want)
	}
}

func TestTheWholeOfAQuestionThatRunsOnIsBright(t *testing.T) {
	styled(t, true)
	typing(t, "n\n")
	question := "This will destroy the sandbox. Everything in it that isn't in the workspace is lost. Continue?"
	got := stderrOf(t, func() { confirmDefault(false, false, question) })
	if want := "vibe: " + bright + question + plain + " [y/N] "; got != want {
		t.Errorf("got %q", got)
	}
}

func TestNoStylingIsAddedWhenOutputIsNotAStyledTerminal(t *testing.T) {
	styled(t, false)
	typing(t, "\n")
	got := stderrOf(t, func() { confirmDefault(false, true, "Continue?") })
	if want := "vibe: Continue? [Y/n] "; got != want {
		t.Errorf("got %q: a log or a pipe, or NO_COLOR, gets the same words and no escapes", got)
	}
}

func TestNoColourStopsTheStylingToo(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := askText("Continue?"); got != "Continue?" {
		t.Errorf("askText with NO_COLOR = %q", got)
	}
}

func TestAQuestionOverAPickerIsBrightOnTheTerminalAndPlainInTheLog(t *testing.T) {
	styled(t, true)
	var logged []string
	teeNotes(func(line string) { logged = append(logged, line) })
	defer teeNotes(nil)
	got := stderrOf(t, func() { noteAs("which agent?", askText("which agent?")) })
	if want := "vibe: " + bright + "which agent?" + plain + "\n"; got != want {
		t.Errorf("the terminal got %q, want %q", got, want)
	}
	if len(logged) != 1 || logged[0] != "which agent?" {
		t.Errorf("the log got %q: the page's log has no use for escapes", logged)
	}
}

// A terminal for asking on: a zero Terminal will do, as the tests' ask does
// nothing with it (its Close copes with there being nothing to close).
func withAPickerTerminal(t *testing.T) {
	t.Helper()
	was := openTerminal
	openTerminal = func() (*prompt.Terminal, bool) { return &prompt.Terminal{}, true }
	t.Cleanup(func() { openTerminal = was })
}

func TestTheQuestionOfEveryPickerIsAskedInBrightOnTheTerminalAndPlainInTheLog(t *testing.T) {
	styled(t, true)
	withAPickerTerminal(t)
	var logged []string
	teeNotes(func(line string) { logged = append(logged, line) })
	defer teeNotes(nil)
	asked := false
	got := stderrOf(t, func() {
		asking("which agent should sandboxes run by default?", func(*prompt.Terminal) (int, bool) { asked = true; return 0, true })
	})
	if want := "vibe: " + bright + "which agent should sandboxes run by default?" + plain + "\n"; got != want {
		t.Errorf("the terminal got %q, want %q", got, want)
	}
	if len(logged) != 1 || logged[0] != "which agent should sandboxes run by default?" {
		t.Errorf("the log got %q: it has no use for escapes", logged)
	}
	if !asked {
		t.Error("the picker was never run")
	}
}

func TestTheQuestionAboutQuittingDuringTheMemoryBackupIsAskedInBright(t *testing.T) {
	styled(t, true)
	typing(t, "n\n")
	var quit bool
	got := stderrOf(t, func() { quit = confirmQuitDuringBackup(make(chan os.Signal)) })
	want := "vibe: " + bright + "vibe is currently exporting memories from the sandbox - are you sure you want to quit?" + plain + " [y/N] "
	if got != want || quit {
		t.Errorf("got %q (quit %v), want %q", got, quit, want)
	}
}

func TestWaitingForEnterAsksItsQuestionInBright(t *testing.T) {
	styled(t, true)
	typing(t, "\n")
	got := stderrOf(t, func() { waitForEnter("press enter to open the page") })
	if want := "vibe: " + bright + "press enter to open the page" + plain + " "; got != want {
		t.Errorf("got %q", got)
	}
}

// Anything that asks the user something on the terminal goes through askText,
// so a new prompt can't come out among the notes unnoticed. What asks is found
// by what it uses: the terminal's reader (ttyReader), its drawing and keys (ttyRW),
// a masked line (ReadMasked), or the terminal itself, opened without those two
// (openTerminal, openTerminalInput, prompt.Open, prompt.OpenInput); a function
// that does must say its question with askText too, except those that only look
// at whether there is a terminal, or are the wrappers that open it.
//
// It looks at whole functions: one that asked twice and styled one question would
// pass. None does today, so that is for whoever writes the first to know.
func TestEverythingThatAsksOnTheTerminalSaysItsQuestionWithAskText(t *testing.T) {
	notAsking := map[string]string{
		"interactive": "only looks for a terminal",
		"ttyReader":   "opens it",
		"ttyRW":       "opens it",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			uses, says := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch call := n.(type) {
				case *ast.CallExpr:
					switch f := call.Fun.(type) {
					case *ast.Ident:
						uses = uses || f.Name == "ttyReader" || f.Name == "ttyRW" || f.Name == "openTerminal" || f.Name == "openTerminalInput"
						says = says || f.Name == "askText"
					case *ast.SelectorExpr:
						uses = uses || f.Sel.Name == "ReadMasked"
						if pkg, ok := f.X.(*ast.Ident); ok && pkg.Name == "prompt" {
							uses = uses || f.Sel.Name == "Open" || f.Sel.Name == "OpenInput"
						}
					}
				}
				return true
			})
			if uses && !says {
				if _, fine := notAsking[fn.Name.Name]; !fine {
					missing = append(missing, path+": "+fn.Name.Name)
				}
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these ask on the terminal without askText, so their question won't stand out:\n  %s", strings.Join(missing, "\n  "))
	}
}
