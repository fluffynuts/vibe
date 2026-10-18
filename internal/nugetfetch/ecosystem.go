package nugetfetch

import (
	"context"
	"fmt"
	"strings"

	"vibe/internal/packagereq"
)

// Ecosystem is NuGet, as a desk fetches packages (see internal/packagereq).
type Ecosystem struct {
	// Options are where a fetch for r happens, asked for each fetch: the
	// workspace config the user has approved can change between them.
	Options func(r packagereq.Request) Options
	// Hint, when set, is added to a failure for what the user could do to
	// make it work ("" for nothing).
	Hint func() string
}

func nugetRequest(r packagereq.Request) Request {
	return Request{ID: r.ID, Version: r.Version, Framework: r.Framework}
}

// Validate says what's wrong with r, or nil.
func (Ecosystem) Validate(r packagereq.Request) error {
	if r.Ecosystem != "nuget" {
		return fmt.Errorf("%q isn't a NuGet request", r.Ecosystem)
	}
	return nugetRequest(r).Validate()
}

// maxReported is the most of dotnet's output that goes back to the agent.
const maxReported = 3000

// Fetch fetches r, and says what came of it: the packages now in the cache
// or, on failure, NuGet's own words.
func (e Ecosystem) Fetch(ctx context.Context, r packagereq.Request) (string, error) {
	res, err := Fetch(ctx, nugetRequest(r), e.Options(r))
	if err != nil {
		report := reportable(err.Error() + "\n" + res.Output)
		if e.Hint != nil {
			if hint := e.Hint(); hint != "" {
				report += "\n\n" + hint
			}
		}
		return report, err
	}
	if len(res.Fetched) == 0 {
		return fmt.Sprintf("%s %s is in the host's package cache already: restore again", r.ID, r.Version), nil
	}
	return fmt.Sprintf("fetched %s into the host's package cache: restore again", strings.Join(res.Fetched, ", ")), nil
}

// reportable is the end of text, where dotnet says what went wrong.
func reportable(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > maxReported {
		text = "…" + text[len(text)-maxReported:]
	}
	return text
}
