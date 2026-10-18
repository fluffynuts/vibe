package main

import (
	"os"
	"path/filepath"

	agentmem "vibe/internal/memory"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// folder is one of the host's folders a sandbox is given: mounted at its
// own path (read-only, when readOnly), with variables telling the sandbox
// where it is.
type folder struct {
	path     string
	readOnly bool
	env      []string
	// made is whether vibe makes it when it isn't there.
	made bool
	// note is what creating the sandbox says of it; "" says nothing.
	note string
}

// mount is the folder as sbx create takes it.
func (f folder) mount() string {
	if f.readOnly {
		return f.path + ":ro"
	}
	return f.path
}

const (
	nugetFallback = "fallback"
	nugetShared   = "shared"
)

// nugetFolder is what a sandbox is given of the host's NuGet folder, when
// the host has what it would be given. Shared is all of it, read-write.
// Otherwise only the package cache goes in, read-only: the rest holds
// feeds' credentials and plugins NuGet runs, and a package the agent can
// write to runs on the host at its next build.
func nugetFolder(merged settings.Settings) (folder, bool) {
	if merged.NugetDir == "" {
		return folder{}, false
	}
	path, env, note := filepath.Join(merged.NugetDir, "packages"), "SBX_HOST_NUGET_PACKAGES=", " (packages, read-only)"
	readOnly := true
	if merged.Nuget == nugetShared {
		path, env, note, readOnly = merged.NugetDir, "SBX_HOST_NUGET=", " (shared read-write: the agent can plant code in packages your builds use, and read your feeds' credentials)", false
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return folder{}, false
	}
	return folder{path: path, readOnly: readOnly, env: []string{env + path}, note: "nuget:     " + path + note}, true
}

// sandboxFolders is every folder of the host's that sandbox name, working
// in target, is given, in the order they're mounted. It is the one place
// they're decided: creating a sandbox mounts these, and its fingerprint
// counts them, so a release that changes them alone leaves sandboxes out of
// date rather than on the old ones, unnoticed.
func sandboxFolders(vibeHome, name, target string, merged settings.Settings, agent, memoryStore string) []folder {
	var folders []folder
	if f, ok := nugetFolder(merged); ok {
		folders = append(folders, f)
	}
	if agentmem.Supported(agent) {
		if memoryStore == "" {
			memoryStore = agentmem.StoreFor(vibeHome, merged.MemoryRoot, name)
		}
		folders = append(folders, folder{path: memoryStore, env: []string{"VIBE_MEMORY_STORE=" + memoryStore}, made: true,
			note: "memories:  " + memoryStore})
	}
	// The companion page's Review tab keeps the sandbox's reviews on the
	// host; vibe-review, in the sandbox, drops the agent's comments in the
	// inbox there, and reads the review from the view, read-only (see
	// docs/plan-review-tab.md). Nothing else of it is the sandbox's to
	// touch: the host acts on what's there. vibe-review runs git where the
	// tab does: from the workspace's top.
	reviews := state.ReviewPath(vibeHome, name)
	folders = append(folders,
		folder{path: filepath.Join(reviews, "inbox"), made: true,
			env: []string{"VIBE_REVIEW_DIR=" + reviews, "VIBE_REVIEW_WORKSPACE=" + target}},
		folder{path: filepath.Join(reviews, "view"), readOnly: true, made: true})
	// The clipboard page's history is kept on the host, so what was pasted
	// outlives the sandbox (see start-clipboard).
	for _, p := range merged.Publish {
		if p.Name == "clipboard" && len(p.Ports) > 0 {
			store := state.ClipboardPath(vibeHome, name)
			folders = append(folders, folder{path: store, env: []string{"VIBE_CLIPBOARD_DIR=" + store}, made: true,
				note: "clipboard: " + store})
			break
		}
	}
	return folders
}
