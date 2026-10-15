package main

import "testing"

func TestALoggedOutAgentIsOnlySaidToBeWhenThatsCertain(t *testing.T) {
	out := `{"loggedIn": false, "authMethod": "none"}`
	in := `{"loggedIn": true, "authMethod": "claude.ai"}`
	for _, tc := range []struct {
		name, mode, status string
		ok, want           bool
	}{
		{"logged in", "none", in, true, false},
		{"logged out, its own login the only way", "none", out, true, true},
		{"logged out, no credential mode at all", "", out, true, true},
		// sbx may be handing it credentials itself, which auth status
		// may not know of.
		{"logged out, sbx may supply credentials", "proxy", out, true, false},
		{"status couldn't be read", "none", "", false, false},
		{"status unreadable", "none", "claude: command not found", true, false},
	} {
		if got := agentLoggedOut(tc.mode, tc.status, tc.ok); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
