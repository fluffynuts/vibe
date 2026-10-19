package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vibe/internal/layout"
	"vibe/internal/nugetfetch"
	"vibe/internal/packagereq"
	"vibe/internal/pathresolve"
	"vibe/internal/review"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// fetchWithin bounds one restore of what the agent asked for: the requests
// approved together go in one, which can be dozens of packages.
const fetchWithin = 15 * time.Minute

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

// resolved is path with its links followed: where it is, or will be, if it isn't made yet. The links
// of the part that exists are followed, and the rest is kept as it was named.
func resolved(path string) string {
	rest := ""
	for p := path; ; {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
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

// syncWithin bounds one fetch of what the sandbox restored: a whole solution's
// packages, downloaded in one go.
const syncWithin = 15 * time.Minute

// packageSyncFor is what fetches, for the user's IDE, the packages the sandbox
// restored into its own cache (an IDE on the host reads the host's), or nil
// when the sandbox isn't given the host's packages to fall back on, or its
// folder holds what NuGet reads on the host (nothing is fetched for it: see
// packagesUnavailable).
func packageSyncFor(lay layout.Layout, name string, inst state.Instance, merged settings.Settings) *nugetfetch.Sync {
	cache, ok := nugetFallbackPackages(merged)
	if !ok || packagesUnavailable(inst.Target, lay.Home, merged) != "" {
		return nil
	}
	root := state.NugetFetchPath(lay.Home)
	workspace := workspaceConfigFor(lay.Home, name, inst, merged)
	return &nugetfetch.Sync{
		Workspace: inst.Target,
		Cache:     cache,
		Options: func() nugetfetch.Options {
			return nugetfetch.Options{Root: root, Cache: cache, Env: os.Environ(), Timeout: syncWithin,
				ApprovedConfig: workspace.ApprovedContent()}
		},
	}
}

// packageSyncAuto says whether to fetch what the sandbox restored without
// asking: when the user's settings allow package fetches, as they're read each
// time.
func packageSyncAuto(lay layout.Layout, profile string) func() bool {
	return func() bool {
		now, err := loadSettings(lay, profile)
		return err == nil && packagesPolicy(now) == packagereq.PolicyAllow
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

// maxOverviewListed is how many waiting requests the overview names; the rest are counted.
const maxOverviewListed = 5

// packagesOverview says, for an agent that asks (vibe-nuget status), what the user has waiting: the
// requests held for their approval, and what the sandbox restored that their package cache lacks,
// looked at as it's asked. sync is nil where the sandbox has none.
func packagesOverview(desk *packagereq.Desk, sync *nugetfetch.Sync) func() string {
	return func() string {
		var requests []string
		for _, p := range desk.Pending() {
			if !p.Approved {
				requests = append(requests, p.Request.ID+" "+p.Request.Version)
			}
		}
		missing, fetching := 0, false
		if sync != nil {
			sync.Check()
			st := sync.State()
			fetching = st.Running
			st.Running = false
			missing = st.Actionable()
		}
		return overviewText(requests, missing, fetching)
	}
}

// overviewText is the overview, in the terms an agent can act on: what is waiting for the user, and
// that it should tell them and wait, not build and download its own copies of what their cache lacks.
func overviewText(requests []string, missing int, fetching bool) string {
	var parts []string
	if n := len(requests); n > 0 {
		listed := requests
		more := ""
		if n > maxOverviewListed {
			listed = requests[:maxOverviewListed]
			more = fmt.Sprintf(" and %d more", n-maxOverviewListed)
		}
		noun := either(n == 1, "package request", "package requests")
		parts = append(parts, fmt.Sprintf("The user has %d %s waiting for their approval on the Admin tab's Packages card (%s%s). "+
			"Tell the user, and wait for them to approve before you restore or build: you can't have those packages until they do.",
			n, noun, strings.Join(listed, ", "), more))
	}
	if missing > 0 {
		one := missing == 1
		noun := either(one, "package", "packages")
		if fetching {
			parts = append(parts, fmt.Sprintf("%d %s this sandbox restored %s being fetched into the user's package cache. "+
				"Wait for that to finish, then run vibe-nuget status again before you restore or build.",
				missing, noun, either(one, "is", "are")))
		} else {
			parts = append(parts, fmt.Sprintf("%d %s this sandbox restored %s in the user's package cache, so their IDE can't see %s. "+
				"If you restore or build now you'll download your own copies of any that are missing, which their IDE won't see either. "+
				"Tell the user: the Admin tab's Packages card has a Fetch them button, which fetches them on their machine. "+
				"Wait until they have, then run vibe-nuget status again.",
				missing, noun, either(one, "isn't", "aren't"), either(one, "it", "them")))
		}
	}
	if len(parts) == 0 {
		return "Nothing is waiting for the user: no package requests to approve, and every package this sandbox restored is in their package cache."
	}
	return strings.Join(parts, "\n\n")
}

// either is a when yes, and b when not.
func either(yes bool, a, b string) string {
	if yes {
		return a
	}
	return b
}
