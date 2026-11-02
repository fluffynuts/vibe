package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/sbxrun"
)

// orphanWithin bounds the sbx calls made to look for an orphaned sandbox.
const orphanWithin = 30 * time.Second

// orphanSteps are what clearing an orphaned sandbox does to the machine, apart so that tests can answer for it.
type orphanSteps struct {
	list   func(ctx context.Context) ([]sbxrun.Status, error)
	exists func(path string) bool
	// home is the vibe home (~/.vibe): the folders in it are vibe's own.
	home   string
	canAsk bool
	// confirm asks a yes/no question, answering def for Enter.
	confirm func(question string, def bool) bool
	remove  func(ctx context.Context, name string) error
	say     func(line string)
}

// missingWorkspaces are the mounted folders that are gone from the host: sbx lists a read-only one with a :ro (or :rw)
// after its path, which isn't part of the path.
func missingWorkspaces(workspaces []string, exists func(path string) bool) []string {
	var missing []string
	for _, w := range workspaces {
		path := strings.TrimSuffix(strings.TrimSuffix(w, ":ro"), ":rw")
		if !exists(path) {
			missing = append(missing, path)
		}
	}
	return missing
}

// clearOrphanedSandbox deals with a sandbox sbx still has whose folders on the host are gone, which can't start: what is
// left when ~/.vibe is deleted, or a setup half done. It says so, and offers on the terminal to remove it, so that the
// start goes on to make it afresh (yes is the default only when what is gone is vibe's own folders: nothing in it can be got at). With no terminal, or -f, it is
// never removed unasked: the command to run is given.
func clearOrphanedSandbox(args cliargs.Args, name, vibeHome string) error {
	return clearOrphanedSandboxWith(context.Background(), name, args, orphanSteps{
		list: func(ctx context.Context) ([]sbxrun.Status, error) {
			ctx, cancel := context.WithTimeout(ctx, orphanWithin)
			defer cancel()
			return sbxrun.List(ctx)
		},
		exists:  func(path string) bool { _, err := os.Stat(path); return err == nil },
		home:    vibeHome,
		canAsk:  interactive(),
		confirm: func(question string, def bool) bool { return confirmDefault(false, def, question) },
		remove: func(ctx context.Context, name string) error {
			return sbxrun.Remove(ctx, name, true, nil)
		},
		say: func(line string) { note("%s", line) },
	})
}

func clearOrphanedSandboxWith(ctx context.Context, name string, args cliargs.Args, s orphanSteps) error {
	statuses, err := s.list(ctx)
	if err != nil {
		return nil // sbx's own error, from what the start asks of it, says why better
	}
	var missing []string
	for _, st := range statuses {
		if st.Name == name {
			missing = missingWorkspaces(st.Workspaces, s.exists)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	byHand := fmt.Errorf("remove it with 'sbx rm %s', then run vibe again to make it afresh", name)
	var foreign []string // gone, and not vibe's own
	for _, m := range missing {
		if !under(m, s.home) {
			foreign = append(foreign, m)
		}
	}
	s.say(fmt.Sprintf("sandbox '%s' exists in sbx, but folders it mounts are gone from this machine, so it can't start:", name))
	for _, m := range missing {
		s.say("  " + m)
	}
	if len(foreign) == 0 {
		s.say("  (probably left by a deleted ~/.vibe, or a setup that wasn't finished)")
	} else {
		s.say("  (" + strings.Join(foreign, ", ") + " isn't one of vibe's own folders: if it is on a drive that isn't plugged in, or was moved, put it back and the sandbox starts again)")
	}
	if args.Force || !s.canAsk {
		return byHand
	}
	// Yes is the default only when all that is gone is vibe's own: nothing then can be got at, and it can't start. A project
	// folder (or another mounted one) may be coming back, and Enter must not delete a sandbox that would still be wanted.
	question := fmt.Sprintf("Remove sandbox '%s' and make it afresh? Its own disk goes with it; your project folder is untouched.", name)
	if len(foreign) > 0 {
		question = fmt.Sprintf("Remove sandbox '%s' and make it afresh, though %s is missing and may come back? Its own disk goes with it.", name, strings.Join(foreign, ", "))
	}
	if !s.confirm(question, len(foreign) == 0) {
		return byHand
	}
	if err := s.remove(ctx, name); err != nil {
		return fmt.Errorf("couldn't remove sandbox '%s': %w", name, err)
	}
	s.say(fmt.Sprintf("removed sandbox '%s'", name))
	return nil
}

// under says path is inside dir, or is it: by what the paths say, not what is on disk (the folders are gone).
func under(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
