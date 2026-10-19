package main

import (
	"fmt"
	"os"
	"path/filepath"

	"vibe/internal/instructions"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/packagereq"
	"vibe/internal/pathresolve"
	"vibe/internal/portalloc"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// loadSettings merges the bundle's settings.yaml with the profile's.
func loadSettings(lay layout.Layout, profile string) (settings.Settings, error) {
	baseSettings, err := settings.Load(lay.File("settings.yaml"))
	if err != nil {
		return settings.Settings{}, err
	}
	profileSettings, err := settings.Load(filepath.Join(lay.ProfileDir(profile), "settings.yaml"))
	if err != nil {
		return settings.Settings{}, err
	}
	merged := settings.Merge(baseSettings, profileSettings)
	if merged.Nuget != "" && merged.Nuget != nugetFallback && merged.Nuget != nugetOverlay && merged.Nuget != nugetShared {
		return settings.Settings{}, fmt.Errorf("settings: nuget is %q; it must be %q (the host's packages, read-only), %q (the same, overlaid at their own path: experimental) or %q (all of the host's nuget folder, read-write)",
			merged.Nuget, nugetFallback, nugetOverlay, nugetShared)
	}
	if merged.Packages != "" && merged.Packages != packagereq.PolicyAsk && merged.Packages != packagereq.PolicyAllow {
		return settings.Settings{}, fmt.Errorf("settings: packages is %q; it must be %q (each request waits for you to approve it) or %q (requests are fetched at once)",
			merged.Packages, packagereq.PolicyAsk, packagereq.PolicyAllow)
	}
	merged.NugetDir = pathresolve.ExpandHome(merged.NugetDir)
	merged.MemoryRoot = pathresolve.ExpandHome(merged.MemoryRoot)
	return merged, nil
}

func loadKit(lay layout.Layout, profile string) (kitspec.Doc, settings.Settings, error) {
	if !lay.ProfileExists(profile) {
		return nil, settings.Settings{}, fmt.Errorf(
			"unknown profile '%s' — expected %s/config.yaml", profile, lay.ProfileDir(profile))
	}
	profileDir := lay.ProfileDir(profile)

	merged, err := loadSettings(lay, profile)
	if err != nil {
		return nil, settings.Settings{}, err
	}

	baseConfig, err := kitspec.LoadDoc(lay.File("config.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}
	profileConfig, err := kitspec.LoadDoc(filepath.Join(profileDir, "config.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}

	// The kits' instructions go to sbx, which offers them to the agent as
	// reading for when a kit is relevant. The user's own (instructions.File)
	// are loaded in every session, another way.
	var agentInstructions string
	if data, err := os.ReadFile(filepath.Join(profileDir, instructions.KitFile)); err == nil {
		agentInstructions = string(data)
	} else if !os.IsNotExist(err) {
		return nil, settings.Settings{}, err
	}

	doc, err := kitspec.Build(baseConfig, profileConfig, lay.DefaultsDir(), profileDir, agentInstructions)
	if err != nil {
		return nil, settings.Settings{}, err
	}
	return doc, merged, nil
}

func writeKit(doc kitspec.Doc, name string) (string, error) {
	dir, err := os.MkdirTemp("", "vibe-kit-"+name+"-")
	if err != nil {
		return "", err
	}
	data, err := kitspec.Marshal(doc)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.yaml"), data, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

type publishMapping struct {
	name          string
	containerPort int
	hostPort      int
	urlEnv        string
}

// portSearchSpan is how far above a container port allocatePublish will look
// for a free host port before giving up.
const portSearchSpan = 60

// allocatePublish assigns a host port to every container port the merged
// settings publish. The instance records are the only source of truth for who
// holds what: a sandbox re-claims the ports its own record remembers, so its
// URL survives a re-init, and every port another record claims is excluded
// even when nothing is listening on it — that sandbox may simply be stopped.
// Ports handed out earlier in this same pass are excluded on the same
// grounds, since the sandbox that will bind them does not exist yet.
func allocatePublish(vibeHome, name string, merged settings.Settings) ([]publishMapping, error) {
	instances, err := state.List(vibeHome)
	if err != nil {
		return nil, err
	}
	taken := map[int]bool{}
	remembered := map[int]int{}
	for _, inst := range instances {
		for _, rec := range inst.Publish {
			if inst.Name == name {
				remembered[rec.ContainerPort] = rec.HostPort
				continue
			}
			taken[rec.HostPort] = true
		}
	}

	var mappings []publishMapping
	for _, entry := range merged.Publish {
		for i, cp := range entry.Ports {
			hostPort := remembered[cp]
			if hostPort == 0 || taken[hostPort] || portalloc.InUse(hostPort) {
				hostPort, err = portalloc.FindFree(cp, cp+portSearchSpan, taken)
				if err != nil {
					return nil, err
				}
			}
			taken[hostPort] = true
			urlEnv := ""
			if i == 0 {
				urlEnv = entry.UrlEnv
			}
			mappings = append(mappings, publishMapping{name: entry.Name, containerPort: cp, hostPort: hostPort, urlEnv: urlEnv})
		}
	}
	return mappings, nil
}
