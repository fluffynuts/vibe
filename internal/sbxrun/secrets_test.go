package sbxrun

import (
	"slices"
	"testing"
)

// What sbx secret ls printed on the user's machine, 2026-11-03 (the table, as it is without a --format).
const secretTable = `
  SCOPE                  TYPE      NAME        SECRET
  convert-type-imports   service   github      (stored)
  vibe                   service   github      (stored)
  yumbi                  service   github      (stored)
  yumbi-alt              service   github      (stored)
  (global)               service   anthropic   (oauth configured)
  (global)               service   cursor      (oauth configured)
`

func TestTheServicesASandboxHasASecretForAreTheGlobalOnesAndItsOwn(t *testing.T) {
	got := SecretServices(secretTable, "vibe")
	slices.Sort(got)
	if want := []string{"anthropic", "cursor", "github"}; !slices.Equal(got, want) {
		t.Errorf("vibe has %v, want %v", got, want)
	}
	got = SecretServices(secretTable, "other")
	slices.Sort(got)
	if want := []string{"anthropic", "cursor"}; !slices.Equal(got, want) {
		t.Errorf("another sandbox has %v, want %v", got, want)
	}
}

func TestSecretsThatAreNotServicesAreNotCounted(t *testing.T) {
	text := "SCOPE TYPE NAME SECRET\n(global) registry ghcr.io (stored)\n(global) service openai (stored)\n"
	if got := SecretServices(text, "box"); !slices.Equal(got, []string{"openai"}) {
		t.Errorf("services: %v", got)
	}
}

func TestNothingStoredIsNoServices(t *testing.T) {
	for _, text := range []string{"", "\n", "No secrets stored.\n", "SCOPE TYPE NAME SECRET\n"} {
		if got := SecretServices(text, "box"); len(got) != 0 {
			t.Errorf("%q gave %v", text, got)
		}
	}
}
