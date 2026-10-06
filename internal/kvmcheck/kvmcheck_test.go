package kvmcheck

import (
	"strings"
	"testing"
)

func TestParseCPUInfo(t *testing.T) {
	for _, tt := range []struct {
		info string
		virt bool
		cpu  string
	}{
		{"processor\t: 0\nflags\t\t: fpu vme de pse vmx sse\n", true, "intel"},
		{"processor\t: 0\nflags\t\t: fpu svm lm\n", true, "amd"},
		{"processor\t: 0\nflags\t\t: fpu vme hypervisor\n", false, ""},
		{"", false, ""},
	} {
		if virt, cpu := ParseCPUInfo(tt.info); virt != tt.virt || cpu != tt.cpu {
			t.Errorf("ParseCPUInfo(%q) = %v, %q, want %v, %q", tt.info, virt, cpu, tt.virt, tt.cpu)
		}
	}
}

func TestAdvice(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state State
		want  string // in the advice; "" for none
	}{
		{"all's well", State{Exists: true, Accessible: true}, ""},
		{"no modules, intel", State{CPUVirt: true, CPU: "intel"}, "sudo modprobe kvm_intel"},
		{"no modules, amd", State{CPUVirt: true, CPU: "amd"}, "sudo modprobe kvm_amd"},
		{"no virtualization", State{}, "BIOS/UEFI"},
		{"not in the group", State{Exists: true, Group: "kvm", GroupRW: true, User: "me"}, "sudo usermod -aG kvm me"},
		{"added, not yet logged in again", State{Exists: true, Group: "kvm", GroupRW: true, InGroup: true, User: "me"}, "log out and back in"},
		{"group can't write it", State{Exists: true, Group: "root", User: "me"}, "udev rule"},
	} {
		got := strings.Join(Advice(tt.state), "\n")
		if tt.want == "" && got != "" || !strings.Contains(got, tt.want) {
			t.Errorf("%s: Advice = %q, want it to mention %q", tt.name, got, tt.want)
		}
	}
}
