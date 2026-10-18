package nugetfetch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
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

// One at a time: a restore holds locks in the cache, and a flood of
// requests should queue rather than pile up.
var one sync.Mutex

// Fetch restores r into opts.Cache and removes what it made. It never reads
// the agent's workspace.
func Fetch(ctx context.Context, r Request, opts Options) (Result, error) {
	if err := r.Validate(); err != nil {
		return Result{}, err
	}
	one.Lock()
	defer one.Unlock()

	dir, err := os.MkdirTemp(opts.Root, "fetch-")
	if err != nil {
		return Result{}, fmt.Errorf("making a folder to fetch in under %s: %w", opts.Root, err)
	}
	defer os.RemoveAll(dir)

	project := filepath.Join(dir, "fetch.csproj")
	if err := os.WriteFile(project, []byte(r.ProjectXML()), 0o644); err != nil {
		return Result{}, err
	}
	if opts.ApprovedConfig != nil {
		if err := os.WriteFile(filepath.Join(dir, "nuget.config"), opts.ApprovedConfig, 0o644); err != nil {
			return Result{}, err
		}
	}

	before := cached(opts.Cache)
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dotnet", RestoreArgs(project)...)
	cmd.Dir = dir
	cmd.Env = RestoreEnv(opts.Env, opts.Cache)
	out, err := cmd.CombinedOutput()
	res := Result{Output: string(out), Fetched: added(before, cached(opts.Cache))}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return res, fmt.Errorf("fetching %s %s passed its time limit of %s", r.ID, r.Version, opts.Timeout)
	}
	if err != nil {
		return res, fmt.Errorf("fetching %s %s: %w", r.ID, r.Version, err)
	}
	return res, nil
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
