package sbxrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// PublishedPorts asks sbx which host ports each of a sandbox's published
// ports is actually bound to right now, as sandbox port → host ports, in
// the order sbx lists them. It is what a URL handed to the user should be
// built from: the port vibe asked for at creation is only what it asked
// for, and a mapping can be changed since with `sbx ports --publish`. A
// sandbox port can have more than one: besides the mapping asked for at
// creation, sbx adds one of its own on a fresh ephemeral port at every
// start.
func PublishedPorts(name string) (map[int][]int, error) {
	// stdout alone, so a warning on stderr can't corrupt what's parsed.
	args := []string{"ports", name, "--json"}
	out, stderr, err := captureSplitContext(context.Background(), args...)
	if err != nil {
		return nil, fmt.Errorf("sbx %s: %w", strings.Join(args, " "), saying(err, stderr))
	}
	return parsePorts([]byte(out))
}

// parsePorts reads `sbx ports --json`: an array with an object per
// mapping, each naming its sandbox_port and host_port. Nothing at all, or
// null, is nothing published; anything else it can't read is an error,
// never an empty answer, which would read as "nothing is published".
func parsePorts(out []byte) (map[int][]int, error) {
	ports := map[int][]int{}
	if len(bytes.TrimSpace(out)) == 0 {
		return ports, nil
	}
	var mappings []struct {
		SandboxPort int `json:"sandbox_port"`
		HostPort    int `json:"host_port"`
	}
	if err := json.Unmarshal(out, &mappings); err != nil {
		return nil, fmt.Errorf("couldn't read what sbx ports --json printed (%v) — vibe may need updating for this sbx:\n%s", err, bytes.TrimSpace(out))
	}
	for _, m := range mappings {
		if m.SandboxPort == 0 || m.HostPort == 0 {
			return nil, fmt.Errorf("sbx ports --json printed a mapping without a sandbox_port and host_port — vibe may need updating for this sbx:\n%s", bytes.TrimSpace(out))
		}
		addPort(ports, m.SandboxPort, m.HostPort)
	}
	return ports, nil
}

// addPort records hostPort for sandboxPort, once: sbx lists a dual-stack
// mapping once per address family, on the same port.
func addPort(ports map[int][]int, sandboxPort, hostPort int) {
	for _, p := range ports[sandboxPort] {
		if p == hostPort {
			return
		}
	}
	ports[sandboxPort] = append(ports[sandboxPort], hostPort)
}

// WriteFile writes content to path inside the sandbox, creating its
// directory. The content goes in on stdin, so no shell runs in the sandbox
// to put it there — sbx exec may join its arguments into one command line,
// which a quoted `sh -c` script doesn't survive.
func WriteFile(name, file, content string) error {
	if !ExecSilent(context.Background(), name, "mkdir", "-p", path.Dir(file)) {
		return fmt.Errorf("could not create %s in the sandbox", path.Dir(file))
	}
	if err := ExecInput(name, content, "cp", "/dev/stdin", file); err != nil {
		return fmt.Errorf("writing %s in the sandbox: %w", file, err)
	}
	return nil
}
