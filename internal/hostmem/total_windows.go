package hostmem

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx is Windows' MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func total() (uint64, error) {
	s := memoryStatusEx{}
	s.Length = uint32(unsafe.Sizeof(s))
	if r, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return 0, err
	}
	return s.TotalPhys, nil
}
