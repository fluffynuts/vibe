// Package nugetfetch fetches a NuGet package onto the host for a sandbox
// that asked for it (see docs/plan-nuget-feeds.md). The request comes from
// the agent, so it is data only and checked before anything is written or
// run: nothing of it is ever interpreted as code, and nothing from the
// agent's workspace is read.
package nugetfetch

import (
	"fmt"
	"regexp"
)

// Request is one package the sandbox asks for, at one exact version.
type Request struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Framework string `json:"framework"`
}

const maxIDLength = 100

var (
	idPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	// An exact version only — no ranges, no floating "*" — as NuGet reads
	// one: up to four numeric parts, then SemVer 2's prerelease and build.
	versionPattern = regexp.MustCompile(`^\d+(\.\d+){0,3}(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	// The target framework monikers a package is asked for: net8.0,
	// netstandard2.0, netcoreapp3.1 and the old net48 form.
	frameworkPattern = regexp.MustCompile(`^(net\d+\.\d+|netstandard\d\.\d|netcoreapp\d\.\d|net\d{2,3})$`)
)

// Validate says what is wrong with r, in the agent's terms, or nil.
func (r Request) Validate() error {
	if r.ID == "" || len(r.ID) > maxIDLength || !idPattern.MatchString(r.ID) {
		return fmt.Errorf("package id %q is not a NuGet id: letters, digits, '.', '_' and '-', up to %d characters", r.ID, maxIDLength)
	}
	if !versionPattern.MatchString(r.Version) {
		return fmt.Errorf("version %q is not one exact version, such as 1.2.3 or 1.2.3-beta.1", r.Version)
	}
	if !frameworkPattern.MatchString(r.Framework) {
		return fmt.Errorf("framework %q is not a target framework such as net8.0 or netstandard2.0", r.Framework)
	}
	return nil
}
