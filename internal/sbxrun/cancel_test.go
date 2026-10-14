package sbxrun

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// hangingSbx puts an sbx on PATH that hangs, as a stuck sbx would.
func hangingSbx(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in sbx is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCancellingEndsAStuckSbx(t *testing.T) {
	hangingSbx(t)
	for name, call := range map[string]func(context.Context) error{
		"stop":   func(ctx context.Context) error { return StopQuietContext(ctx, "box") },
		"rm":     func(ctx context.Context) error { return RemoveContext(ctx, "box", true) },
		"create": func(ctx context.Context) error { return CreateContext(ctx, CreateOpts{Name: "box"}) },
		"start": func(ctx context.Context) error {
			if StartContext(ctx, "box") {
				t.Error("start: a sandbox that never came up was reported up")
			}
			return ctx.Err()
		},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		began := time.Now()
		time.AfterFunc(100*time.Millisecond, cancel)
		err := call(ctx)
		if took := time.Since(began); took > 5*time.Second {
			t.Errorf("%s: took %s after being cancelled", name, took)
		}
		if err == nil {
			t.Errorf("%s: no error from a cancelled call", name)
		}
		cancel()
	}
}

// askingSbx puts an sbx on PATH that asks a question and waits for the
// answer, as 'sbx create' does for a workspace folder that's gone.
func askingSbx(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in sbx is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'Would you like to create it? (y/N)'\nread answer || exit 1\n[ \"$answer\" = y ]\n"
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// With nobody to answer, a question from sbx fails the create at once,
// rather than waiting on a terminal for good.
func TestACreateWithNoInputFailsAtAQuestionInsteadOfWaiting(t *testing.T) {
	askingSbx(t)
	// A terminal nobody types into: reading it never ends.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	defer r.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = CreateContext(ctx, CreateOpts{Name: "box", NoInput: true})
	if ctx.Err() != nil {
		t.Fatal("the create waited for an answer nobody could give")
	}
	if err == nil {
		t.Error("the create succeeded, though sbx's question went unanswered")
	}
}
