// Package sbxrun wraps the external `sbx` (Docker Sandboxes) CLI.
package sbxrun

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Available reports whether sbx is on PATH and responds.
func Available() bool {
	cmd := exec.Command("sbx", "version")
	cmd.Stdin = nil
	return cmd.Run() == nil
}

// captureOut runs sbx with the given args and returns combined stdout.
func captureOut(args ...string) (string, error) {
	return captureOutContext(context.Background(), args...)
}

// captureOutContext is captureOut, killing sbx if ctx is cancelled. An sbx
// run with a cancellable ctx is also kept apart from the terminal's Ctrl-C:
// the caller has taken on deciding when it ends, and a Ctrl-C would
// otherwise reach sbx before the caller could ask what was meant.
func captureOutContext(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "sbx", args...)
	if ctx.Done() != nil {
		ignoreTerminalInterrupt(cmd)
		cmd.WaitDelay = time.Second
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
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
	cmd := exec.Command("sbx", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	exited := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigs:
				cmd.Process.Signal(sig)
			case <-stop:
				terminate(cmd.Process)
				stop = nil
			case <-exited:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(exited)
	signal.Stop(sigs)
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

// Exists reports whether a sandbox with the given name exists.
func Exists(name string) bool {
	out, err := captureOut("ls", "--quiet")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// Reachable reports whether the sandbox will currently accept an exec.
func Reachable(name string) bool {
	return ReachableContext(context.Background(), name)
}

// ReachableContext is Reachable, giving up if ctx is cancelled.
func ReachableContext(ctx context.Context, name string) bool {
	_, err := captureOutContext(ctx, "exec", name, "--", "/bin/true")
	return err == nil
}

// Status is one row of `sbx ls`.
type Status struct {
	Name    string
	Running bool
}

// List returns every sandbox sbx knows about and whether it is running.
//
// This depends on the exact output format of `sbx ls`; if that format
// changes upstream this parser may need adjusting.
func List() ([]Status, error) {
	out, err := captureOut("ls")
	if err != nil {
		return nil, fmt.Errorf("sbx ls: %w", err)
	}
	var statuses []Status
	scanner := bufio.NewScanner(strings.NewReader(out))
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if first && (strings.HasPrefix(lower, "name") || strings.Contains(lower, "status")) {
			first = false
			continue
		}
		first = false
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		running := strings.Contains(lower, "running") && !strings.Contains(lower, "not running")
		statuses = append(statuses, Status{Name: name, Running: running})
	}
	return statuses, nil
}

// CreateOpts configures a new sandbox.
type CreateOpts struct {
	Name    string
	KitDir  string
	Memory  string
	Publish []string // "hostPort:containerPort" entries
	Env     []string // "KEY=VALUE" entries
	Agent   string
	Target  string
	Mounts  []string
}

// Create provisions a new sandbox (does not start it).
func Create(opts CreateOpts) error {
	args := []string{"create", "--name", opts.Name, "--kit", opts.KitDir}
	if opts.Memory != "" {
		args = append(args, "--memory", opts.Memory)
	}
	for _, p := range opts.Publish {
		args = append(args, "--publish", p)
	}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	args = append(args, opts.Agent, opts.Target)
	args = append(args, opts.Mounts...)

	cmd := exec.Command("sbx", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
func WaitReachable(name string, limit int) bool {
	for i := 0; i < limit; i++ {
		if Reachable(name) {
			return true
		}
		time.Sleep(time.Second)
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
func Start(name string) bool {
	if Reachable(name) {
		return true
	}
	cmd := exec.Command("sbx", "start", name)
	if err := cmd.Run(); err != nil {
		return Reachable(name)
	}
	return WaitReachable(name, startTimeout)
}

// startTimeout is how long Start waits for a booting sandbox to accept an
// exec, in seconds — generous, since it is only ever reached when the user
// has already committed to the operation the start is for.
const startTimeout = 60

// Remove deletes a sandbox. force maps to `sbx rm -f`.
func Remove(name string, force bool) error {
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, name)
	cmd := exec.Command("sbx", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExecCapture runs a command inside the sandbox and returns combined output.
func ExecCapture(name string, args ...string) (string, error) {
	return ExecCaptureContext(context.Background(), name, args...)
}

// ExecCaptureContext is ExecCapture, ending the exec if ctx is cancelled.
func ExecCaptureContext(ctx context.Context, name string, args ...string) (string, error) {
	full := append([]string{"exec", name, "--"}, args...)
	return captureOutContext(ctx, full...)
}

// ExecSilent runs a command inside the sandbox, discarding output, and
// reports only success/failure.
func ExecSilent(name string, args ...string) bool {
	return ExecSilentContext(context.Background(), name, args...)
}

// ExecSilentContext is ExecSilent, ending the exec if ctx is cancelled.
func ExecSilentContext(ctx context.Context, name string, args ...string) bool {
	_, err := ExecCaptureContext(ctx, name, args...)
	return err == nil
}

// ExecDetached dispatches a command inside the sandbox in the background
// (`sbx exec -d`).
func ExecDetached(name string, args ...string) error {
	full := append([]string{"exec", "-d", name, "--"}, args...)
	cmd := exec.Command("sbx", full...)
	return cmd.Run()
}
