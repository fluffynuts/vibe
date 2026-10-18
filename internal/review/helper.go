package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// A repository is read in a helper process: vibe running itself, with
// helperEnv set. What go-git does with what the sandbox wrote (inflate a
// zlib bomb, walk a history made long) can't be bounded from in here; in a
// helper, it costs only the helper, held to helperMemory and killed when
// its time is up.

const helperEnv = "VIBE_REVIEW_HELPER"

// helperMemory is the most heap the helper may use, in MB
// (VIBE_REVIEW_HELPER_MEMORY, for a test, says otherwise): far more than
// any diff the tab shows needs.
func helperMemory() int64 {
	if mb, err := strconv.ParseInt(os.Getenv("VIBE_REVIEW_HELPER_MEMORY"), 10, 64); err == nil && mb > 0 {
		return mb << 20
	}
	return 1 << 30
}

// maxAnswer bounds what's read of the helper's answer.
const maxAnswer = 512 << 20

type helperRequest struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

type helperAnswer struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type helperArgs struct {
	Dir    string    `json:"dir"`
	Ref    string    `json:"ref,omitempty"`
	Path   string    `json:"path,omitempty"`
	Review Review    `json:"review"`
	How    []Diffing `json:"how,omitempty"`
}

// ServeHelperIfAsked, run first thing, serves as the helper when this
// process was started as one (see isolated), and exits; otherwise it does
// nothing. vibe's main runs it, and so must any test binary whose tests
// read a repository.
func ServeHelperIfAsked() {
	if os.Getenv(helperEnv) == "" {
		return
	}
	limit := helperMemory()
	limitOwnMemory(limit)
	debug.SetMemoryLimit(limit)
	go watchMemory(limit)
	answer := serve(os.Stdin)
	out, _ := json.Marshal(answer)
	os.Stdout.Write(out)
	os.Exit(0)
}

// watchMemory ends the helper once its heap is over limit, saying so: an
// allocation can't be refused, but the process can end before it's done.
func watchMemory(limit int64) {
	var m runtime.MemStats
	for {
		time.Sleep(10 * time.Millisecond)
		runtime.ReadMemStats(&m)
		if int64(m.HeapAlloc) > limit {
			fmt.Fprintf(os.Stderr, "reading the repository needed more memory than vibe allows it (%d MB): it may be damaged, or built to exhaust memory\n", limit>>20)
			os.Exit(3)
		}
	}
}

func serve(in io.Reader) helperAnswer {
	var req helperRequest
	if err := json.NewDecoder(io.LimitReader(in, 64<<20)).Decode(&req); err != nil {
		return helperAnswer{Error: "the helper couldn't read what it was asked: " + err.Error()}
	}
	var a helperArgs
	if err := json.Unmarshal(req.Args, &a); err != nil {
		return helperAnswer{Error: "the helper couldn't read what it was asked: " + err.Error()}
	}
	ctx := context.Background()
	var result any
	var err error
	switch req.Op {
	case "workingTree":
		result, err = workingTree(ctx, a.Dir, a.How...)
	case "against":
		result, err = against(ctx, a.Dir, a.Ref, a.How...)
	case "commit":
		result, err = commit(ctx, a.Dir, a.Ref, a.How...)
	case "unpushed":
		result, err = unpushed(ctx, a.Dir, a.Ref, a.How...)
	case "root":
		result, err = root(ctx, a.Dir)
	case "options":
		result, err = optionsOf(ctx, a.Dir)
	case "defaultBranch":
		result, err = defaultBranchOf(a.Dir)
	case "status":
		result, err = statusOf(ctx, a.Dir)
	case "moves":
		result = moves(ctx, a.Dir, a.Review)
	case "fileLines":
		result, err = fileLines(ctx, a.Dir, a.Review, a.Path)
	case "newTextOf":
		result, err = newTextOf(ctx, a.Dir, a.Review, a.Path)
	case "oldTextOf":
		result, err = oldTextOf(ctx, a.Dir, a.Review, a.Path)
	default:
		err = fmt.Errorf("the helper has no %q", req.Op)
	}
	if err != nil {
		return helperAnswer{Error: err.Error()}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return helperAnswer{Error: err.Error()}
	}
	return helperAnswer{Result: data}
}

// isolated runs op with args in a helper process, and decodes its result
// into out: ended, its process killed, when ctx is.
func isolated(ctx context.Context, op string, args helperArgs, out any) error {
	if os.Getenv(helperEnv) != "" {
		// A helper that isn't serving: a program that never ran
		// ServeHelperIfAsked. Starting another would only do the same.
		return errors.New("this program can't read repositories: it doesn't serve as vibe's helper")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("vibe can't find itself to read the repository: %w", err)
	}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		return err
	}
	req, err := json.Marshal(helperRequest{Op: op, Args: rawArgs})
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	cmd.Stdin = bytes.NewReader(req)
	stdout := &capped{max: maxAnswer}
	stderr := &capped{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("reading the repository took too long, and was stopped: %w", ctx.Err())
		}
		return fmt.Errorf("vibe couldn't start its helper to read the repository: %w", err)
	}
	release := limitChildMemory(cmd.Process, helperMemory())
	err = cmd.Wait()
	release()
	if ctx.Err() != nil {
		return fmt.Errorf("reading the repository took too long, and was stopped: %w", ctx.Err())
	}
	var answer helperAnswer
	if jerr := json.Unmarshal(stdout.Bytes(), &answer); jerr != nil || (answer.Result == nil && answer.Error == "") {
		why := strings.TrimSpace(stderr.String())
		if why == "" && err != nil {
			why = err.Error()
		}
		if stdout.over {
			why = "its answer was too large"
		}
		return fmt.Errorf("vibe's helper failed reading the repository: %s", lastLine(why))
	}
	if answer.Error != "" {
		return errors.New(answer.Error)
	}
	return json.Unmarshal(answer.Result, out)
}

// lastLine is text's last line: what a process said as it ended, past what
// it said before (a Go panic's trace, say).
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "goroutine ") {
			return l
		}
	}
	return text
}

// capped keeps up to max bytes written to it, and says when there were
// more.
type capped struct {
	bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.Len(); room < len(p) {
		c.over = true
		if room > 0 {
			c.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return c.Buffer.Write(p)
}

// The repository's reading, each in a helper (see isolated).

// WorkingTree is the diff of the folder dir as it is: everything not yet
// committed, staged or not, and untracked files (those git doesn't ignore)
// as added.
func WorkingTree(ctx context.Context, dir string, how ...Diffing) ([]File, error) {
	var out []File
	return out, isolated(ctx, "workingTree", helperArgs{Dir: dir, How: how}, &out)
}

// Against is the diff of the folder dir as it is from where it parted from
// ref (a branch, say): this branch's changes, committed or not, without
// those ref has gained since.
func Against(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	var out []File
	return out, isolated(ctx, "against", helperArgs{Dir: dir, Ref: ref, How: how}, &out)
}

// Commit is the diff of what commit ref changed: a merge commit's, what it
// brought in to its first parent.
func Commit(ctx context.Context, dir, ref string, how ...Diffing) ([]File, error) {
	var out []File
	return out, isolated(ctx, "commit", helperArgs{Dir: dir, Ref: ref, How: how}, &out)
}

// Unpushed is the diff of the commits on HEAD that upstream hasn't: what
// they changed, from where the two parted, without anything uncommitted.
func Unpushed(ctx context.Context, dir, upstream string, how ...Diffing) ([]File, error) {
	var out []File
	return out, isolated(ctx, "unpushed", helperArgs{Dir: dir, Ref: upstream, How: how}, &out)
}

// Root is the top of the repository folder dir is in: what its paths are
// relative to. It's dir or a folder above it, whatever the repository's
// config says.
func Root(ctx context.Context, dir string) (string, error) {
	var out string
	return out, isolated(ctx, "root", helperArgs{Dir: dir}, &out)
}

// DefaultBranch is the main line of the repository the folder dir is in:
// the branch origin's HEAD names, else the one set for it; none is guessed.
// An error is the repository not being read.
func DefaultBranch(ctx context.Context, dir string) (Mainline, error) {
	var out Mainline
	return out, isolated(ctx, "defaultBranch", helperArgs{Dir: dir}, &out)
}

// Options are the reviews the folder dir can be started on.
func Options(ctx context.Context, dir string) (StartOptions, error) {
	var out StartOptions
	return out, isolated(ctx, "options", helperArgs{Dir: dir}, &out)
}

// StatusOf is the git status of the folder dir; an error when it's in no
// repository (or one that can't be read).
func StatusOf(ctx context.Context, dir string) (*FolderStatus, error) {
	var out FolderStatus
	if err := isolated(ctx, "status", helperArgs{Dir: dir}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Moves are rv's comments on the new side whose code (as it read when
// they were made) is no longer at their lines but is elsewhere in the file:
// where to. What can't be read moves nothing: its comments stay, and the
// page marks any whose lines read differently.
func Moves(ctx context.Context, dir string, rv Review) []Move {
	var out []Move
	if isolated(ctx, "moves", helperArgs{Dir: dir, Review: rv}, &out) != nil {
		return nil
	}
	return out
}

// FileLines is path's lines, as the diff of r has its new side (see
// NewTextOf). They're what the Review tab shows when asked for more of a
// file's context than the diff has.
func FileLines(ctx context.Context, dir string, r Review, path string) ([]string, error) {
	var out []string
	return out, isolated(ctx, "fileLines", helperArgs{Dir: dir, Review: r, Path: path}, &out)
}

// NewTextOf is path's text as review r has its new side: in the commit r is
// of, at HEAD for unpushed commits, else in the folder dir as it is.
func NewTextOf(ctx context.Context, dir string, r Review, path string) (string, error) {
	var out string
	return out, isolated(ctx, "newTextOf", helperArgs{Dir: dir, Review: r, Path: path}, &out)
}

// OldTextOf is path's text where review rv's diff starts from: the commit
// before rv's, where its branch parted from its ref, or the folder's last
// commit.
func OldTextOf(ctx context.Context, dir string, rv Review, path string) (string, error) {
	var out string
	return out, isolated(ctx, "oldTextOf", helperArgs{Dir: dir, Review: rv, Path: path}, &out)
}
