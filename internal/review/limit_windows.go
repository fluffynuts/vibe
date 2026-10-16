//go:build windows

package review

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func limitOwnMemory(int64) {}

// limitChildMemory puts the helper in a job holding its memory to limit
// four times over (watchMemory holds its heap to limit): Windows ends it
// when it asks for more. Released, the job is closed.
func limitChildMemory(p *os.Process, limit int64) func() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() {}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY | windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
		ProcessMemoryLimit: uintptr(4 * limit),
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	if h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid)); err == nil {
		windows.AssignProcessToJobObject(job, h)
		windows.CloseHandle(h)
	}
	return func() { windows.CloseHandle(job) }
}
