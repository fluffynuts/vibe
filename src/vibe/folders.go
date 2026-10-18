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

// sandboxFolders is every folder of the host's that sandbox name, working
// in target, is given, in the order they're mounted. It is the one place
// they're decided: creating a sandbox mounts these, and its fingerprint
// counts them, so a release that changes them alone leaves sandboxes out of
// date rather than on the old ones, unnoticed.
func sandboxFolders(vibeHome, name, target string, merged settings.Settings, agent, memoryStore string) []folder {
	var folders []folder
	if merged.NugetDir != "" {
		if info, err := os.Stat(merged.NugetDir); err == nil && info.IsDir() {
			folders = append(folders, folder{path: merged.NugetDir, env: []string{"SBX_HOST_NUGET=" + merged.NugetDir},
				note: "nuget:     " + merged.NugetDir + " (shared with host)"})
		}
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
