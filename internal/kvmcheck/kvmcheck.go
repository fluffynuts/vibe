// Package kvmcheck checks, for vibe --install on Linux, that sbx will be
// able to start its VMs: that /dev/kvm exists, and that the user can read
// and write it.
package kvmcheck

import (
	"fmt"
	"strings"
)

// Device is KVM's device node.
const Device = "/dev/kvm"

// State is what Probe found out about Device and the user.
type State struct {
	Exists bool
	// Accessible is whether this process can read and write Device.
	Accessible bool
	// CPUVirt is whether the CPU advertises hardware virtualization (vmx
	// or svm in /proc/cpuinfo); CPU names which ("intel", "amd"), if so.
	CPUVirt bool
	CPU     string
	// Group is Device's group, by name (or GID, when it has no name), and
	// GroupRW whether that group may read and write it.
	Group   string
	GroupRW bool
	// InGroup is whether the user is listed as a member of Group;
	// InSession whether this login session already has it.
	InGroup   bool
	InSession bool
	User      string
}

// Advice is what to tell the user about s, a line per entry; none when
// all's well.
func Advice(s State) []string {
	if !s.Exists {
		if !s.CPUVirt {
			return []string{
				"WARNING: " + Device + " doesn't exist, and this CPU doesn't advertise hardware virtualization (vmx/svm),",
				"WARNING: so sbx can't start its VMs. Enable virtualization (VT-x / AMD-V / SVM) in your BIOS/UEFI,",
				"WARNING: or, inside a VM, enable nested virtualization on its host.",
			}
		}
		module := "kvm_intel"
		if s.CPU == "amd" {
			module = "kvm_amd"
		}
		return []string{
			"WARNING: " + Device + " doesn't exist — the kvm kernel modules aren't loaded, so sbx can't start its VMs.",
			"WARNING: load them with: sudo modprobe " + module,
			fmt.Sprintf("WARNING: and to load them at every boot: echo %s | sudo tee /etc/modules-load.d/kvm.conf", module),
		}
	}
	if s.Accessible {
		return nil
	}
	lines := []string{"WARNING: you can't read and write " + Device + ", so sbx can't start its VMs."}
	switch {
	case s.Group == "" || !s.GroupRW:
		lines = append(lines,
			"WARNING: its permissions don't give its group access either; ask your distribution's docs how KVM",
			"WARNING: access is granted (usually a udev rule making it root:kvm, mode 0660).")
	case s.InGroup && !s.InSession:
		lines = append(lines,
			"WARNING: you're in its group, '"+s.Group+"', but this login session started before you were added:",
			"WARNING: log out and back in (or restart) to pick it up.")
	default:
		user := s.User
		if user == "" {
			user = "$USER"
		}
		lines = append(lines,
			"WARNING: add yourself to its group, '"+s.Group+"', then log out and back in:",
			"WARNING:   sudo usermod -aG "+s.Group+" "+user)
	}
	return lines
}

// ParseCPUInfo reads /proc/cpuinfo's flags for hardware virtualization:
// vmx (Intel) or svm (AMD).
func ParseCPUInfo(cpuinfo string) (virt bool, cpu string) {
	for _, line := range strings.Split(cpuinfo, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "flags" {
			continue
		}
		for _, flag := range strings.Fields(value) {
			switch flag {
			case "vmx":
				return true, "intel"
			case "svm":
				return true, "amd"
			}
		}
	}
	return false, ""
}

// contains reports whether list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
