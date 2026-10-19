package nugetfetch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Options say where a fetch happens. Nothing here comes from the agent.
type Options struct {
	// Root is the folder the throwaway project is made in. It and every
	// folder above it must be out of the sandbox's reach: NuGet reads a
	// nuget.config from any of them.
	Root string
	// Cache is the host's package cache, which the packages land in.
	Cache string
	// Env is the environment dotnet runs in; its HOME decides whose NuGet
	// config is read.
	Env []string
	// Timeout bounds the restore.
	Timeout time.Duration
	// ApprovedConfig, when set, is a workspace nuget.config the user has
	// approved; it is put beside the project so NuGet layers it over theirs.
	ApprovedConfig []byte
}

// Result is what a fetch did.
type Result struct {
	// Output is dotnet's own, so a failure is in NuGet's words.
	Output string
	// Fetched is each package that is newly in the cache, as "id version".
	Fetched []string
}

// Fetch restores r into opts.Cache and removes what it made. It never reads
// the agent's workspace. Fetches may run together: each has a folder of its
// own, and NuGet locks the cache per package.
func Fetch(ctx context.Context, r Request, opts Options) (Result, error) {
	if err := r.Validate(); err != nil {
		return Result{}, err
	}
	before := cached(opts.Cache)
	out, err := restore(ctx, r.ProjectXML(), opts)
	res := Result{Output: out, Fetched: added(before, cached(opts.Cache))}
	if err != nil {
		return res, fmt.Errorf("fetching %s %s: %w", r.ID, r.Version, err)
	}
	return res, nil
}

// restore restores the throwaway project projectXML in a folder of its own
// under opts.Root, which is removed after, giving what dotnet said. A restore
// that ran out of time says so.
// timedOut is a restore that ran out of time: not a package's fault, so not
// something to split a batch over.
type timedOut struct{ limit time.Duration }

func (e timedOut) Error() string { return fmt.Sprintf("passed its time limit of %s", e.limit) }

func restore(ctx context.Context, projectXML string, opts Options) (string, error) {
	if err := os.MkdirAll(opts.Root, 0o700); err != nil {
		return "", fmt.Errorf("making %s to fetch in: %w", opts.Root, err)
	}
	dir, err := os.MkdirTemp(opts.Root, "fetch-")
	if err != nil {
		return "", fmt.Errorf("making a folder to fetch in under %s: %w", opts.Root, err)
	}
	defer os.RemoveAll(dir)

	project := filepath.Join(dir, "fetch.csproj")
	if err := os.WriteFile(project, []byte(projectXML), 0o644); err != nil {
		return "", err
	}
	if opts.ApprovedConfig != nil {
		if err := os.WriteFile(filepath.Join(dir, "nuget.config"), opts.ApprovedConfig, 0o644); err != nil {
			return "", err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dotnet", RestoreArgs(project)...)
	cmd.Dir = dir
	cmd.Env = RestoreEnv(opts.Env, opts.Cache)
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return string(out), timedOut{opts.Timeout}
	}
	return string(out), err
}

// cached lists the cache as "id version", ids lowercase as NuGet keeps them.
func cached(cache string) map[string]bool {
	found := map[string]bool{}
	ids, _ := os.ReadDir(cache)
	for _, id := range ids {
		versions, _ := os.ReadDir(filepath.Join(cache, id.Name()))
		for _, v := range versions {
			found[id.Name()+" "+v.Name()] = true
		}
	}
	return found
}

func added(before, after map[string]bool) []string {
	var out []string
	for k := range after {
		if !before[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
