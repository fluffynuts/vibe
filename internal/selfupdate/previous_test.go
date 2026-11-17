package selfupdate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// feedOf is a GitHub releases Atom feed listing tags, newest first, as
// github.com/<repo>/releases.atom does.
func feedOf(base string, tags ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/" xml:lang="en-US">
  <id>tag:github.com,2008:https://github.com/o/r/releases</id>
  <title>Release notes from r</title>
`)
	for _, tag := range tags {
		fmt.Fprintf(&b, `  <entry>
    <id>tag:github.com,2008:Repository/1/%[1]s</id>
    <updated>2026-11-17T10:00:00Z</updated>
    <link rel="alternate" type="text/html" href="%[2]s/releases/tag/%[1]s"/>
    <title>%[1]s</title>
  </entry>
`, tag, base)
	}
	b.WriteString("</feed>\n")
	return b.String()
}

// releaseLists serves a releases feed of feed's tags at /releases.atom and,
// at /api/releases, every tag in pages of pageSize, as GitHub's REST API
// does (newest first, ?page=N from 1). apiStatus, when not 200, is how the
// API answers instead. It returns the releases' URL and the API's, and a
// count of the API calls made.
func releaseLists(t *testing.T, feed []string, all []string, pageSize, apiStatus int) (releases, api string, apiCalls *atomic.Int32) {
	t.Helper()
	apiCalls = &atomic.Int32{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases.atom":
			w.Write([]byte(feedOf(srv.URL, feed...)))
		case "/api/releases":
			apiCalls.Add(1)
			if apiStatus != http.StatusOK {
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, `{"message":"API rate limit exceeded"}`, apiStatus)
				return
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page < 1 {
				page = 1
			}
			if r.URL.Query().Get("per_page") != strconv.Itoa(apiPageSize) {
				t.Errorf("the API was asked for %q a page, want %d", r.URL.Query().Get("per_page"), apiPageSize)
			}
			var out []map[string]any
			for i := (page - 1) * pageSize; i < page*pageSize && i < len(all); i++ {
				out = append(out, map[string]any{"tag_name": all[i], "draft": false, "prerelease": strings.Contains(all[i], "-")})
			}
			if out == nil {
				out = []map[string]any{}
			}
			json.NewEncoder(w).Encode(out)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/releases", srv.URL + "/api/releases", apiCalls
}

func TestPreviousTagIsTheNewestReleaseBeforeTheInstalledOne(t *testing.T) {
	feed := []string{"v0.1.235", "v0.1.228", "v0.1.10", "v0.1.9"}
	releases, api, calls := releaseLists(t, feed, feed, 100, http.StatusOK)
	for _, tt := range []struct{ installed, want string }{
		{"0.1.235", "v0.1.228"},
		{"0.1.230", "v0.1.228"},
		{"0.1.228", "v0.1.10"},
		{"0.1.10", "v0.1.9"}, // by number: 0.1.9 is older than 0.1.10
		{"v0.1.236", "v0.1.235"},
	} {
		got, err := PreviousTagAt(releases, api, tt.installed)
		if err != nil || got != tt.want {
			t.Errorf("PreviousTagAt(%q) = %q, %v; want %q", tt.installed, got, err, tt.want)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("the API was called %d times; the feed had the answer every time", n)
	}
}

func TestPreviousTagSkipsPrereleases(t *testing.T) {
	feed := []string{"v0.54.0", "v0.54.0-rc3", "v0.54.0-rc1", "nightly-2026-11-15", "v0.53.0", "v0.53.0-rc2"}
	releases, api, _ := releaseLists(t, feed, feed, 100, http.StatusOK)
	for _, installed := range []string{"v0.54.0", "v0.54.0-rc3"} {
		if got, err := PreviousTagAt(releases, api, installed); err != nil || got != "v0.53.0" {
			t.Errorf("PreviousTagAt(%q) = %q, %v; want v0.53.0", installed, got, err)
		}
	}
}

// The feed has only the newest ten releases: one older than all of them is
// looked up in the API, a page at a time, only as far as it needs.
func TestPreviousTagGoesToTheAPIWhenTheFeedHasNothingOlder(t *testing.T) {
	all := []string{"v0.1.240", "v0.1.239", "v0.1.238", "v0.1.237", "v0.1.236", "v0.1.200", "v0.1.199", "v0.1.150"}
	releases, api, calls := releaseLists(t, all[:3], all, 2, http.StatusOK)
	got, err := PreviousTagAt(releases, api, "0.1.201")
	if err != nil || got != "v0.1.200" {
		t.Errorf("PreviousTagAt = %q, %v; want v0.1.200", got, err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("the API was called %d times; want 3 (v0.1.200 is on the third page, of two each)", n)
	}
}

func TestPreviousTagIsNoneWhenNothingIsOlder(t *testing.T) {
	all := []string{"v0.1.240", "v0.1.239", "v0.1.238"}
	releases, api, _ := releaseLists(t, all[:2], all, 2, http.StatusOK)
	got, err := PreviousTagAt(releases, api, "0.1.238")
	if err != nil || got != "" {
		t.Errorf("PreviousTagAt = %q, %v; want nothing, and no error", got, err)
	}
}

func TestPreviousTagSaysWhenTheAPIRefuses(t *testing.T) {
	releases, api, _ := releaseLists(t, []string{"v0.1.240"}, nil, 100, http.StatusForbidden)
	_, err := PreviousTagAt(releases, api, "0.1.200")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "60") {
		t.Errorf("PreviousTagAt = %v; want an error saying the API refused, and that its limit is 60 an hour", err)
	}
}

func TestPreviousTagSaysWhenTheFeedFails(t *testing.T) {
	_, err := PreviousTagAt("http://127.0.0.1:1/releases", "http://127.0.0.1:1/api", "0.1.200")
	if err == nil || !strings.Contains(err.Error(), "releases.atom") {
		t.Errorf("PreviousTagAt = %v; want an error naming the feed", err)
	}
}
