package sbxrun

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"vibe/internal/sbxfind"
)

// Process is an sbx command with a person on the other end, as a sign-in is: what it prints comes line by line, stdout
// and stderr together, and what the person types goes to its stdin. Lines is closed when it has printed all it will;
// Done then says how it ended, once.
type Process struct {
	lines  chan string
	done   chan error
	stdin  io.WriteCloser
	cancel context.CancelFunc
}

// maxProcessLine is the longest line a Process passes on whole; a longer one is cut.
const maxProcessLine = 16 << 10

// StartLogin runs `sbx login`, which prints an address and a code and waits for them to be used in a browser. It is given
// no input.
func StartLogin() (*Process, error) {
	return start(false, "login")
}

// StartExec runs a command inside the sandbox with its stdin open for Send.
func StartExec(name string, args ...string) (*Process, error) {
	return start(true, append([]string{"exec", "-i", name, "--"}, args...)...)
}

func start(withInput bool, args ...string) (*Process, error) {
	ctx, cancel := context.WithCancel(context.Background())
	// A command whose end the caller decides is kept apart from the terminal's Ctrl-C, and cancelling it ends everything
	// it started.
	cmd := exec.CommandContext(ctx, sbxfind.Path(), args...)
	ignoreTerminalInterrupt(cmd)
	// Once it is stopped, a descendant that has left its process group (a daemon it started) must not hold what is read from it open:
	// that would leave Done unsent, and a sign-in "running" for good. After a second the pipes are closed, and it is ended.
	cmd.WaitDelay = time.Second
	p := &Process{lines: make(chan string), done: make(chan error, 1), cancel: cancel}
	if withInput {
		in, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			return nil, err
		}
		p.stdin = in
	}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		defer close(p.lines)
		r := bufio.NewReaderSize(pr, 4096)
		var line []byte
		for {
			chunk, prefix, err := r.ReadLine()
			if err != nil {
				return
			}
			if len(line)+len(chunk) <= maxProcessLine {
				line = append(line, chunk...)
			}
			if prefix {
				continue
			}
			select {
			case p.lines <- string(line):
			case <-ctx.Done():
				return
			}
			line = line[:0]
		}
	}()
	go func() {
		err := cmd.Wait()
		// It exited well, but something it started held its output past WaitDelay: that is not the command failing.
		if errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
		pw.Close()
		<-scanned
		cancel()
		p.done <- err
	}()
	return p, nil
}

// Lines is what the process prints, a line at a time; it must be read until it is closed.
func (p *Process) Lines() <-chan string { return p.lines }

// Done says how the process ended (nil when it succeeded), after Lines is closed.
func (p *Process) Done() <-chan error { return p.done }

// Send gives the process a line on its stdin. A process that takes none says so.
func (p *Process) Send(line string) error {
	if p.stdin == nil {
		return errors.New("this process takes no input")
	}
	_, err := io.WriteString(p.stdin, line+"\n")
	return err
}

// Stop ends the process, and anything it started.
func (p *Process) Stop() { p.cancel() }
