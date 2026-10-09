package settings

import (
	"reflect"
	"strings"
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

const shipped = `memory: 12g
agent: claude
# ticked by default when a guided profile is created
defaultFeatures:
  - diffity
`

func TestSetReplacesKeepingTheRest(t *testing.T) {
	data := []byte(shipped)
	var err error
	for _, kv := range []struct {
		key   string
		value interface{}
	}{{"memory", "8g"}, {"agent", "codex"}, {"defaultFeatures", []string{"diffity", "mysql"}}} {
		if data, err = Set(data, kv.key, kv.value); err != nil {
			t.Fatalf("Set(%s): %v", kv.key, err)
		}
	}
	want := `memory: 8g
agent: codex
# ticked by default when a guided profile is created
defaultFeatures:
  - diffity
  - mysql
`
	if string(data) != want {
		t.Errorf("Set gave:\n%s\nwant:\n%s", data, want)
	}
}

func TestSetToNoFeatures(t *testing.T) {
	data, err := Set([]byte(shipped), "defaultFeatures", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "defaultFeatures: []") {
		t.Errorf("Set to no features gave:\n%s", data)
	}
}

func TestSetAddsAMissingKey(t *testing.T) {
	data, err := Set([]byte("agent: claude\n"), "memory", "4g")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "agent: claude\nmemory: 4g\n" {
		t.Errorf("Set gave %q", data)
	}
	if data, err = Set(nil, "memory", "4g"); err != nil || string(data) != "memory: 4g\n" {
		t.Errorf("Set on an empty file gave %q, %v", data, err)
	}
}

func TestSetRefusesANonMapping(t *testing.T) {
	if _, err := Set([]byte("- a\n- b\n"), "memory", "4g"); err == nil {
		t.Error("Set on a list succeeded")
	}
}

func TestMergeCompanionOverrides(t *testing.T) {
	got := Merge(Settings{Companion: "open"}, Settings{Companion: "off"})
	if got.Companion != "off" {
		t.Errorf("Companion = %q, want off", got.Companion)
	}
	got = Merge(Settings{Companion: "serve"}, Settings{})
	if got.Companion != "serve" {
		t.Errorf("Companion = %q, want serve (unset override should not clobber base)", got.Companion)
	}
}
