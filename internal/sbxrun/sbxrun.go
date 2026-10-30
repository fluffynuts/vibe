// Package sbxrun wraps the external `sbx` (Docker Sandboxes) CLI.
package sbxrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"vibe/internal/sbxfind"
)

// Available reports whether there's an sbx to run: on PATH, or where it's
// installed. It's looked for, not run: every vibe asks first thing, and one
// that's there but broken says why itself at the first command.
func Available() bool {
	return sbxfind.Available()
}

// Agents lists the agents sbx can run, read from the "Available agents:"
// line of sbx run --help.
func Agents() ([]string, error) {
	out, err := exec.Command(sbxfind.Path(), "run", "--help").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sbx run --help: %w", err)
	}
	agents := ParseAgents(string(out))
	if len(agents) == 0 {
		return nil, fmt.Errorf("sbx run --help lists no agents")
	}
	return agents, nil
}

// ParseAgents reads the agents from sbx run --help's output, whose line
// "Available agents: claude, codex, ..." lists them.
func ParseAgents(help string) []string {
	for _, line := range strings.Split(help, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Available agents:")
		if !ok {
			continue
		}
		var agents []string
		for _, a := range strings.Split(rest, ",") {
			if a = strings.TrimSpace(a); a != "" {
				agents = append(agents, a)
			}
		}
		return agents
	}
	return nil
}

// captureOutContext runs sbx with args and returns what it printed, stdout
// and stderr together, killing sbx if ctx is cancelled.
func captureOutContext(ctx context.Context, args ...string) (string, error) {
	cmd := cancellable(ctx, args...)
	var out capped
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := out.run(cmd)
	return out.String(), err
}

// captureSplitContext is captureOutContext, keeping what sbx prints on
// stderr apart from stdout, for output that's read as data.
func captureSplitContext(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	return split(cancellable(ctx, args...))
}

// split runs cmd, giving what it printed on stdout and on stderr apart.
func split(cmd *exec.Cmd) (stdout, stderr string, err error) {
	var out, errOut capped
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = out.run(cmd, &errOut)
	return out.String(), errOut.String(), err
}

// maxCaptured is the most vibe keeps of what an sbx command prints: what's
// read from a sandbox is the agent's to make, and could be made endless.
var maxCaptured = 256 << 20

// capped is what a command printed, up to maxCaptured: past it, the write
// fails, which ends the command's output. The buffer isn't embedded: its
// ReadFrom would let io.Copy go round Write, and the limit.
type capped struct {
	buf  bytes.Buffer
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > maxCaptured {
		c.over = true
		return 0, errTooMuch
	}
	return c.buf.Write(p)
}

func (c *capped) String() string { return c.buf.String() }

var errTooMuch = errors.New("more than vibe takes in")

// run runs cmd, whose output goes to c (and to others): an error when any
// of them was given more than maxCaptured.
func (c *capped) run(cmd *exec.Cmd, others ...*capped) error {
	err := cmd.Run()
	for _, b := range append(others, c) {
		if b.over {
			return fmt.Errorf("sbx printed %w (%d MB)", errTooMuch, maxCaptured>>20)
		}
	}
	return err
}

// saying is err with what the command said on stderr, when it said
// anything.
func saying(err error, stderr string) error {
	if msg := strings.TrimSpace(stderr); err != nil && msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// runInherit runs sbx with stdio connected to the current process, returning
// the exit code (0 on success).
func runInherit(args ...string) (int, error) {
	return runInheritUntil(nil, args...)
}

// runInheritUntil is runInherit, ending sbx early if stop is closed. A
// SIGTERM sent to vibe is passed on to sbx rather than killing vibe
// outright. Either way vibe gets to finish up (saving the agent's memories,
// say) once sbx has gone.
func runInheritUntil(stop <-chan struct{}, args ...string) (int, error) {
	cmd := exec.Command(sbxfind.Path(), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	var tty *term.State
	if stop != nil {
		tty, _ = term.GetState(int(os.Stdin.Fd()))
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	stopped := false
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	exited, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case sig := <-sigs:
				cmd.Process.Signal(sig)
			case <-stop:
				stopped = true
				terminate(cmd.Process)
				stop = nil
			case <-exited:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(exited)
	<-done
	signal.Stop(sigs)
	if stopped {
		resetTerminal(tty)
	}
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

// terminalModeResets turns off what a full-screen terminal program such as
// the agent may have turned on, and would have turned off again itself had
// it been allowed to finish: mouse tracking in all its encodings, focus
// reporting, bracketed paste, extended keyboard reporting (the kitty
// protocol, xterm's modifyOtherKeys, Windows Terminal's win32-input-mode),
// a hidden cursor and any colour left set. Left on, the shell the user goes
// back to gets every mouse movement and window switch typed into it.
//
// The alternate screen is left alone: leaving it when it isn't in use also
// restores a cursor position that was never saved, which on some terminals
// is the top left of the screen.
const terminalModeResets = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1005l\x1b[?1006l\x1b[?1015l" +
	"\x1b[?1004l\x1b[?2004l\x1b[<u\x1b[>4m\x1b[?9001l\x1b[?25h\x1b[0m"

// resetTerminal tidies up after an sbx session vibe ended itself. Killed
// (on Windows) or cut short, neither sbx nor the agent inside it gets to put
// the terminal back as it found it, so vibe does: the input mode it had
// before sbx started (tty, when known), and terminalModeResets.
func resetTerminal(tty *term.State) {
	if tty != nil {
		term.Restore(int(os.Stdin.Fd()), tty)
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		os.Stdout.WriteString(terminalModeResets + "\r\n")
	}
}

// Exists reports whether a sandbox with the given name exists, or why sbx
// couldn't say: a "no" sbx never gave would lead vibe to create a sandbox
// that's already there.
func Exists(name string) (bool, error) {
	statuses, err := List(context.Background())
	if err != nil {
		return false, err
	}
	for _, s := range statuses {
		if s.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// Reachable reports whether the sandbox will currently accept an exec.
// It gives up if ctx is cancelled.
func Reachable(ctx context.Context, name string) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err := captureOutContext(ctx, "exec", name, "--", "/bin/true")
	return err == nil
}

// probeTimeout is the backstop on one reachability probe: many times what
// even booting a stopped sandbox takes. The sandbox can make the probe
// never answer (the agent can replace /bin/true), and every caller,
// WaitReachable's count of probes too, would wait on it for good.
var probeTimeout = 2 * time.Minute

// Running says whether sandbox name is running, as `sbx ls` lists it:
// unlike Reachable, which runs something in it, asking doesn't start a
// sandbox sbx has stopped. ok is false when sbx couldn't say.
func Running(ctx context.Context, name string) (running, ok bool) {
	statuses, err := List(ctx)
	if err != nil {
		return false, false
	}
	for _, s := range statuses {
		if s.Name == name {
			return s.Running, true
		}
	}
	return false, true
}

// Status is one row of `sbx ls`.
type Status struct {
	Name    string
	Running bool
	// State is what sbx says of it, lowercased: "running", "stopped", or
	// whatever else sbx reports; "" when sbx's output didn't say.
	State string
	// Workspaces are the host folders it has mounted, as sbx lists them: a path may end in :ro, for a read-only mount.
	Workspaces []string
}

// List returns every sandbox sbx knows about and whether it is running.
// It gives up (and ends sbx) when ctx ends. It reads
// `sbx ls --json`, sbx's output for scripts, rather than its table, which
// is laid out for people and may change.
func List(ctx context.Context) ([]Status, error) {
	stdout, stderr, err := captureSplitContext(ctx, "ls", "--json")
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("sbx ls didn't answer: %w", ctx.Err())
		}
		said := strings.TrimSpace(stderr)
		if strings.Contains(said, "--json") {
			return nil, fmt.Errorf("sbx ls --json: %s\nthis sbx is too old for vibe — upgrade it with 'vibe --install-sbx'", said)
		}
		if said != "" {
			return nil, fmt.Errorf("sbx ls --json: %w: %s", err, said)
		}
		return nil, fmt.Errorf("sbx ls --json: %w", err)
	}
	return parseList(stdout)
}

// parseList reads `sbx ls --json`: an array with an object per sandbox.
// Nothing at all is taken as no sandboxes, as is null.
func parseList(out string) ([]Status, error) {
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var sandboxes []struct {
		Name       string   `json:"name"`
		Status     string   `json:"status"`
		Workspaces []string `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(out), &sandboxes); err != nil {
		return nil, fmt.Errorf("couldn't read what sbx ls --json printed (%v) — vibe may need updating for this sbx:\n%s", err, out)
	}
	statuses := make([]Status, 0, len(sandboxes))
	for _, sb := range sandboxes {
		state := strings.ToLower(sb.Status)
		statuses = append(statuses, Status{Name: sb.Name, Running: state == "running", State: state, Workspaces: sb.Workspaces})
	}
	return statuses, nil
}

// CreateOpts configures a new sandbox.
type CreateOpts struct {
	Name   string
	KitDir string
	Memory string
	// CPUs is how many CPUs the sandbox gets; 0 leaves it to sbx, which
	// gives it all of the host's.
	CPUs    int
	Publish []string // "hostPort:containerPort" entries
	Env     []string // "KEY=VALUE" entries
	Agent   string
	Target  string
	Mounts  []string
	// NoInput gives sbx nothing to read, for when nobody is there to answer
	// a question it asks: the create fails with sbx's question as the
	// reason, rather than waiting on the terminal.
	NoInput bool
	// Output is where what sbx prints goes, as plain lines, rather than the
	// terminal (nil): a companion page's log, when the terminal is a
	// session's agent's screen.
	Output io.Writer
}

// Create provisions a new sandbox (does not start it), ending sbx when ctx
// ends.
func Create(ctx context.Context, opts CreateOpts) error {
	args := []string{"create", "--name", opts.Name, "--kit", opts.KitDir}
	if opts.Memory != "" {
		args = append(args, "--memory", opts.Memory)
	}
	if opts.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(opts.CPUs))
	}
	for _, p := range opts.Publish {
		args = append(args, "--publish", p)
	}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	if opts.Output != nil {
		// Progress drawn in place is for a terminal; a log gets lines.
		args = append(args, "--no-progress")
	}
	args = append(args, opts.Agent, opts.Target)
	args = append(args, opts.Mounts...)

	cmd := cancellable(ctx, args...)
	if !opts.NoInput {
		cmd.Stdin = os.Stdin
	}
	outputTo(cmd, opts.Output)
	return cmd.Run()
}

// outputTo sends what cmd prints to out, or to the terminal when out is nil.
func outputTo(cmd *exec.Cmd, out io.Writer) {
	if out == nil {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return
	}
	cmd.Stdout, cmd.Stderr = out, out
}

// cancellable is sbx with args, ended when ctx ends. It is also kept apart
// from the terminal's Ctrl-C: the caller has taken on deciding when it ends,
// and a Ctrl-C would otherwise reach sbx before the caller could ask what
// was meant. A ctx that never ends gives a plain command.
func cancellable(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, sbxfind.Path(), args...)
	if ctx.Done() != nil {
		ignoreTerminalInterrupt(cmd)
		cmd.WaitDelay = time.Second
	}
	return cmd
}

// Run boots (or attaches to) a sandbox, blocking until the session ends —
// or until stop is closed, which ends it — and returns its exit code.
func Run(name string, stop <-chan struct{}) (int, error) {
	return runInheritUntil(stop, "run", "--name", name)
}

// Stop stops a running sandbox.
func Stop(name string) (int, error) {
	return runInherit("stop", name)
}

// StopQuiet stops a running sandbox without touching the terminal, so several
// can be stopped at once. A failure carries sbx's output.
// sbx is ended when ctx ends.
func StopQuiet(ctx context.Context, name string) error {
	out, err := captureOutContext(ctx, "stop", name)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(out); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

// Ssh connects an ssh session to the sandbox's host alias.
func Ssh(host string) (int, error) {
	cmd := exec.Command("ssh", host)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

// SshConfigured reports whether ssh has a usable ProxyCommand for host,
// mirroring vibe.sh's ssh_host_configured (via `ssh -G`).
func SshConfigured(host string) bool {
	cmd := exec.Command("ssh", "-G", host)
	out, err := cmd.Output()
	if err != nil {
		// ssh -G unavailable: let ssh speak for itself, as vibe.sh does.
		return true
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if strings.EqualFold(fields[0], "proxycommand") && !strings.EqualFold(fields[1], "none") {
			return true
		}
	}
	return false
}

// WaitReachable polls until the sandbox will accept an exec, or limit
// seconds pass. sbx create provisions but does not start; sbx run boots it.
// It gives up early when ctx ends.
func WaitReachable(ctx context.Context, name string, limit int) bool {
	for i := 0; i < limit; i++ {
		if Reachable(ctx, name) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return false
}

// Start boots a stopped sandbox without attaching a session to it, and
// reports whether it ended up reachable. It is what lets vibe ask a stopped
// sandbox a question — for its agent memories, say — before destroying it.
//
// `sbx run` is no use here: it boots the sandbox and hands the terminal to
// the agent, blocking until that session ends. An sbx without a `start`
// command leaves Start returning false rather than pretending, so callers
// can say what they could not do instead of assuming an answer.
// sbx (and the wait) is ended when ctx ends.
func Start(ctx context.Context, name string) bool {
	started, _ := StartExplained(ctx, name)
	return started
}

// StartExplained is Start, saying why when the sandbox didn't start: what sbx said of the start, which is the only
// place the reason is, or that it took the start and the sandbox never answered.
func StartExplained(ctx context.Context, name string) (started bool, why string) {
	if Reachable(ctx, name) {
		return true, ""
	}
	said, err := captureOutContext(ctx, "start", name)
	if err != nil {
		if ctx.Err() == nil && Reachable(ctx, name) {
			return true, ""
		}
		if said = strings.TrimSpace(said); said != "" {
			return false, said
		}
		return false, err.Error()
	}
	if WaitReachable(ctx, name, startTimeout) {
		return true, ""
	}
	return false, fmt.Sprintf("sbx started it, but it didn't accept a command in %d seconds", startTimeout)
}

// startTimeout is how long Start waits for a booting sandbox to accept an
// exec, in seconds — generous, since it is only ever reached when the user
// has already committed to the operation the start is for.
var startTimeout = 60

// Remove deletes a sandbox, ending sbx when ctx ends. force maps to
// `sbx rm -f`. What sbx prints goes to out, or the terminal when out is nil
// (see CreateOpts.Output).
func Remove(ctx context.Context, name string, force bool, out io.Writer) error {
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, name)
	cmd := cancellable(ctx, args...)
	outputTo(cmd, out)
	return cmd.Run()
}

// ExecCapture runs a command inside the sandbox and returns combined output.
// The exec is ended if ctx is cancelled.
func ExecCapture(ctx context.Context, name string, args ...string) (string, error) {
	full := append([]string{"exec", name, "--"}, args...)
	return captureOutContext(ctx, full...)
}

// ExecStdout runs a command inside the sandbox and returns what it wrote to
// stdout alone, for when that output is file content: anything sbx says on
// stderr stays out of it (and is in the error, if the command fails).
func ExecStdout(ctx context.Context, name string, args ...string) (string, error) {
	stdout, stderr, err := captureSplitContext(ctx, append([]string{"exec", name, "--"}, args...)...)
	return stdout, saying(err, stderr)
}

// ExecInputStdout runs a command inside the sandbox with input on its
// stdin, returning its stdout alone; ctx ending kills it.
func ExecInputStdout(ctx context.Context, name, input string, args ...string) (string, error) {
	cmd := cancellable(ctx, append([]string{"exec", "-i", name, "--"}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	stdout, stderr, err := split(cmd)
	return stdout, saying(err, stderr)
}

// ExecSilent runs a command inside the sandbox, discarding output, and
// reports only success/failure.
// The exec is ended if ctx is cancelled.
func ExecSilent(ctx context.Context, name string, args ...string) bool {
	_, err := ExecCapture(ctx, name, args...)
	return err == nil
}

// ExecInput runs a command inside the sandbox with input on its stdin, so
// content gets there with no shell in the sandbox to put it there.
func ExecInput(name, input string, args ...string) error {
	full := append([]string{"exec", "-i", name, "--"}, args...)
	cmd := exec.Command(sbxfind.Path(), full...)
	cmd.Stdin = strings.NewReader(input)
	var out capped
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := out.run(cmd); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

// maxStreamLine is the longest line ExecStream passes on; what is past it is
// dropped, since a caller wanting one that long wants it whole.
const maxStreamLine = 1 << 20

// ExecStream runs a command inside the sandbox with input on its stdin,
// calling onLine with each line it prints as it prints it, and ends the
// exec if ctx is cancelled. delivered (which may be nil) is called once the
// command has taken all of input. A failure carries what the command said on
// its stderr.
func ExecStream(ctx context.Context, name, input string, delivered func(), onLine func(line []byte), args ...string) error {
	full := append([]string{"exec", "-i", name, "--"}, args...)
	cmd := exec.CommandContext(ctx, sbxfind.Path(), full...)
	return stream(cmd, input, delivered, onLine)
}

// signalAtEOF calls done when the reader it wraps has been read to its end.
type signalAtEOF struct {
	io.Reader
	done func()
}

func (r signalAtEOF) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.done != nil {
		r.done()
	}
	return n, err
}

func stream(cmd *exec.Cmd, input string, delivered func(), onLine func(line []byte)) error {
	// A cmd with a context is one whose end the caller decides, and so is
	// kept apart from the terminal's Ctrl-C.
	if cmd.Cancel != nil {
		ignoreTerminalInterrupt(cmd)
		cmd.WaitDelay = time.Second
	}
	cmd.Stdin = strings.NewReader(input)
	if delivered != nil {
		var once sync.Once
		cmd.Stdin = signalAtEOF{strings.NewReader(input), func() { once.Do(delivered) }}
	}
	var errs tail
	cmd.Stderr = &errs
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	r := bufio.NewReaderSize(out, 64*1024)
	var line []byte
	for {
		chunk, prefix, err := r.ReadLine()
		if err != nil {
			break
		}
		if len(line)+len(chunk) <= maxStreamLine {
			line = append(line, chunk...)
		}
		if prefix {
			continue
		}
		onLine(line)
		line = line[:0]
	}
	if err := cmd.Wait(); err != nil {
		if said := strings.TrimSpace(errs.String()); said != "" {
			return fmt.Errorf("%w: %s", err, said)
		}
		return err
	}
	return nil
}

// tail keeps the last few kilobytes written to it.
type tail struct{ buf []byte }

const tailSize = 4096

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - tailSize; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }

// SetGitHubSecret gives the sandbox token as its own GitHub secret, which
// sbx's proxy adds to the sandbox's requests to GitHub — the token itself
// never enters the sandbox. It goes in on stdin, never on the command line,
// where anyone on the machine could read it. Any secret the sandbox already
// had is replaced.
func SetGitHubSecret(name, token string) error {
	// sbx only overwrites without asking when the token is given with -t,
	// so the old one goes first; there being none to remove is fine.
	RemoveGitHubSecret(name)
	cmd := exec.Command(sbxfind.Path(), "secret", "set", "github", "--sandbox", name)
	cmd.Stdin = strings.NewReader(token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sbx secret set github --sandbox %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveGitHubSecret takes the sandbox's own GitHub secret away. It leaves
// a global one alone.
func RemoveGitHubSecret(name string) error {
	if out, err := exec.Command(sbxfind.Path(), "secret", "rm", "github", "--sandbox", name, "-f").CombinedOutput(); err != nil {
		return fmt.Errorf("sbx secret rm github --sandbox %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Probe runs sbx with args and returns what it printed, stdout and stderr together, for reading one answer from it. It
// is given no input, so a question sbx asks fails rather than waits; it is ended when ctx ends.
func Probe(ctx context.Context, args ...string) (string, error) {
	return captureOutContext(ctx, args...)
}

// Login runs `sbx login` on the user's terminal, which shows an address and a code and waits until they have been used in a
// browser. An error says it didn't end well.
func Login() error {
	code, err := runInherit("login")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("sbx login ended with exit code %d", code)
	}
	return nil
}
