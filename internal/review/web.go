package review

import (
	"regexp"
	"strings"
)

// gitHubRepo is a GitHub repository's path in a remote's URL, once its scheme, user and host are gone: owner/name, with the
// characters GitHub allows in them (so that what is made of it is a plain address, never anything else).
var gitHubRepo = regexp.MustCompile(`^([A-Za-z0-9-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// gitHubWeb is the address on GitHub of the repository a remote's URL names (https://github.com/owner/name), from any of the forms
// git takes it in: https (with or without credentials), git@github.com:, ssh://. "" for anything that isn't on github.com.
func gitHubWeb(url string) string {
	rest := url
	switch {
	case strings.HasPrefix(rest, "git@"):
		host, path, ok := strings.Cut(strings.TrimPrefix(rest, "git@"), ":")
		if !ok || !strings.EqualFold(host, "github.com") {
			return ""
		}
		rest = path
	default:
		scheme, after, ok := strings.Cut(rest, "://")
		if !ok || (scheme != "https" && scheme != "http" && scheme != "ssh") {
			return ""
		}
		host, path, _ := strings.Cut(after, "/")
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		host, _, _ = strings.Cut(host, ":")
		if !strings.EqualFold(host, "github.com") {
			return ""
		}
		rest = path
	}
	m := gitHubRepo.FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	return "https://github.com/" + m[1] + "/" + m[2]
}

// web is the repository's address on GitHub, for the page to link pull requests and issues to: upstream's when it is on GitHub (in
// a fork, origin is the fork, and the pull requests are upstream's), else origin's; "" when neither is.
func (r *repository) web() string {
	for _, name := range []string{"upstream", "origin"} {
		remote, err := r.git.Remote(name)
		if err != nil {
			continue
		}
		for _, url := range remote.Config().URLs {
			if web := gitHubWeb(url); web != "" {
				return web
			}
		}
	}
	return ""
}
