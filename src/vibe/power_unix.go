//go:build !windows

package main

import "syscall"

// processAlive says whether a process with pid is running (a zombie not yet
// reaped counts: its parent has gone, so init reaps it).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
