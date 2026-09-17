package kitspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeDocsMapsRecurseListsAppendScalarsOverride(t *testing.T) {
	base := Doc{
		"name": "base",
		"permissions": Doc{
			"network": Doc{
				"allow": []interface{}{"a.com", "b.com"},
			},
		},
		"environment": Doc{
			"variables": Doc{"A": "1"},
		},
	}
	override := Doc{
		"name": "profile",
		"permissions": Doc{
			"network": Doc{
				"allow": []interface{}{"c.com"},
			},
		},
		"environment": Doc{
			"variables": Doc{"B": "2"},
		},
	}
	got := MergeDocs(base, override)

	if got["name"] != "profile" {
		t.Errorf("scalar should be overridden: name = %v", got["name"])
	}
	allow := got["permissions"].(Doc)["network"].(Doc)["allow"].([]interface{})
	if len(allow) != 3 || allow[0] != "a.com" || allow[2] != "c.com" {
		t.Errorf("expected appended allow list, got %v", allow)
	}
	vars := got["environment"].(Doc)["variables"].(Doc)
	if vars["A"] != "1" || vars["B"] != "2" {
		t.Errorf("expected merged variables map, got %v", vars)
	}
}

func TestInstallStepsOrderAndDirectives(t *testing.T) {
	libDir := t.TempDir()
	profDir := t.TempDir()
	libScripts := filepath.Join(libDir, "install-scripts")
	profScripts := filepath.Join(profDir, "install-scripts")
	must(t, os.MkdirAll(libScripts, 0o755))
	must(t, os.MkdirAll(profScripts, 0o755))

	must(t, os.WriteFile(filepath.Join(libScripts, "02-second"), []byte("# vibe: user: 1000\necho lib-second\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(libScripts, "01-first"), []byte("echo lib-first\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(profScripts, "01-profile-first"), []byte("# vibe: description: custom desc\necho profile-first\n"), 0o644))

	steps, err := InstallSteps(libScripts, profScripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	// defaults' own numeric order, then profile's, defaults entirely first.
	s0 := steps[0].(Doc)
	s1 := steps[1].(Doc)
	s2 := steps[2].(Doc)
	if s0["description"] != "01-first" {
		t.Errorf("step 0 = %v", s0)
	}
	if s1["description"] != "02-second" || s1["user"] != "1000" {
		t.Errorf("step 1 = %v", s1)
	}
	if s2["description"] != "custom desc" || s2["user"] != "0" {
		t.Errorf("step 2 = %v", s2)
	}
	if got := s2["command"].(string); got != "echo profile-first\n" {
		t.Errorf("expected directive line stripped from command, got %q", got)
	}
}

func TestFileEntriesOverrideAndSidecar(t *testing.T) {
	libDir := t.TempDir()
	profDir := t.TempDir()
	libFiles := filepath.Join(libDir, "agent-files", ".local", "bin")
	profFiles := filepath.Join(profDir, "agent-files", ".claude")
	must(t, os.MkdirAll(libFiles, 0o755))
	must(t, os.MkdirAll(profFiles, 0o755))

	must(t, os.WriteFile(filepath.Join(libFiles, "tool"), []byte("#!/bin/sh\necho hi\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(profFiles, "settings.json"), []byte("{\"a\":1}\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(profFiles, "settings.json.vibe"), []byte("onlyIfMissing: true\ndescription: deny rules\n"), 0o644))

	entries, err := FileEntries(filepath.Join(libDir, "agent-files"), filepath.Join(profDir, "agent-files"))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Doc{}
	for _, e := range entries {
		d := e.(Doc)
		byPath[d["path"].(string)] = d
	}

	tool, ok := byPath[AgentHome+"/.local/bin/tool"]
	if !ok {
		t.Fatal("expected .local/bin/tool entry")
	}
	if tool["mode"] != "0755" {
		t.Errorf("expected shebang file to be mode 0755, got %v", tool["mode"])
	}

	settings, ok := byPath[AgentHome+"/.claude/settings.json"]
	if !ok {
		t.Fatal("expected .claude/settings.json entry")
	}
	if settings["onlyIfMissing"] != true {
		t.Errorf("expected onlyIfMissing true via sidecar, got %v", settings["onlyIfMissing"])
	}
	if settings["description"] != "deny rules" {
		t.Errorf("expected description from sidecar, got %v", settings["description"])
	}
	if settings["content"] != "{\"a\":1}\n" {
		t.Errorf("sidecar metadata should not affect deployed content, got %q", settings["content"])
	}
	// The sidecar file itself must never appear as a deployed entry.
	if _, ok := byPath[AgentHome+"/.claude/settings.json.vibe"]; ok {
		t.Error("sidecar file should not be deployed")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
