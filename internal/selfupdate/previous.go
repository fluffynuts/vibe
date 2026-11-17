package selfupdate

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// API is GitHub's REST API listing of vibe's releases: asked only when the
// releases feed doesn't reach back far enough (see PreviousTagAt). A
// variable so tests can point it at a stand-in server.
var API = "https://api.github.com/repos/fluffynuts/vibe/releases"

// apiPageSize is the most releases GitHub's API gives in one page.
const apiPageSize = 100

// releaseTag matches a release proper ("v0.1.235", "v0.53.0"), not an RC
// ("v0.54.0-rc2"), a nightly or a dev build.
var releaseTag = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// PreviousTag returns the tag of the newest vibe release older than
// installed, or "" when there is none.
func PreviousTag(installed string) (string, error) {
	return PreviousTagAt(Releases, API, installed)
}

// PreviousTagAt returns the tag of the newest release older than installed
// ("0.1.235" or "v0.54.0") of the repo whose releases page is releases, and
// whose API listing is api, or "" when there is none. Only releases proper
// count (vX.Y.Z), never an RC or a nightly; versions compare by number.
//
// The list comes from the releases' Atom feed, which needs no API and no
// sign-in, but holds only the newest ten and doesn't page. Only when none of
// those is older than installed is the API asked (which allows 60 calls an
// hour without sign-in), a page at a time, as far as it takes. Both list
// newest first, so the first page with an older release has the newest one.
func PreviousTagAt(releases, api, installed string) (string, error) {
	tags, err := feedTags(releases + ".atom")
	if err != nil {
		return "", err
	}
	if tag := newestBefore(tags, installed); tag != "" {
		return tag, nil
	}
	for page := 1; ; page++ {
		tags, listed, err := apiTags(api, page)
		if err != nil {
			return "", err
		}
		if tag := newestBefore(tags, installed); tag != "" {
			return tag, nil
		}
		if listed == 0 {
			return "", nil
		}
	}
}

// newestBefore is the newest of tags that is a release proper older than
// installed, or "".
func newestBefore(tags []string, installed string) string {
	best := ""
	for _, tag := range tags {
		if releaseTag.MatchString(tag) && Newer(installed, tag) && (best == "" || Newer(tag, best)) {
			best = tag
		}
	}
	return best
}

// feedTags reads the tags of the releases in a GitHub releases Atom feed,
// from each entry's link to its release page (…/releases/tag/<tag>).
func feedTags(url string) ([]string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("reading the list of releases (%s): %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading the list of releases (%s): %s", url, resp.Status)
	}
	var feed struct {
		Entries []struct {
			Links []struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("reading the list of releases (%s): %w", url, err)
	}
	var tags []string
	for _, e := range feed.Entries {
		for _, l := range e.Links {
			if strings.Contains(l.Href, "/releases/tag/") {
				tags = append(tags, path.Base(l.Href))
				break
			}
		}
	}
	return tags, nil
}

// apiTags reads one page of the release tags GitHub's API lists, drafts and
// prereleases left out, and how many releases the page listed in all: none
// is the page after the last.
func apiTags(api string, page int) (tags []string, listed int, err error) {
	url := fmt.Sprintf("%s?per_page=%d&page=%d", api, apiPageSize, page)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("asking GitHub's API for older releases (%s): %w", url, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, 0, fmt.Errorf("GitHub's API refused to list older releases (%s): without sign-in it allows 60 calls an hour from one address — try again later", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return nil, 0, fmt.Errorf("asking GitHub's API for older releases (%s): %s", url, resp.Status)
	}
	var releases []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, 0, fmt.Errorf("reading GitHub's list of releases (%s): %w", url, err)
	}
	for _, r := range releases {
		if !r.Draft && !r.Prerelease {
			tags = append(tags, r.Tag)
		}
	}
	return tags, len(releases), nil
}
