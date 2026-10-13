package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// sbxFailureHint is shown when sbx exits non-zero, which is most often a
// sandbox that failed to start.
const sbxFailureHint = "sbx will fail to start if other virtualisation (eg virtualbox) is running - check that you have no such process running"

// virtualBoxProcesses are the names of processes that mean a VirtualBox VM
// is running, and so holding the CPU virtualisation sbx needs.
var virtualBoxProcesses = []string{"VBoxHeadless", "VirtualBoxVM"}

const virtualBoxHint = "You should stop VirtualBox - sbx and virtualbox do not play well together without cpu pinning"

func reportPortHolder(port int) {
	note("  processes holding %d:", port)
	for _, out := range portHolders(runtime.GOOS, port) {
		fmt.Fprintln(os.Stderr, out)
	}
}

// portHolders asks whatever this OS offers which processes listen on port.
// Each tool that is present and answers contributes its output; one that
// isn't is skipped quietly, since this is only ever a diagnostic aside.
func portHolders(goos string, port int) []string {
	var outputs []string
	try := func(name string, args ...string) {
		if _, err := exec.LookPath(name); err != nil {
			return
		}
		if out, err := exec.Command(name, args...).CombinedOutput(); err == nil {
			outputs = append(outputs, string(out))
		}
	}
	switch goos {
	case "windows":
		if _, err := exec.LookPath("netstat"); err == nil {
			if out, err := exec.Command("netstat", "-ano", "-p", "TCP").CombinedOutput(); err == nil {
				if lines := netstatListeners(string(out), port); lines != "" {
					outputs = append(outputs, "  Proto  Local Address  Foreign Address  State  PID\n"+lines)
				}
			}
		}
	case "linux":
		try("ss", "-tlnp", fmt.Sprintf("sport = :%d", port))
		try("fuser", "-v", fmt.Sprintf("%d/tcp", port))
	default: // macOS and the BSDs
		try("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN")
	}
	return outputs
}

// runningProcesses returns which of names are running as processes, asking
// whatever this OS offers. Like portHolders it is only a diagnostic aside, so
// a process list it cannot get just finds nothing.
func runningProcesses(goos string, names []string) []string {
	var listed []string
	switch goos {
	case "windows":
		out, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return nil
		}
		listed = tasklistImages(string(out))
	case "linux":
		// /proc needs no tool; comm is the name truncated to 15 bytes,
		// which is long enough for the names asked about here.
		comms, _ := filepath.Glob("/proc/[0-9]*/comm")
		for _, comm := range comms {
			if b, err := os.ReadFile(comm); err == nil {
				listed = append(listed, strings.TrimSpace(string(b)))
			}
		}
	default: // macOS and the BSDs
		out, err := exec.Command("ps", "-A", "-o", "comm=").Output()
		if err != nil {
			return nil
		}
		listed = strings.Split(string(out), "\n")
	}
	return matchProcesses(listed, names)
}

// tasklistImages picks the image names out of `tasklist /FO CSV /NH`
// output, where each line starts with the quoted image name.
func tasklistImages(out string) []string {
	var images []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		if end := strings.Index(line[1:], `"`); end >= 0 {
			images = append(images, line[1:end+1])
		}
	}
	return images
}

// matchProcesses returns which of names appear in listed, a list of process
// names or paths (macOS's ps gives the full executable path). Names match
// case-insensitively and ignore a Windows .exe suffix.
func matchProcesses(listed, names []string) []string {
	seen := map[string]bool{}
	for _, p := range listed {
		p = strings.TrimSpace(p)
		if i := strings.LastIndexAny(p, `/\`); i >= 0 {
			p = p[i+1:]
		}
		p = strings.TrimSuffix(strings.ToLower(p), ".exe")
		seen[p] = true
	}
	var found []string
	for _, n := range names {
		if seen[strings.ToLower(n)] {
			found = append(found, n)
		}
	}
	return found
}

// netstatListeners picks, out of `netstat -ano` output, the lines for a
// socket listening on port — its PID is the last field.
func netstatListeners(out string, port int) string {
	suffix := fmt.Sprintf(":%d", port)
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if strings.HasSuffix(fields[1], suffix) {
			b.WriteString(strings.TrimRight(line, "\r") + "\n")
		}
	}
	return b.String()
}
