package main

import (
	"fmt"
	"slices"
	"strings"

	"vibe/internal/layout"
	"vibe/internal/library"
	"vibe/internal/pathresolve"
)

// shadowedNamed is how many of a copy's differences are named: the rest are counted.
const shadowedNamed = 3

// noteShadowedFeatures says, before a profile is composed from features (and the base features), which of
// them will be read from a copy of the user's own in ~/.vibe/library that differs from the one vibe ships.
// Such a copy is the user's to keep, and vibe won't touch it, but it beats the shipped one whole: what vibe
// changes in the feature afterwards never reaches the profile, and nothing says so. A copy that is the
// same as the shipped one is not spoken of.
func noteShadowedFeatures(lay layout.Layout, features []string) {
	var seen []string
	for _, name := range append(lay.BaseFeatures(), features...) {
		if slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		mine, shipped, ok := lay.Override(name)
		if !ok {
			continue
		}
		differences := library.Differences(mine, shipped)
		if len(differences) == 0 {
			continue
		}
		named := differences
		if len(named) > shadowedNamed {
			named = differences[:shadowedNamed]
		}
		more := ""
		if len(differences) > len(named) {
			more = fmt.Sprintf(" and %d more", len(differences)-len(named))
		}
		note("%s is your own copy of feature '%s', and profiles are composed from it, not from the one vibe ships", pathresolve.Tilde(mine), name)
		note("  it differs from the shipped one in %s%s, so what vibe changes in '%s' doesn't reach the profile", strings.Join(named, ", "), more, name)
		note("  delete your copy to use the shipped one, or keep it if the difference is yours: diff -r %s %s", pathresolve.Tilde(mine), shipped)
	}
}
