// Package profilegen creates profiles: blank ones (the minimum a profile
// needs to be usable) and copies of existing ones. Everything it creates
// goes into the user's ~/.vibe overlay, so the bundle stays as shipped.
package profilegen

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"vibe/internal/directive"
	"vibe/internal/fscopy"
	"vibe/internal/layout"
	"vibe/internal/library"
)

// validName is what a profile directory may be called: the same shape
// pathresolve.DeriveName produces, so a derived profile name is always
// valid, and nothing that could escape the profiles directory.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Validate reports whether name is usable as a profile directory name.
func Validate(name string) error {
	if !validName.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("'%s' is not a usable profile name — "+
			"use lowercase letters, digits, '-', '_' and '.', starting with a letter or digit", name)
	}
	return nil
}

const blankConfig = `# vibe profile "%[1]s".
#
# This file is a docker sbx kit fragment, overlaid on the base config.yaml at
# sandbox creation. Anything the base sets — permissions, environment,
# requires — can be added to or overridden here.
#
# Alongside it, a profile may carry:
#   settings.yaml          memory, agent, publish, nugetDir, ... overrides
#   install-scripts/       run once at creation, in leading-number order
#   agent-files/           copied into /home/agent in the sandbox
#   agent-instructions.md  appended to the agent's instructions
name: %[1]s
displayName: %[1]s sandbox
description: Sandbox for the %[1]s workspace
`

// CreateBlank writes the most basic usable profile: a config.yaml naming it,
// plus the empty install-scripts and agent-files directories a profile grows
// into. It returns the created profile directory.
func CreateBlank(l layout.Layout, profile string) (string, error) {
	if err := Validate(profile); err != nil {
		return "", err
	}
	dir := l.NewProfileDir(profile)
	for _, sub := range []string{"install-scripts", "agent-files"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", createErr(dir, err)
		}
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(blankConfig, profile)), 0o644); err != nil {
		return "", createErr(dir, err)
	}
	return dir, nil
}

// CreateGuided writes a new profile assembled from the chosen library
// features, in the given order: it starts from the same minimum a blank
// profile has, then has library.Compose lay it out install-scripts (each
// feature's own scripts renumbered to run in this order, one feature block
// after another) and agent-files (a later feature's file overrides an
// earlier one's at the same path), generating an on-start script when any
// feature needs one, and merging in each feature's config, settings and
// instruction fragments. It returns the created profile directory.
//
// The feature list is recorded in the profile's config.yaml before
// composing, so the profile can say later what it was made of — see
// ComposedFrom, which is what re-composing it against a changed library
// works from.
func CreateGuided(l layout.Layout, profile string, features []string) (string, error) {
	dir, err := CreateBlank(l, profile)
	if err != nil {
		return "", err
	}
	if err := recordComposedFrom(dir, features); err != nil {
		return "", createErr(dir, err)
	}
	if err := library.Compose(features, l.FeatureDir, dir); err != nil {
		return "", createErr(dir, err)
	}
	return dir, nil
}

// composedFromKey is the directive a generated profile records its feature
// list under, in its config.yaml's leading comment block — where composing
// preserves it, and where anyone reading the file can see it.
const composedFromKey = "features"

// headerEnd returns the offset in a config.yaml where its leading block of
// comment and blank lines ends, which is where a recorded directive goes.
func headerEnd(data []byte) int {
	offset := 0
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		offset += len(line)
	}
	return offset
}

// recordComposedFrom writes (or replaces) the "# vibe: features: ..." line in
// a profile's config.yaml. An empty list records nothing, so a profile
// composed from no features at all doesn't claim to have been.
func recordComposedFrom(dir string, features []string) error {
	if len(features) == 0 {
		return nil
	}
	path := filepath.Join(dir, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if _, found := directive.Parse([]byte(line))[composedFromKey]; !found {
			kept = append(kept, line)
		}
	}
	data = []byte(strings.Join(kept, "\n"))
	at := headerEnd(data)
	line := fmt.Sprintf("# vibe: %s: %s\n", composedFromKey, strings.Join(features, ", "))
	out := append([]byte{}, data[:at]...)
	out = append(out, []byte(line)...)
	out = append(out, data[at:]...)
	return os.WriteFile(path, out, 0o644)
}

// ComposedFrom returns the library features a profile was composed from, in
// the order they were composed in, or nil for a profile that records none —
// one written by hand, copied, or created blank.
func ComposedFrom(profileDir string) []string {
	data, err := os.ReadFile(filepath.Join(profileDir, "config.yaml"))
	if err != nil {
		return nil
	}
	var features []string
	for _, name := range strings.Split(directive.Parse(data).String(composedFromKey, ""), ",") {
		if name = strings.TrimSpace(name); name != "" {
			features = append(features, name)
		}
	}
	return features
}

// CopyFrom creates a profile as a copy of an existing one — wherever that
// one lives — rewriting the copy's top-level config.yaml "name:" to the new
// profile name. It returns the created profile directory.
func CopyFrom(l layout.Layout, source, profile string) (string, error) {
	if err := Validate(profile); err != nil {
		return "", err
	}
	if !l.ProfileExists(source) {
		return "", fmt.Errorf("no profile '%s' to copy from", source)
	}
	dir := l.NewProfileDir(profile)
	if err := fscopy.Tree(l.ProfileDir(source), dir); err != nil {
		return "", createErr(dir, err)
	}
	if err := rewriteName(filepath.Join(dir, "config.yaml"), profile); err != nil {
		return "", err
	}
	return dir, nil
}

// ImportToHome copies a profile the bundle ships into the overlay unchanged,
// so the user can edit their own copy of it. It returns the created
// directory.
func ImportToHome(l layout.Layout, profile string) (string, error) {
	if err := Validate(profile); err != nil {
		return "", err
	}
	src := l.BundlePath("profiles", profile)
	dir := l.NewProfileDir(profile)
	if dir == src {
		return "", fmt.Errorf("no overlay to import profile '%s' into", profile)
	}
	if err := fscopy.Tree(src, dir); err != nil {
		return "", createErr(dir, err)
	}
	return dir, nil
}

// createErr adds a hint when the target directory can't be written to, which
// is the usual reason profile creation fails.
func createErr(dir string, err error) error {
	if os.IsPermission(err) {
		return fmt.Errorf("%w\n%s is not writable — create the profile there by hand, "+
			"or point VIBE_HOME at a directory you can write to", err, dir)
	}
	return err
}

var nameLineRE = regexp.MustCompile(`(?m)^name:[^\n]*$`)

// rewriteName replaces the first top-level "name:" line in a copied
// config.yaml, leaving every comment and other field as they were.
func rewriteName(path, profile string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	replaced := false
	out := nameLineRE.ReplaceAllFunc(data, func(line []byte) []byte {
		if replaced {
			return line
		}
		replaced = true
		return []byte("name: " + profile)
	})
	if !replaced {
		out = append([]byte("name: "+profile+"\n"), out...)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
