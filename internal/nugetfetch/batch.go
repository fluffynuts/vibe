package nugetfetch

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// BatchProjectXML is the throwaway project whose restore fetches pkgs, each at
// exactly its version, for one framework. In order, so the same batch is the
// same project; the ids and versions must be valid, or they could be markup.
func BatchProjectXML(framework string, pkgs []Package) string {
	sorted := append([]Package(nil), pkgs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var b strings.Builder
	// The job is only to get these exact versions into the cache, so what NuGet
	// would warn the project's owner of (known vulnerabilities, a package a
	// direct reference is lower than) isn't a reason to fetch none of them.
	fmt.Fprintf(&b, "<Project Sdk=\"Microsoft.NET.Sdk\">\n  <PropertyGroup>\n    <TargetFramework>%s</TargetFramework>\n    <NuGetAudit>false</NuGetAudit>\n    <NoWarn>$(NoWarn);NU1605</NoWarn>\n  </PropertyGroup>\n  <ItemGroup>\n", framework)
	for _, p := range sorted {
		fmt.Fprintf(&b, "    <PackageReference Include=\"%s\" Version=\"[%s]\" />\n", p.ID, p.Version)
	}
	b.WriteString("  </ItemGroup>\n</Project>\n")
	return b.String()
}

// BatchResult is what a batch fetch did.
type BatchResult struct {
	// Output is dotnet's last, so a failure is in NuGet's words.
	Output string
	// Fetched are the packages that weren't in the cache and now are.
	Fetched []Package
	// Unresolved are the packages no feed has (NuGet said so, by name): set
	// aside, so they don't keep the rest from coming.
	Unresolved []Package
	// UnresolvedWhy is what NuGet said of each, from the restore that said it.
	UnresolvedWhy map[Package]string
	// Failed are the packages NuGet failed on otherwise, each with its words, found
	// by restoring halves: one it fails on keeps it from fetching any of the others.
	Failed []Failure
}

// Failure is a package NuGet couldn't fetch, and what it said.
type Failure struct {
	Package Package `json:"package"`
	Message string  `json:"message"`
}

// maxBatchRestores is how many restores one batch may take, in all: setting aside what
// no feed has, and finding what else NuGet fails on. What's left when they're spent is
// said as not tried, not left out.
const maxBatchRestores = 40

var unresolvedPattern = regexp.MustCompile(`NU110[12]: Unable to find package ([A-Za-z0-9_.-]+)`)

// restoreBatch is what FetchBatch restores with; tests stand in for it.
var restoreBatch = restore

// FetchBatch restores pkgs for framework into opts.Cache. A package NuGet says no feed
// has is named from its own message, set aside, and the rest restored again: a
// restore that can't find one doesn't fetch the others. A failure NuGet doesn't put a
// name to is found by restoring halves, the package it fails on is said with NuGet's
// words, and the rest still come: so an error is only for what isn't a package's fault,
// a restore that can't be run, or that ran out of time, or was cancelled.
func FetchBatch(ctx context.Context, framework string, pkgs []Package, opts Options) (BatchResult, error) {
	if !frameworkPattern.MatchString(framework) {
		return BatchResult{}, fmt.Errorf("framework %q is not a target framework such as net8.0", framework)
	}
	if len(pkgs) == 0 {
		return BatchResult{}, fmt.Errorf("no packages to fetch")
	}
	for _, p := range pkgs {
		if !valid(p.ID, p.Version) {
			return BatchResult{}, fmt.Errorf("%q %q is not a package id and an exact version", p.ID, p.Version)
		}
	}
	run := &batchRun{ctx: ctx, framework: framework, opts: opts, budget: maxBatchRestores}
	absent := Missing(opts.Cache, pkgs)
	err := run.attempt(append([]Package(nil), pkgs...))
	for _, p := range absent {
		if len(Missing(opts.Cache, []Package{p})) == 0 {
			run.res.Fetched = append(run.res.Fetched, p)
		}
	}
	sort.Slice(run.res.Unresolved, func(i, j int) bool { return run.res.Unresolved[i].ID < run.res.Unresolved[j].ID })
	sort.Slice(run.res.Failed, func(i, j int) bool { return run.res.Failed[i].Package.ID < run.res.Failed[j].Package.ID })
	return run.res, err
}

// batchRun is one FetchBatch under way.
type batchRun struct {
	ctx       context.Context
	framework string
	opts      Options
	budget    int
	res       BatchResult
}

// attempt fetches todo: in one restore if NuGet will, else without what no feed has,
// else half at a time until the package it fails on is alone.
func (b *batchRun) attempt(todo []Package) error {
	if len(todo) == 0 {
		return nil
	}
	if b.budget <= 0 {
		for _, p := range todo {
			b.res.Failed = append(b.res.Failed, Failure{p, "not tried: finding which packages NuGet fails on took more restores than one fetch is allowed"})
		}
		return nil
	}
	b.budget--
	out, err := restoreBatch(b.ctx, BatchProjectXML(b.framework, todo), b.opts)
	b.res.Output = out
	if err == nil {
		return nil
	}
	var late timedOut
	if errors.As(err, &late) || b.ctx.Err() != nil {
		return fmt.Errorf("fetching %d packages: %w", len(todo), err)
	}
	if set := unresolvedIn(out, todo); len(set) > 0 {
		b.res.Unresolved = append(b.res.Unresolved, set...)
		if b.res.UnresolvedWhy == nil {
			b.res.UnresolvedWhy = map[Package]string{}
		}
		for _, p := range set {
			b.res.UnresolvedWhy[p] = linesAbout(out, p.ID)
		}
		return b.attempt(without(todo, set))
	}
	if len(todo) == 1 {
		b.res.Failed = append(b.res.Failed, Failure{todo[0], reportable(out)})
		return nil
	}
	half := len(todo) / 2
	if err := b.attempt(todo[:half]); err != nil {
		return err
	}
	return b.attempt(todo[half:])
}

// unresolvedIn is the packages of among that NuGet's output says it couldn't
// find: by id, which is all it says, whatever the case.
func unresolvedIn(output string, among []Package) []Package {
	named := map[string]bool{}
	for _, m := range unresolvedPattern.FindAllStringSubmatch(output, -1) {
		named[strings.ToLower(strings.TrimRight(m[1], "."))] = true
	}
	var out []Package
	for _, p := range among {
		if named[strings.ToLower(p.ID)] {
			out = append(out, p)
		}
	}
	return out
}

// linesAbout is the lines of NuGet's output that name id (whatever the case), as it said them.
func linesAbout(output, id string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(id)) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return strings.Join(lines, "\n")
}

func without(pkgs, drop []Package) []Package {
	gone := map[Package]bool{}
	for _, p := range drop {
		gone[p] = true
	}
	var out []Package
	for _, p := range pkgs {
		if !gone[p] {
			out = append(out, p)
		}
	}
	return out
}
