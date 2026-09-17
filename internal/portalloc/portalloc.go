// Package portalloc finds and remembers the host port published for a
// sandbox, so its URL stays stable across restarts.
package portalloc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Dir returns the ports directory under the given vibe home.
func Dir(vibeHome string) string {
	return filepath.Join(vibeHome, "ports")
}

// InUse reports whether something is already listening on the given port.
func InUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

// FindFree walks upward from start until it finds a port nothing is
// listening on, giving up above limit.
func FindFree(start, limit int) (int, error) {
	port := start
	for InUse(port) {
		port++
		if port >= limit {
			return 0, fmt.Errorf("no free port found in %d-%d", start, limit)
		}
	}
	return port, nil
}

// Remember persists the chosen host port for a sandbox name.
func Remember(vibeHome, name string, port int) error {
	if err := os.MkdirAll(Dir(vibeHome), 0o755); err != nil {
		return fmt.Errorf("creating ports dir: %w", err)
	}
	path := filepath.Join(Dir(vibeHome), name)
	if err := os.WriteFile(path, []byte(strconv.Itoa(port)+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing port record: %w", err)
	}
	return nil
}

// Recall returns the previously remembered port for a sandbox, or fallback
// when none is recorded.
func Recall(vibeHome, name string, fallback int) int {
	path := filepath.Join(Dir(vibeHome), name)
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fallback
	}
	return port
}
