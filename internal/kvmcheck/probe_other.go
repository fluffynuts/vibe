//go:build !linux

package kvmcheck

// Probe is Linux-only: elsewhere sbx doesn't use KVM. It reports all's well.
func Probe() State {
	return State{Exists: true, Accessible: true}
}
