package timezone

import "golang.org/x/sys/windows/registry"

// detect reads Windows' name for its timezone ("South Africa Standard
// Time", as `tzutil /g` prints it) from the registry, and gives the zone
// CLDR maps it to.
func detect() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\TimeZoneInformation`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	id, _, err := k.GetStringValue("TimeZoneKeyName")
	if err != nil {
		return ""
	}
	return fromWindows(id)
}
