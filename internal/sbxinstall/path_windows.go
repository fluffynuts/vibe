package sbxinstall

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// PersistentPath is the PATH a terminal opened now would start with: the
// machine's and the user's, as the registry holds them. An installer that
// adds to PATH changes those, which a process already running — this one,
// and the terminal it was started from — never sees.
func PersistentPath() string {
	var parts []string
	for _, k := range []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
		{registry.CURRENT_USER, `Environment`},
	} {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue("Path")
		key.Close()
		if err != nil {
			continue
		}
		if expanded, err := registry.ExpandString(v); err == nil {
			v = expanded
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, ";")
}

// AddToUserPath adds dir to the end of the user's PATH in the registry —
// what Windows' own "Edit environment variables for your account" does —
// unless it's already there, and tells running programs the environment
// has changed, so terminals opened from here on (by Explorer, the Start
// menu or Windows Terminal) start with it. This process, and the terminal
// it was started from, keep the PATH they have. added is false when dir
// was there already.
func AddToUserPath(dir string) (added bool, err error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	current, kind, err := key.GetStringValue("Path")
	switch {
	case err == registry.ErrNotExist:
		current, kind = "", registry.EXPAND_SZ
	case err != nil:
		return false, err
	}
	if hasEntry(current, dir) {
		return false, nil
	}
	updated := dir
	if trimmed := strings.TrimRight(current, ";"); trimmed != "" {
		updated = trimmed + ";" + dir
	}
	// Kept as the type it was: a REG_EXPAND_SZ path holds %VARIABLES% that
	// rewriting it as REG_SZ would stop expanding.
	if kind == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", updated)
	} else {
		err = key.SetStringValue("Path", updated)
	}
	if err != nil {
		return false, err
	}
	broadcastEnvironmentChange()
	return true, nil
}

// broadcastEnvironmentChange sends WM_SETTINGCHANGE for "Environment" to
// every top-level window, which is how Explorer learns to give programs it
// starts the new PATH. It waits at most a second on any window that is slow
// to answer, and failing is no reason to fail the install: signing out and
// back in has the same effect.
func broadcastEnvironmentChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	sendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung, 1000, uintptr(unsafe.Pointer(&result)))
}

var sendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// hasEntry reports whether the registry PATH value list holds dir, however
// it's spelled: in any case, with a trailing backslash, or by way of a
// %VARIABLE% such as %USERPROFILE%.
func hasEntry(list, dir string) bool {
	want := strings.TrimRight(dir, `\`)
	for _, entry := range strings.Split(list, ";") {
		if expanded, err := registry.ExpandString(entry); err == nil {
			entry = expanded
		}
		if strings.EqualFold(strings.TrimRight(entry, `\`), want) {
			return true
		}
	}
	return false
}
