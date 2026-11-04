package sbxrun

import (
	"context"
	"strings"
)

// SecretKinds are the services sbx has a secret stored for that a sandbox gets (the global ones, and those stored for
// the sandbox alone), told apart. text is what `sbx secret ls` prints: a table of SCOPE, TYPE, NAME and SECRET, one
// row for each; a row that isn't a service's (a registry's), or without the columns, is passed over. This reads the
// table as the user's sbx printed it on 2026-11-03. The two kinds are: those stored as a key (an API key
// the proxy puts on a request), and those signed in by OAuth, which only the agent they were made for can use. A
// service with both counts as having a key.
func SecretKinds(text, sandbox string) (keys, oauth []string) {
	isOAuth := map[string]bool{}
	var order []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "SCOPE" || fields[1] != "service" {
			continue
		}
		if fields[0] != "(global)" && fields[0] != sandbox {
			continue
		}
		service := fields[2]
		login := strings.Contains(strings.Join(fields[3:], " "), "oauth")
		if _, seen := isOAuth[service]; !seen {
			order = append(order, service)
			isOAuth[service] = login
		} else if !login {
			isOAuth[service] = false
		}
	}
	for _, service := range order {
		if isOAuth[service] {
			oauth = append(oauth, service)
		} else {
			keys = append(keys, service)
		}
	}
	return keys, oauth
}

// Secrets is what `sbx secret ls` prints, for SecretServices to read.
func Secrets(ctx context.Context) (string, error) {
	return captureOutContext(ctx, "secret", "ls")
}
