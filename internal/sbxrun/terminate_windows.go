//go:build windows

package sbxrun

import "os"

// terminate ends p. Windows has no signal to ask a console program to end
// that can be sent to just one process, so it is killed; the sandbox itself
// is untouched, as it is when sbx is ended any other way.
func terminate(p *os.Process) {
	p.Kill()
}
