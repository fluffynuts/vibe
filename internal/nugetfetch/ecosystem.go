package nugetfetch

import (
	"context"
	"fmt"
	"slices"
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

// Fetch fetches rs, a restore for each framework, and says what came of each request: the
// package now in the cache or, on failure, NuGet's own words. What NuGet can't fetch fails
// its own request and no other.
func (e Ecosystem) Fetch(ctx context.Context, rs []packagereq.Request) []packagereq.Outcome {
	outcomes := make([]packagereq.Outcome, len(rs))
	var frameworks []string
	at := map[string][]int{}
	for i, r := range rs {
		if _, seen := at[r.Framework]; !seen {
			frameworks = append(frameworks, r.Framework)
		}
		at[r.Framework] = append(at[r.Framework], i)
	}
	for _, framework := range frameworks {
		indexes := at[framework]
		pkgs := make([]Package, len(indexes))
		for j, i := range indexes {
			pkgs[j] = Package{ID: rs[i].ID, Version: rs[i].Version}
		}
		opts := e.Options(rs[indexes[0]])
		res, err := FetchBatch(ctx, framework, pkgs, opts)
		for j, i := range indexes {
			outcomes[i] = e.outcome(rs[i], pkgs[j], res, err, opts.Cache)
		}
	}
	return outcomes
}

// outcome is what came of one request of a batch whose result is res (err when
// it couldn't be run at all).
func (e Ecosystem) outcome(r packagereq.Request, p Package, res BatchResult, err error, cache string) packagereq.Outcome {
	failure := func(words string, cause error) packagereq.Outcome {
		report := reportable(words)
		if e.Hint != nil {
			if hint := e.Hint(); hint != "" {
				report += "\n\n" + hint
			}
		}
		return packagereq.Outcome{Message: report, Err: cause}
	}
	if err != nil {
		return failure(err.Error()+"\n"+res.Output, err)
	}
	for _, f := range res.Failed {
		if f.Package == p {
			return failure(f.Message, fmt.Errorf("NuGet failed on %s %s", r.ID, r.Version))
		}
	}
	for _, u := range res.Unresolved {
		if u == p {
			return failure(res.UnresolvedWhy[p], fmt.Errorf("no feed has %s %s", r.ID, r.Version))
		}
	}
	if slices.Contains(res.Fetched, p) {
		return packagereq.Outcome{Message: fmt.Sprintf("fetched %s %s into the host's package cache: restore again", strings.ToLower(r.ID), strings.ToLower(r.Version))}
	}
	if len(Missing(cache, []Package{p})) == 0 {
		return packagereq.Outcome{Message: fmt.Sprintf("%s %s is in the host's package cache already: restore again", r.ID, r.Version)}
	}
	return failure(fmt.Sprintf("NuGet finished without an error, but %s %s isn't in the host's package cache", r.ID, r.Version),
		fmt.Errorf("%s %s isn't in the cache", r.ID, r.Version))
}

// reportable is the end of text, where dotnet says what went wrong.
func reportable(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > maxReported {
		text = "…" + text[len(text)-maxReported:]
	}
	return text
}
