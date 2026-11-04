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
	all := func(sandbox string) []string {
		keys, oauth := SecretKinds(secretTable, sandbox)
		got := append(keys, oauth...)
		slices.Sort(got)
		return got
	}
	if want := []string{"anthropic", "cursor", "github"}; !slices.Equal(all("vibe"), want) {
		t.Errorf("vibe has %v, want %v", all("vibe"), want)
	}
	if want := []string{"anthropic", "cursor"}; !slices.Equal(all("other"), want) {
		t.Errorf("another sandbox has %v, want %v", all("other"), want)
	}
}

func TestSecretsThatAreNotServicesAreNotCounted(t *testing.T) {
	text := "SCOPE TYPE NAME SECRET\n(global) registry ghcr.io (stored)\n(global) service openai (stored)\n"
	if keys, oauth := SecretKinds(text, "box"); !slices.Equal(keys, []string{"openai"}) || len(oauth) != 0 {
		t.Errorf("keys %v, oauth %v", keys, oauth)
	}
}

func TestNothingStoredIsNoServices(t *testing.T) {
	for _, text := range []string{"", "\n", "No secrets stored.\n", "SCOPE TYPE NAME SECRET\n"} {
		if keys, oauth := SecretKinds(text, "box"); len(keys)+len(oauth) != 0 {
			t.Errorf("%q gave %v, %v", text, keys, oauth)
		}
	}
}

// A secret signed in by OAuth (a Claude or Cursor login) is not an API key: the agents that take it send it their own
// way, and another that sends it as a key is refused ("invalid x-api-key").
func TestOnlyASecretStoredAsAKeyIsAKeyAndTheOAuthOnesAreToldApart(t *testing.T) {
	keys, oauth := SecretKinds(secretTable, "vibe")
	slices.Sort(keys)
	slices.Sort(oauth)
	if !slices.Equal(keys, []string{"github"}) || !slices.Equal(oauth, []string{"anthropic", "cursor"}) {
		t.Errorf("keys %v, oauth %v", keys, oauth)
	}
}

func TestAServiceWithBothAKeyAndALoginCountsAsHavingAKey(t *testing.T) {
	text := "SCOPE TYPE NAME SECRET\n(global) service anthropic (oauth configured)\nbox service anthropic (stored)\n"
	keys, oauth := SecretKinds(text, "box")
	if !slices.Equal(keys, []string{"anthropic"}) || len(oauth) != 0 {
		t.Errorf("keys %v, oauth %v", keys, oauth)
	}
}
