package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vibe/internal/layout"
	"vibe/internal/nugetfetch"
	"vibe/internal/packagereq"
	"vibe/internal/pathresolve"
	"vibe/internal/review"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// fetchWithin bounds one fetch: a restore of one package, and what it
// brings, is well within it.
const fetchWithin = 3 * time.Minute

func packagesPolicy(merged settings.Settings) string {
	if merged.Packages == packagereq.PolicyAllow {
		return packagereq.PolicyAllow
	}
	return packagereq.PolicyAsk
}

// packagesUnavailable says why no package can be fetched for a sandbox
// working in target, or "". NuGet on the host reads the user's config, and
// a nuget.config in every folder above the one it restores in; if the
// agent can write any of them it can change what a fetch does, so the
// host fetches nothing for it.
func packagesUnavailable(target, vibeHome string, merged settings.Settings) string {
	protected := []string{vibeHome}
	if merged.NugetDir != "" {
		protected = append(protected, merged.NugetDir)
	}
	if config, err := os.UserConfigDir(); err == nil {
		protected = append(protected, filepath.Join(config, "NuGet"))
	}
	folder := resolved(target)
	for _, p := range protected {
		if pathresolve.Within(folder, resolved(p)) {
			return fmt.Sprintf("this sandbox's folder %s holds %s, which NuGet reads on the host: the agent could change what a fetch does, "+
				"so vibe makes no fetches for it: start vibe in a project folder instead", target, pathresolve.Tilde(p))
		}
	}
	return ""
}

// resolved is path with its links followed, as far as it exists.
func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// packageDeskFor is where the sandbox's requests for packages are decided,
// or nil when the sandbox isn't given a way to ask (see nugetFolders).
func packageDeskFor(lay layout.Layout, name string, inst state.Instance, merged settings.Settings) *packagereq.Desk {
	cache, ok := nugetFallbackPackages(merged)
	if !ok {
		return nil
	}
	root := state.NugetFetchPath(lay.Home)
	workspace := workspaceConfigFor(lay.Home, name, inst, merged)
	return &packagereq.Desk{
		Mailbox:     packagereq.Mailbox{Dir: state.PackagesPath(lay.Home, name)},
		Unavailable: packagesUnavailable(inst.Target, lay.Home, merged),
		Policy: func() string {
			// Read each time: a change of setting applies to the next request.
			if now, err := loadSettings(lay, inst.Profile); err == nil {
				return packagesPolicy(now)
			}
			return packagereq.PolicyAsk
		},
		Ecosystems: map[string]packagereq.Ecosystem{"nuget": nugetfetch.Ecosystem{
			Options: func(packagereq.Request) nugetfetch.Options {
				return nugetfetch.Options{Root: root, Cache: cache, Env: os.Environ(), Timeout: fetchWithin,
					ApprovedConfig: workspace.ApprovedContent()}
			},
			Hint: func() string {
				if info, err := workspace.Look(); err == nil && info != nil && !info.Approved && info.Problem == "" {
					return "This folder has a nuget.config that isn't approved, so it wasn't used: if the package is on a feed it names, " +
						"the user can approve it on the companion page's Admin tab, and then you can ask again."
				}
				return ""
			},
		}},
		Describe: func(r packagereq.Request) string { return describeRequest(inst.Target, r) },
		Log:      func(line string) { note("%s", line) },
	}
}

// describeWithin bounds reading a project to say what a request is.
const describeWithin = 30 * time.Second

// describeRequest says what a request is to the project in target (new,
// an upgrade, only missing), from the project's files; the agent's word
// isn't asked.
func describeRequest(target string, r packagereq.Request) string {
	ctx, cancel := context.WithTimeout(context.Background(), describeWithin)
	defer cancel()
	mainline, err := review.DefaultBranch(ctx, target)
	if err != nil {
		return "couldn't read the project to say what this is: " + err.Error()
	}
	files, err := review.ReadProjectFiles(ctx, target, mainline.Name)
	if err != nil {
		return "couldn't read the project to say what this is: " + err.Error()
	}
	return nugetfetch.Classify(nugetfetch.Request{ID: r.ID, Version: r.Version, Framework: r.Framework},
		nugetfetch.ProjectFiles{Committed: files.Committed, Working: files.Working}, mainline.Name)
}

// workspaceConfigFor is the nuget.config in the sandbox's folder, which the
// user approves on the page before a fetch uses it.
func workspaceConfigFor(vibeHome, name string, inst state.Instance, merged settings.Settings) nugetfetch.WorkspaceConfig {
	w := nugetfetch.WorkspaceConfig{
		Dir:       inst.Target,
		Approvals: filepath.Join(state.PackagesPath(vibeHome, name), "approved-configs.json"),
	}
	if merged.NugetDir != "" {
		w.HostConfigs = append(w.HostConfigs, filepath.Join(merged.NugetDir, "NuGet", "NuGet.Config"))
	}
	if config, err := os.UserConfigDir(); err == nil {
		w.HostConfigs = append(w.HostConfigs, filepath.Join(config, "NuGet", "NuGet.Config"))
	}
	return w
}
