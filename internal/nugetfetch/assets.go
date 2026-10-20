package nugetfetch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vibe/internal/plainfile"
)

// Package is one package at one exact version.
type Package struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

const (
	// maxAssetsFile is the most of a project.assets.json that's read: a big
	// solution's is a few megabytes, and the sandbox writes them.
	maxAssetsFile = 64 << 20
	// maxPackages is how many packages are taken from one framework's list.
	maxPackages = 5000
	// maxFrameworks is how many frameworks are taken from one file.
	maxFrameworks = 16
	// maxAssetsFiles is how many assets files a folder is searched for.
	maxAssetsFiles = 500
)

// Assets are the packages a restore resolved, from a project.assets.json
// the sandbox wrote: as data, never run.
type Assets struct {
	// ByFramework is each target framework's packages, in order of id.
	ByFramework map[string][]Package
	// Downloads is each target framework's packages that were only downloaded (PackageDownload:
	// targeting packs, say), which NuGet keeps apart from what it resolved, in order of id.
	Downloads map[string][]Package
	// Skipped is how many entries weren't taken: not a package id and exact
	// version, past a limit, or of a framework that isn't one.
	Skipped int
}

// ReadAssets reads the packages each target framework resolved from the
// project.assets.json at path. Only what's a valid package id and exact
// version, of a plain target framework (net8.0, not net8.0/linux-x64), is
// taken: the file is the sandbox's to write.
func ReadAssets(path string) (Assets, error) {
	data, err := plainfile.Read(path, maxAssetsFile)
	var tooLarge plainfile.TooLarge
	switch {
	case errors.As(err, &tooLarge):
		return Assets{}, fmt.Errorf("%s is too large to read (over %d MB)", path, maxAssetsFile>>20)
	case err != nil:
		return Assets{}, err
	}
	var doc struct {
		Targets map[string]map[string]struct {
			Type string `json:"type"`
		} `json:"targets"`
		Project struct {
			Frameworks map[string]struct {
				DownloadDependencies []struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"downloadDependencies"`
			} `json:"frameworks"`
		} `json:"project"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Assets{}, fmt.Errorf("%s isn't a project.assets.json: %w", path, err)
	}
	if doc.Targets == nil {
		return Assets{}, fmt.Errorf("%s isn't a project.assets.json: it has no targets", path)
	}
	out := Assets{ByFramework: map[string][]Package{}, Downloads: map[string][]Package{}}
	frameworks := make([]string, 0, len(doc.Targets))
	for tfm := range doc.Targets {
		frameworks = append(frameworks, tfm)
	}
	sort.Strings(frameworks)
	taken := 0
	for _, tfm := range frameworks {
		entries := doc.Targets[tfm]
		if !frameworkPattern.MatchString(tfm) || taken >= maxFrameworks {
			out.Skipped += len(entries)
			continue
		}
		taken++
		var pkgs []Package
		for key, e := range entries {
			if e.Type != "package" {
				continue
			}
			id, version, ok := strings.Cut(key, "/")
			if !ok || !valid(id, version) {
				out.Skipped++
				continue
			}
			pkgs = append(pkgs, Package{id, version})
		}
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ID < pkgs[j].ID })
		if len(pkgs) > maxPackages {
			out.Skipped += len(pkgs) - maxPackages
			pkgs = pkgs[:maxPackages]
		}
		if len(pkgs) > 0 {
			out.ByFramework[tfm] = pkgs
		}
	}
	for _, tfm := range downloadFrameworks(doc.Project.Frameworks) {
		entries := doc.Project.Frameworks[tfm].DownloadDependencies
		if !frameworkPattern.MatchString(tfm) {
			out.Skipped += len(entries)
			continue
		}
		var pkgs []Package
		for _, d := range entries {
			version := exactVersion(d.Version)
			if !valid(d.Name, version) {
				out.Skipped++
				continue
			}
			pkgs = append(pkgs, Package{d.Name, version})
		}
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ID < pkgs[j].ID })
		if len(pkgs) > maxPackages {
			out.Skipped += len(pkgs) - maxPackages
			pkgs = pkgs[:maxPackages]
		}
		if len(pkgs) > 0 {
			out.Downloads[tfm] = pkgs
		}
	}
	return out, nil
}

// downloadFrameworks is the frameworks that have downloads, in order, at most maxFrameworks.
func downloadFrameworks[T any](frameworks map[string]T) []string {
	names := make([]string, 0, len(frameworks))
	for tfm := range frameworks {
		names = append(names, tfm)
	}
	sort.Strings(names)
	if len(names) > maxFrameworks {
		names = names[:maxFrameworks]
	}
	return names
}

// exactVersion is the version a range of NuGet's names alone: [1.0.0] or [1.0.0, 1.0.0], which is
// how a download is written; anything else is a range, and no one version: "".
func exactVersion(rng string) string {
	rng = strings.TrimSpace(rng)
	if !strings.HasPrefix(rng, "[") || !strings.HasSuffix(rng, "]") {
		return ""
	}
	low, high, two := strings.Cut(strings.Trim(rng, "[]"), ",")
	low = strings.TrimSpace(low)
	if two && strings.TrimSpace(high) != low {
		return ""
	}
	return low
}

func valid(id, version string) bool {
	return id != "" && len(id) <= maxIDLength && idPattern.MatchString(id) && versionPattern.MatchString(version)
}

// FindAssets is the project.assets.json files in the obj folders under dir,
// not looking in build output, dependencies or .git, nor following links, and
// at most maxAssetsFiles of them.
func FindAssets(dir string) []string {
	var found []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "bin":
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "project.assets.json" && d.Type().IsRegular() && filepath.Base(filepath.Dir(path)) == "obj" {
			found = append(found, path)
			if len(found) >= maxAssetsFiles {
				return fs.SkipAll
			}
		}
		return nil
	})
	return found
}

// Missing is the packages whose folder isn't whole in cache: NuGet writes
// .nupkg.metadata last, so a package is there once that is. The cache keeps
// ids and versions in lower case.
func Missing(cache string, pkgs []Package) []Package {
	var out []Package
	for _, p := range pkgs {
		id, version := strings.ToLower(p.ID), strings.ToLower(p.Version)
		if _, err := os.Stat(filepath.Join(cache, id, version, ".nupkg.metadata")); err != nil {
			out = append(out, p)
		}
	}
	return out
}

// ReadNamedElsewhere reads the project.nuget.cache at path, which names the file of each package a
// restore used, and gives the packages whose files are anywhere but under root. Only what is
// a package's own file, at <root>/<id>/<version>/<id>.<version>.nupkg.sha512, is taken: the file is
// the sandbox's to write. The ids and versions are in the lower case the cache keeps.
func ReadNamedElsewhere(path string, roots ...string) ([]Package, error) {
	data, err := plainfile.Read(path, maxAssetsFile)
	var tooLarge plainfile.TooLarge
	switch {
	case errors.As(err, &tooLarge):
		return nil, fmt.Errorf("%s is too large to read (over %d MB)", path, maxAssetsFile>>20)
	case err != nil:
		return nil, err
	}
	var doc struct {
		Expected []string `json:"expectedPackageFiles"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s isn't a project.nuget.cache: %w", path, err)
	}
	for i := range roots {
		roots[i] = strings.ToLower(strings.TrimRight(strings.ReplaceAll(roots[i], "\\", "/"), "/")) + "/"
	}
	seen := map[Package]bool{}
	var out []Package
	for _, file := range doc.Expected {
		if len(out) >= maxPackages {
			break
		}
		slashed := strings.ReplaceAll(file, "\\", "/")
		lower := strings.ToLower(slashed)
		under := false
		for _, root := range roots {
			if strings.HasPrefix(lower, root) {
				under = true
			}
		}
		if under {
			continue
		}
		parts := strings.Split(lower, "/")
		if len(parts) < 3 {
			continue
		}
		id, version, name := parts[len(parts)-3], parts[len(parts)-2], parts[len(parts)-1]
		if id == "." || id == ".." || version == "." || version == ".." || name != id+"."+version+".nupkg.sha512" || !valid(id, version) {
			continue
		}
		p := Package{id, version}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, nil
}
