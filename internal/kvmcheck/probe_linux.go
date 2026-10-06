package kvmcheck

import (
	"os"
	"os/user"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// Probe looks at Device, the CPU, and the user's groups.
func Probe() State {
	var s State
	if cpuinfo, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		s.CPUVirt, s.CPU = ParseCPUInfo(string(cpuinfo))
	}
	info, err := os.Stat(Device)
	if err != nil {
		return s
	}
	s.Exists = true
	// access(2), not the mode bits, so ACLs — systemd's uaccess grants
	// the logged-in user one — count too.
	s.Accessible = unix.Access(Device, unix.R_OK|unix.W_OK) == nil
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return s
	}
	gid := strconv.FormatUint(uint64(st.Gid), 10)
	s.Group = gid
	if g, err := user.LookupGroupId(gid); err == nil {
		s.Group = g.Name
	}
	s.GroupRW = info.Mode().Perm()&0o060 == 0o060
	if u, err := user.Current(); err == nil {
		s.User = u.Username
		if ids, err := u.GroupIds(); err == nil {
			s.InGroup = contains(ids, gid)
		}
	}
	if groups, err := os.Getgroups(); err == nil {
		for _, g := range groups {
			if strconv.Itoa(g) == gid {
				s.InSession = true
			}
		}
	}
	if os.Getegid() == int(st.Gid) {
		s.InSession = true
	}
	return s
}
