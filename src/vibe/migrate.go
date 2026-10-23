package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"vibe/internal/cliargs"
	"vibe/internal/fscopy"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// migrateRebuild is rebuildSandboxTo, folderClaims running.Claims, and
// sameFilesystem onOneFilesystem: variables for tests.
var (
	migrateRebuild = rebuildSandboxTo
	folderClaims   = running.Claims
	sameFilesystem = onOneFilesystem
)

// confirmMigration asks whether to go ahead with the migration steps lists:
// the sandbox is rebuilt, so only -f goes ahead unasked (and with no
// terminal, it's refused, saying to add -f). Yes is the default: asking for
// --migrate is asking for this, and the question is there to show what it
// takes. A variable for tests.
var confirmMigration = func(force bool, steps []string) bool {
	if !force {
		for _, step := range steps {
			note("  %s", step)
		}
	}
	return confirmDefault(force, true, "Go ahead?")
}

// doMigrate is --migrate: folder from, which has sandbox name, moves to to.
// The sandbox goes with it: as sbx can't move a sandbox to another folder,
// it's rebuilt there, under the name the new folder gives it, with its
// agent's memories, conversation, GitHub token, clipboard history, reviews and the
// rest of what vibe keeps for it carried across. The folder isn't moved
// until the old sandbox is gone (its memories are read out of it first),
// and nothing at all changes if a check fails or the user says no.
func doMigrate(lay layout.Layout, args cliargs.Args, name, from, to string) error {
	to, err := filepath.Abs(to)
	if err != nil {
		return err
	}
	newName, err := checkMigration(lay.Home, name, from, to, sbxrun.Exists)
	if err != nil {
		return err
	}
	if claims := folderClaims(lay.Home, from); len(claims) > 0 {
		var pids []int
		for _, c := range claims {
			pids = append(pids, c.PID)
		}
		return fmt.Errorf("vibe (%s) is running for %s — end it first ('vibe --stop %s' ends it, and stops the sandbox), then migrate", pidList(pids), from, from)
	}
	steps := []string{
		fmt.Sprintf("move %s to %s", pathresolve.Tilde(from), pathresolve.Tilde(to)),
		fmt.Sprintf("rebuild sandbox '%s' there as '%s': its agent's memories, conversation, GitHub token, clipboard history and reviews go with it;", name, newName),
		"  anything installed in it by hand, or kept outside its folder, doesn't",
	}
	if newName == name {
		steps[1] = fmt.Sprintf("rebuild sandbox '%s' there: its agent's memories, conversation, GitHub token, clipboard history and reviews go with it;", name)
	}
	note("migrating:")
	if !confirmMigration(args.Force, steps) {
		return fmt.Errorf("aborted — nothing moved")
	}
	relocate := func() (string, string, error) {
		note("moving %s to %s", pathresolve.Tilde(from), pathresolve.Tilde(to))
		if err := os.Rename(from, to); err != nil {
			return "", "", fmt.Errorf("the old sandbox is gone, but %s couldn't be moved: %w — it's where it was, with what vibe keeps for it: 'vibe -r %s' builds its sandbox again there", from, err, from)
		}
		if err := migrateRecords(lay.Home, name, newName, from, to); err != nil {
			return "", "", fmt.Errorf("%s is now %s, but what vibe keeps for its sandbox couldn't all be moved: %w", from, to, err)
		}
		return newName, to, nil
	}
	if err := migrateRebuild(context.Background(), lay, args, name, from, true, false, relocate); err != nil {
		return err
	}
	note("%s is now %s, with sandbox '%s' — 'vibe' there starts it", pathresolve.Tilde(from), pathresolve.Tilde(to), newName)
	return nil
}

// checkMigration says what sandbox name, for folder from, will be called
// once from moves to to, or why it can't move: anything that would make
// the move fail part-way is caught here, before anything changes.
func checkMigration(vibeHome, name, from, to string, sbxHas func(string) (bool, error)) (string, error) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		return "", err
	}
	if !found || inst.Target != from {
		return "", fmt.Errorf("vibe has no sandbox for %s — only a folder vibe has made a sandbox for can be migrated", from)
	}
	if _, err := os.Lstat(to); err == nil {
		return "", fmt.Errorf("%s already exists — vibe moves the folder there itself, so give a path that isn't taken", to)
	}
	parent := filepath.Dir(to)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s doesn't exist — make it first, then migrate", parent)
	}
	if pathresolve.Within(from, to) {
		return "", fmt.Errorf("%s is inside it — a folder can't be moved into itself", to)
	}
	newName := pathresolve.DeriveName(to)
	if newName != name {
		inSbx, err := sbxHas(newName)
		if err != nil {
			return "", err
		}
		if _, taken, _ := state.Load(vibeHome, newName); taken || inSbx {
			return "", fmt.Errorf("a sandbox called '%s' already exists, and the folder's new name would give it that name — pick another name for the folder, or delete that sandbox first ('vibe --cleanup')", newName)
		}
	}
	if !sameFilesystem(from, parent) {
		return "", fmt.Errorf("%s is on another disk from %s — vibe only moves a folder within a disk; copy it there yourself, then run vibe in the copy for a sandbox of its own", parent, from)
	}
	return newName, nil
}

// notProjectChars is what the agent (Claude Code) turns into "-" when it
// names the folder it keeps a project's memories and transcripts in, under
// ~/.claude/projects, after the project's path.
var notProjectChars = regexp.MustCompile(`[^A-Za-z0-9]`)

// projectFolder is the name the agent gives the folder it keeps the project
// at path in: /home/me/code/app is -home-me-code-app.
func projectFolder(path string) string {
	return notProjectChars.ReplaceAllString(path, "-")
}

// migrateRecords moves everything vibe keeps for sandbox oldName, made for
// folder from, to newName, for folder to (the two names may be the same).
// The agent's memories and kept transcript are filed under the new
// folder's project name, as the agent will look for them there.
func migrateRecords(vibeHome, oldName, newName, from, to string) error {
	inst, found, err := state.Load(vibeHome, oldName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("vibe has no record of a sandbox '%s'", oldName)
	}
	if err := state.Move(vibeHome, oldName, newName); err != nil {
		return err
	}
	if err := state.MoveIfThere(headlessLog(vibeHome, oldName), headlessLog(vibeHome, newName)); err != nil {
		return err
	}
	store := inst.MemoryStore
	// A store of vibe's own is named after the sandbox, so it follows the
	// name; one somewhere else (a profile's memoryRoot) is left where it is.
	if dir, base := filepath.Split(store); store != "" && base == oldName {
		moved := filepath.Join(dir, newName)
		if err := state.MoveIfThere(store, moved); err != nil {
			return err
		}
		store = moved
	}
	if store != "" {
		if err := state.MoveIfThere(filepath.Join(store, projectFolder(from)), filepath.Join(store, projectFolder(to))); err != nil {
			return err
		}
	}
	if err := refileTranscript(state.TranscriptPath(vibeHome, newName), projectFolder(from), projectFolder(to)); err != nil {
		return err
	}
	inst, _, err = state.Load(vibeHome, newName)
	if err != nil {
		return err
	}
	inst.Target, inst.MemoryStore = to, store
	return state.Save(vibeHome, inst)
}

// refileTranscript rewrites the kept transcript at file (a gzipped tar of
// the agent's ~/.claude) so what was under projects/<from> is under
// projects/<to>. There may be none.
func refileTranscript(file, from, to string) error {
	data, err := fscopy.ReadFile(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}
	var out bytes.Buffer
	gzOut := gzip.NewWriter(&out)
	tw := tar.NewWriter(gzOut)
	tr := tar.NewReader(gz)
	oldDir, newDir := "projects/"+from, "projects/"+to
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		name := strings.TrimPrefix(h.Name, "./")
		if name == oldDir || name == oldDir+"/" {
			h.Name = newDir + "/"
		} else if rest, ok := strings.CutPrefix(name, oldDir+"/"); ok {
			h.Name = newDir + "/" + rest
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gzOut.Close(); err != nil {
		return err
	}
	return fscopy.WriteWhole(file, out.Bytes())
}
