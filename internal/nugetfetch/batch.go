package nugetfetch

import (
	"context"
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
	fmt.Fprintf(&b, "<Project Sdk=\"Microsoft.NET.Sdk\">\n  <PropertyGroup>\n    <TargetFramework>%s</TargetFramework>\n  </PropertyGroup>\n  <ItemGroup>\n", framework)
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
}

// maxBatchRounds is how many times a batch is run, each time without what
// NuGet said no feed had.
const maxBatchRounds = 4

var unresolvedPattern = regexp.MustCompile(`NU110[12]: Unable to find package ([A-Za-z0-9_.-]+)`)

// FetchBatch restores pkgs for framework into opts.Cache, in one restore.
// A package NuGet says no feed has is named from its own message, set aside
// and the rest restored again: a restore that can't find one doesn't fetch the
// others. Anything else that fails is an error.
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
	var res BatchResult
	absent := Missing(opts.Cache, pkgs)
	todo := append([]Package(nil), pkgs...)
	for round := 0; round < maxBatchRounds && len(todo) > 0; round++ {
		out, err := restore(ctx, BatchProjectXML(framework, todo), opts)
		res.Output = out
		if err == nil {
			break
		}
		set := unresolvedIn(out, todo)
		if len(set) == 0 {
			return res, fmt.Errorf("fetching %d packages: %w", len(todo), err)
		}
		res.Unresolved = append(res.Unresolved, set...)
		todo = without(todo, set)
	}
	for _, p := range absent {
		if len(Missing(opts.Cache, []Package{p})) == 0 {
			res.Fetched = append(res.Fetched, p)
		}
	}
	sort.Slice(res.Unresolved, func(i, j int) bool { return res.Unresolved[i].ID < res.Unresolved[j].ID })
	return res, nil
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
