package library

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// flatDirFor returns a DirFor that resolves every feature directly under
// root — the shape most of these tests need, where every feature lives in
// one flat "library" directory rather than split across two layers.
func flatDirFor(root string) DirFor {
	return func(feature string) string { return filepath.Join(root, feature) }
}

func TestListDescribesFromInstallScriptFirst(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "mysql", "install-scripts", "01-install-mysql"),
		"#!/bin/sh\n# vibe: description: Install mysql-server\necho hi\n")
	write(t, filepath.Join(dir, "mysql", "agent-files", ".local", "bin", "start-mysql"),
		"#!/bin/sh\n# vibe: description: should not be used\necho hi\n")

	features := List([]string{"mysql"}, flatDirFor(dir))
	if len(features) != 1 || features[0].Name != "mysql" {
		t.Fatalf("features = %+v", features)
	}
	if features[0].Description != "Install mysql-server" {
		t.Errorf("Description = %q", features[0].Description)
	}
	if got, want := features[0].Label(), "mysql: Install mysql-server"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
}

func TestListFallsBackToAgentFileDescription(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "nuget", "agent-files", ".local", "bin", "link-nuget"),
		"#!/bin/sh\n# vibe: description: Link the host nuget cache\necho hi\n")

	features := List([]string{"nuget"}, flatDirFor(dir))
	if len(features) != 1 {
		t.Fatalf("features = %+v", features)
	}
	if features[0].Description != "Link the host nuget cache" {
		t.Errorf("Description = %q", features[0].Description)
	}
}

func TestListFallsBackToBareNameWithNoDescriptionAnywhere(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "redis", "install-scripts", "01-install-redis-server"), "echo hi\n")

	features := List([]string{"redis"}, flatDirFor(dir))
	if features[0].Description != "" {
		t.Errorf("Description = %q, want empty", features[0].Description)
	}
	if got, want := features[0].Label(), "redis"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
}

func TestListPreservesGivenOrderAndResolvesEachIndependently(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "rabbitmq", "install-scripts", "01-a"), "echo\n")
	overlay := t.TempDir()
	write(t, filepath.Join(overlay, "dotnet", "install-scripts", "01-a"),
		"#!/bin/sh\n# vibe: description: my custom dotnet\necho\n")

	// dotnet resolves from the overlay, rabbitmq from the (flat) bundle —
	// List must not assume every feature comes from the same directory.
	dirFor := func(feature string) string {
		if feature == "dotnet" {
			return filepath.Join(overlay, "dotnet")
		}
		return filepath.Join(dir, feature)
	}

	features := List([]string{"rabbitmq", "dotnet"}, dirFor)
	if len(features) != 2 || features[0].Name != "rabbitmq" || features[1].Name != "dotnet" {
		t.Fatalf("features = %+v, want the given order preserved", features)
	}
	if features[1].Description != "my custom dotnet" {
		t.Errorf("expected dotnet resolved from its own dir, got %+v", features[1])
	}
}

// seedTwoFeatures gives mysql two install-scripts (numbered 01 and 02, plus
// a start-mysql agent-file) and rabbitmq one install-script (numbered 01,
// plus its own start-rabbit agent-file), so combining them tests both the
// per-feature offset and multiple scripts within one feature.
func seedTwoFeatures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "mysql", "install-scripts", "01-install-mysql"), "echo install-mysql\n")
	write(t, filepath.Join(dir, "mysql", "install-scripts", "02-configure-mysql"), "echo configure-mysql\n")
	write(t, filepath.Join(dir, "mysql", "agent-files", ".local", "bin", "start-mysql"), "echo start-mysql\n")
	write(t, filepath.Join(dir, "rabbitmq", "install-scripts", "01-install-rabbitmq"), "echo install-rabbitmq\n")
	write(t, filepath.Join(dir, "rabbitmq", "agent-files", ".local", "bin", "start-rabbit"), "echo start-rabbit\n")
	return dir
}

func TestComposeRenumbersEachFeatureBlockInOrder(t *testing.T) {
	libDir := seedTwoFeatures(t)
	profileDir := t.TempDir()

	if err := Compose([]string{"mysql", "rabbitmq"}, flatDirFor(libDir), profileDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(profileDir, "install-scripts"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"01-install-mysql", "02-configure-mysql", "03-install-rabbitmq"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("install-scripts = %v, want %v", names, want)
	}

	content, err := os.ReadFile(filepath.Join(profileDir, "install-scripts", "03-install-rabbitmq"))
	if err != nil || string(content) != "echo install-rabbitmq\n" {
		t.Errorf("content = %q, %v", content, err)
	}

	for _, want := range []string{"start-mysql", "start-rabbit"} {
		if _, err := os.Stat(filepath.Join(profileDir, "agent-files", ".local", "bin", want)); err != nil {
			t.Errorf("expected agent-file %s: %v", want, err)
		}
	}

	onStart, err := os.ReadFile(filepath.Join(profileDir, "agent-files", ".local", "bin", "on-start"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(onStart)
	if i, j := strings.Index(got, "start-mysql"), strings.Index(got, "start-rabbit"); i < 0 || j < 0 || i > j {
		t.Errorf("on-start does not run start-mysql before start-rabbit:\n%s", got)
	}
}

func TestComposeReversedOrderOffsetsFromTheOtherFeature(t *testing.T) {
	libDir := seedTwoFeatures(t)
	profileDir := t.TempDir()

	if err := Compose([]string{"rabbitmq", "mysql"}, flatDirFor(libDir), profileDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(profileDir, "install-scripts"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"01-install-rabbitmq", "02-install-mysql", "03-configure-mysql"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("install-scripts = %v, want %v", names, want)
	}
}

func TestComposeExpandsDigitWidthPastNinetyNine(t *testing.T) {
	libDir := t.TempDir()
	var features []string
	for i := 0; i < 101; i++ {
		feature := "f" + strconv.Itoa(i)
		write(t, filepath.Join(libDir, feature, "install-scripts", "01-only"), "echo\n")
		features = append(features, feature)
	}
	profileDir := t.TempDir()

	if err := Compose(features, flatDirFor(libDir), profileDir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(profileDir, "install-scripts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 101 {
		t.Fatalf("got %d entries, want 101", len(entries))
	}
	if first, last := entries[0].Name(), entries[len(entries)-1].Name(); first != "001-only" || last != "101-only" {
		t.Errorf("first = %q, last = %q, want 3-digit-wide names", first, last)
	}
}

func TestComposeNoInstallScriptsStillCopiesAgentFilesAndOnStart(t *testing.T) {
	libDir := t.TempDir()
	write(t, filepath.Join(libDir, "nuget", "agent-files", ".local", "bin", "link-nuget"), "echo link\n")
	profileDir := t.TempDir()

	if err := Compose([]string{"nuget"}, flatDirFor(libDir), profileDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "install-scripts")); !os.IsNotExist(err) {
		t.Errorf("expected no install-scripts dir, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "agent-files", ".local", "bin", "link-nuget")); err != nil {
		t.Errorf("expected link-nuget copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "agent-files", ".local", "bin", "on-start")); err != nil {
		t.Errorf("expected a generated on-start: %v", err)
	}
}

func TestComposeNoFeaturesWithStartScriptsWritesNoOnStart(t *testing.T) {
	libDir := t.TempDir()
	write(t, filepath.Join(libDir, "dotnet", "install-scripts", "01-install-dotnet-sdk"), "echo\n")
	profileDir := t.TempDir()

	if err := Compose([]string{"dotnet"}, flatDirFor(libDir), profileDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "agent-files", ".local", "bin", "on-start")); !os.IsNotExist(err) {
		t.Errorf("expected no on-start when no feature has a startup script, got err=%v", err)
	}
}
