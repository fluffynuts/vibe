package settings

import (
	"reflect"
	"testing"
)

func TestMergeScalarsOverride(t *testing.T) {
	base := Settings{Memory: "12g", Agent: "claude"}
	override := Settings{Memory: "24g"}
	got := Merge(base, override)
	if got.Memory != "24g" {
		t.Errorf("Memory = %q, want 24g", got.Memory)
	}
	if got.Agent != "claude" {
		t.Errorf("Agent = %q, want claude (unset override should not clobber base)", got.Agent)
	}
}

func TestMergePublishAppends(t *testing.T) {
	base := Settings{Publish: []PublishEntry{{Name: "diffity", Ports: []int{5391}}}}
	override := Settings{Publish: []PublishEntry{{Name: "extra", Ports: []int{8080}}}}
	got := Merge(base, override)
	if len(got.Publish) != 2 {
		t.Fatalf("expected 2 publish entries, got %d", len(got.Publish))
	}
	if got.Publish[0].Name != "diffity" || got.Publish[1].Name != "extra" {
		t.Errorf("unexpected publish order: %+v", got.Publish)
	}
}

func TestMergeEnvOverrideWins(t *testing.T) {
	base := Settings{Env: map[string]string{"A": "1", "B": "2"}}
	override := Settings{Env: map[string]string{"B": "3", "C": "4"}}
	got := Merge(base, override)
	want := map[string]string{"A": "1", "B": "3", "C": "4"}
	for k, v := range want {
		if got.Env[k] != v {
			t.Errorf("Env[%q] = %q, want %q", k, got.Env[k], v)
		}
	}
}

func TestMergeReplacesDefaultFeatures(t *testing.T) {
	base := Settings{DefaultFeatures: []string{"diffity"}}
	if got := Merge(base, Settings{}).DefaultFeatures; !reflect.DeepEqual(got, []string{"diffity"}) {
		t.Errorf("an override that sets none keeps the base's: %v", got)
	}
	got := Merge(base, Settings{DefaultFeatures: []string{"dotnet", "mysql"}}).DefaultFeatures
	if !reflect.DeepEqual(got, []string{"dotnet", "mysql"}) {
		t.Errorf("DefaultFeatures = %v, want the override's outright", got)
	}
}
