package nugetfetch

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ProjectFiles are the text of a folder's project files, at the default
// branch and as they are now, by path.
type ProjectFiles struct {
	Committed map[string]string
	Working   map[string]string
}

var (
	// An element naming a package: self-closed, or holding its version.
	packageElement = regexp.MustCompile(`(?is)<(?:PackageReference|PackageVersion)\b([^>]*?)(?:/>|>(.*?)</(?:PackageReference|PackageVersion)>)`)
	attribute      = regexp.MustCompile(`(?is)\b([A-Za-z]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	childVersion   = regexp.MustCompile(`(?is)<Version>\s*([^<]*?)\s*</Version>`)
)

// packageVersions are the versions the project files give each package (ids
// lower-cased), as plain text: a version from a property is as it's
// written, "$(Name)".
func packageVersions(files map[string]string) map[string][]string {
	found := map[string][]string{}
	for _, text := range files {
		for _, m := range packageElement.FindAllStringSubmatch(text, -1) {
			id, version := "", ""
			for _, a := range attribute.FindAllStringSubmatch(m[1], -1) {
				value := a[2] + a[3]
				switch strings.ToLower(a[1]) {
				case "include", "update":
					id = strings.ToLower(value)
				case "version", "versionoverride":
					version = value
				}
			}
			if version == "" {
				if child := childVersion.FindStringSubmatch(m[2]); child != nil {
					version = child[1]
				}
			}
			if id != "" && version != "" && !contains(found[id], version) {
				found[id] = append(found[id], version)
			}
		}
	}
	return found
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// compareVersions orders two NuGet versions: by their numeric parts, a
// prerelease before its release, prerelease labels part by part (beta.2
// before beta.10). ok is false when either isn't a plain version.
func compareVersions(a, b string) (cmp int, ok bool) {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < 4; i++ {
		if pa.numbers[i] != pb.numbers[i] {
			return before(pa.numbers[i] < pb.numbers[i]), true
		}
	}
	switch {
	case pa.pre == nil && pb.pre == nil:
		return 0, true
	case pa.pre == nil:
		return 1, true
	case pb.pre == nil:
		return -1, true
	}
	for i := 0; i < len(pa.pre) && i < len(pb.pre); i++ {
		x, y := pa.pre[i], pb.pre[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil && xn != yn:
			return before(xn < yn), true
		case xerr == nil && yerr != nil:
			return -1, true
		case xerr != nil && yerr == nil:
			return 1, true
		case xerr != nil && yerr != nil && strings.ToLower(x) != strings.ToLower(y):
			return before(strings.ToLower(x) < strings.ToLower(y)), true
		}
	}
	switch {
	case len(pa.pre) < len(pb.pre):
		return -1, true
	case len(pa.pre) > len(pb.pre):
		return 1, true
	}
	return 0, true
}

// before is -1 for a pair that's in order, else 1.
func before(inOrder bool) int {
	if inOrder {
		return -1
	}
	return 1
}

type version struct {
	numbers [4]int
	pre     []string
}

func parseVersion(s string) (version, bool) {
	var v version
	s, _, _ = strings.Cut(s, "+")
	s, pre, hasPre := strings.Cut(s, "-")
	if hasPre {
		v.pre = strings.Split(pre, ".")
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v.numbers[i] = n
	}
	return v, true
}

// Classify says what a request is, from the project's files and never the
// agent's word: missing (the default branch already uses that version, so
// it's only not in the host's cache), an upgrade or downgrade from what the
// default branch uses, or new to the project. It starts with the kind.
func Classify(r Request, files ProjectFiles, defaultBranch string) string {
	id := strings.ToLower(r.ID)
	if defaultBranch == "" {
		return "can't tell whether this is new: the repository's default branch isn't known (origin's HEAD, or the one set for it)"
	}
	have := packageVersions(files.Committed)[id]
	if len(have) == 0 {
		if len(packageVersions(files.Working)[id]) > 0 {
			return fmt.Sprintf("new — %s doesn't use it, and this branch adds it", defaultBranch)
		}
		return fmt.Sprintf("new — %s doesn't use it, and no project file here names it yet", defaultBranch)
	}
	if contains(have, r.Version) {
		return fmt.Sprintf("missing — %s already uses %s %s; it's only not in your package cache", defaultBranch, r.ID, r.Version)
	}
	higher, lower := 0, 0
	for _, h := range have {
		cmp, ok := compareVersions(r.Version, h)
		if !ok {
			return fmt.Sprintf("changed — %s has %s from %s (a version set by a property, or not plain): can't say if this is an upgrade", defaultBranch, r.ID, strings.Join(have, ", "))
		}
		if cmp > 0 {
			higher++
		} else if cmp < 0 {
			lower++
		}
	}
	switch {
	case higher > 0 && lower == 0:
		return fmt.Sprintf("upgrade — %s has %s %s; this asks for %s", defaultBranch, r.ID, strings.Join(have, ", "), r.Version)
	case lower > 0 && higher == 0:
		return fmt.Sprintf("downgrade — %s has %s %s; this asks for %s", defaultBranch, r.ID, strings.Join(have, ", "), r.Version)
	}
	return fmt.Sprintf("changed — %s has %s %s; this asks for %s", defaultBranch, r.ID, strings.Join(have, ", "), r.Version)
}
