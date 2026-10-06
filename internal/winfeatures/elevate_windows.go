package winfeatures

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Elevated reports whether this process already has admin rights, so can
// run dism itself.
func Elevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// shellExecuteInfo is Windows' SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	size       uint32
	mask       uint32
	hwnd       windows.Handle
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	show       int32
	instApp    windows.Handle
	idList     uintptr
	class      *uint16
	keyClass   windows.Handle
	hotKey     uint32
	icon       windows.Handle
	process    windows.Handle
}

const seeMaskNoCloseProcess = 0x00000040

var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// RunElevated runs file with args as an administrator — Windows asks the
// user first — in a window of its own, waits for it to finish, and returns
// its exit code. Refusing admin rights is exit code 1223
// (ERROR_CANCELLED), as Outcome reads it.
func RunElevated(file string, args []string) (int, error) {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = windows.EscapeArg(a)
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	exe, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return 0, err
	}
	params, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		mask:       seeMaskNoCloseProcess,
		verb:       verb,
		file:       exe,
		parameters: params,
		show:       windows.SW_SHOWNORMAL,
	}
	info.size = uint32(unsafe.Sizeof(info))
	if r, _, err := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return exitCancelled, nil
		}
		return 0, err
	}
	if info.process == 0 {
		return 0, errors.New("windows started " + file + " without a process to wait on")
	}
	defer windows.CloseHandle(info.process)
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.process, &code); err != nil {
		return 0, err
	}
	return int(code), nil
}
