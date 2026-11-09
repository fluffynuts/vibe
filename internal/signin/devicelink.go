package signin

import (
	"net/url"
	"regexp"
	"strings"
)

// Link is the address and code sbx's sign-in flow asks someone to confirm in a browser.
type Link struct {
	URL  string
	Code string
}

var addressInText = regexp.MustCompile(`https://[^\s"'<>)]+`)

// DeviceLink finds the sign-in flow's address in what sbx printed. It goes by the address's shape (an https address on
// docker.com carrying a user_code) and not by the words around it, which sbx may change or translate.
func DeviceLink(text string) (Link, bool) {
	for _, candidate := range addressInText.FindAllString(text, -1) {
		parsed, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if host != "docker.com" && !strings.HasSuffix(host, ".docker.com") {
			continue
		}
		if code := parsed.Query().Get("user_code"); code != "" {
			return Link{URL: candidate, Code: code}, true
		}
	}
	return Link{}, false
}
