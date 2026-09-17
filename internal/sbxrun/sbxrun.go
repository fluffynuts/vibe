// Package sbxrun wraps the external `sbx` (Docker Sandboxes) CLI.
package sbxrun

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	cmd := exec.Command("sbx", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// runInherit runs sbx with stdio connected to the current process, returning
// the exit code (0 on success).
func runInherit(args ...string) (int, error) {
	cmd := exec.Command("sbx", args...)
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
	cmd := exec.Command("sbx", "exec", name, "--", "/bin/true")
	return cmd.Run() == nil
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

// Run boots (or attaches to) a sandbox, blocking until the session ends, and
// returns its exit code.
func Run(name string) (int, error) {
	return runInherit("run", "--name", name)
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
	full := append([]string{"exec", name, "--"}, args...)
	return captureOut(full...)
}

// ExecSilent runs a command inside the sandbox, discarding output, and
// reports only success/failure.
func ExecSilent(name string, args ...string) bool {
	_, err := ExecCapture(name, args...)
	return err == nil
}

// ExecDetached dispatches a command inside the sandbox in the background
// (`sbx exec -d`).
func ExecDetached(name string, args ...string) error {
	full := append([]string{"exec", "-d", name, "--"}, args...)
	cmd := exec.Command("sbx", full...)
	return cmd.Run()
}
