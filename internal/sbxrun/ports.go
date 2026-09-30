package sbxrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// PublishedPorts asks sbx which host port each of a sandbox's published
// ports is actually bound to right now, as sandbox port → host port. It is
// what a URL handed to the user should be built from: the port vibe asked
// for at creation is only what it asked for, and a mapping can be changed
// since with `sbx ports --publish`.
func PublishedPorts(name string) (map[int]int, error) {
	out, err := captureStdout("ports", name, "--json")
	if err != nil {
		return nil, err
	}
	return parsePorts(out)
}

// captureStdout runs sbx and returns stdout alone, so a warning on stderr
// can't corrupt output that is meant to be parsed.
func captureStdout(args ...string) ([]byte, error) {
	cmd := exec.Command("sbx", args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sbx %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

// parsePorts reads `sbx ports --json` output. sbx doesn't document the shape
// of that JSON, so rather than bind to one guess this looks, anywhere in the
// document, for an object naming both a sandbox-side and a host-side port —
// {"sandboxPort": 5391, "hostPort": 5396}, in whatever casing or spelling
// sbx uses — and for Docker's own {"5391/tcp": [{"HostPort": "5396"}]}. A
// document where neither turns up is an error, never an empty answer: an
// empty answer would read as "nothing is published".
func parsePorts(out []byte) (map[int]int, error) {
	var doc interface{}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("reading sbx ports output: %w", err)
	}
	ports := map[int]int{}
	walkPorts(doc, ports)
	if len(ports) == 0 && !emptyDoc(doc) {
		return nil, fmt.Errorf("found no port mappings in sbx ports output: %s", strings.TrimSpace(string(out)))
	}
	return ports, nil
}

func emptyDoc(doc interface{}) bool {
	switch v := doc.(type) {
	case nil:
		return true
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		for _, child := range v {
			if !emptyDoc(child) {
				return false
			}
		}
		return true
	}
	return false
}

var (
	sandboxPortKey = regexp.MustCompile(`^((sandbox|container|target|private|internal|guest)_?)?port$`)
	hostPortKey    = regexp.MustCompile(`^(host|published|public|local)_?port$`)
	portSpecKey    = regexp.MustCompile(`^(\d+)(/[a-z0-9]+)?$`)
)

func walkPorts(node interface{}, ports map[int]int) {
	switch v := node.(type) {
	case []interface{}:
		for _, child := range v {
			walkPorts(child, ports)
		}
	case map[string]interface{}:
		sandbox, host := 0, 0
		for key, val := range v {
			k := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			switch {
			case sandboxPortKey.MatchString(k):
				sandbox = portNumber(val)
			case hostPortKey.MatchString(k):
				host = portNumber(val)
			}
		}
		if sandbox != 0 && host != 0 {
			ports[sandbox] = host
		}
		for key, val := range v {
			// Docker's shape: the sandbox port is the key itself.
			if m := portSpecKey.FindStringSubmatch(key); m != nil {
				sandboxPort, _ := strconv.Atoi(m[1])
				if hostPort := firstHostPort(val); hostPort != 0 {
					ports[sandboxPort] = hostPort
					continue
				}
			}
			walkPorts(val, ports)
		}
	}
}

// firstHostPort finds a host port in the value under a Docker-style
// "5391/tcp" key: a list of bindings, or a single one.
func firstHostPort(val interface{}) int {
	switch v := val.(type) {
	case []interface{}:
		for _, child := range v {
			if p := firstHostPort(child); p != 0 {
				return p
			}
		}
	case map[string]interface{}:
		for key, child := range v {
			if hostPortKey.MatchString(strings.ToLower(key)) {
				return portNumber(child)
			}
		}
	default:
		return portNumber(v)
	}
	return 0
}

// portNumber reads a port given as a number, "5396", or "5391/tcp".
func portNumber(val interface{}) int {
	switch v := val.(type) {
	case float64:
		if v > 0 && v < 65536 && v == float64(int(v)) {
			return int(v)
		}
	case string:
		if m := portSpecKey.FindStringSubmatch(strings.ToLower(strings.TrimSpace(v))); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > 0 && n < 65536 {
				return n
			}
		}
	}
	return 0
}

// WriteFile writes content to path inside the sandbox, creating its
// directory. The content goes in on stdin, so no shell runs in the sandbox
// to put it there — sbx exec may join its arguments into one command line,
// which a quoted `sh -c` script doesn't survive.
func WriteFile(name, file, content string) error {
	if !ExecSilent(name, "mkdir", "-p", path.Dir(file)) {
		return fmt.Errorf("could not create %s in the sandbox", path.Dir(file))
	}
	cmd := exec.Command("sbx", "exec", "-i", name, "--", "cp", "/dev/stdin", file)
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("writing %s in the sandbox: %w: %s", file, err, strings.TrimSpace(string(out)))
	}
	return nil
}
