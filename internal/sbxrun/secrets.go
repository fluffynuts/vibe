package sbxrun

import (
	"context"
	"strings"
)

// SecretServices are the services (anthropic, openai, github...) sbx has a secret stored for that a sandbox gets:
// the global ones, and those stored for the sandbox alone. text is what `sbx secret ls` prints: a table of SCOPE,
// TYPE, NAME and SECRET, one row for each. A row that isn't a service's (a registry's), or that doesn't have the
// four columns, is passed over. This reads the table as the user's sbx printed it on 2026-11-03.
func SecretServices(text, sandbox string) []string {
	var services []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "SCOPE" || fields[1] != "service" {
			continue
		}
		if fields[0] != "(global)" && fields[0] != sandbox {
			continue
		}
		if !seen[fields[2]] {
			seen[fields[2]] = true
			services = append(services, fields[2])
		}
	}
	return services
}

// Secrets is what `sbx secret ls` prints, for SecretServices to read.
func Secrets(ctx context.Context) (string, error) {
	return captureOutContext(ctx, "secret", "ls")
}
