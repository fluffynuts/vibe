package sbxinstall

import (
	"strings"

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
