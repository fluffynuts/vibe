package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxPaste is the most the page may paste at once: the clipboard page's own
// limit.
const maxPaste = 50 << 20

// pasteClient talks to the sandbox's clipboard page: on this machine, never
// through a proxy, and not for long. The page is the sandbox's: a redirect
// from it isn't followed (this machine would fetch wherever it said).
var pasteClient = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// clipboardAt is the sandbox's clipboard page, or "" when it has none.
func (s *Server) clipboardAt() string {
	if s.cfg.ClipboardURL == nil {
		return ""
	}
	return cached(s.cache, "clipboard-url", 5*time.Second, s.cfg.ClipboardURL)
}

// clipboardPaste hands what was pasted on the page (an image, a file) to
// the clipboard page, as a paste there would be: it goes to the agent with
// the next message. The clipboard page's answer is passed on as it is: one
// already waiting comes back as a duplicate.
func (s *Server) clipboardPaste(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(w, r) {
		return
	}
	base := s.clipboardAt()
	if base == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this sandbox has no clipboard page to paste to: its profile hasn't the clipboard bridge"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pasteClient.Timeout)
	defer cancel()
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/items", http.MaxBytesReader(w, r.Body, maxPaste))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	if name := r.Header.Get("X-Filename"); name != "" {
		out.Header.Set("X-Filename", name)
	}
	res, err := pasteClient.Do(out)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("that's over %d MB: too large to paste", maxPaste>>20)})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the clipboard page couldn't be reached (is the sandbox running?): " + err.Error()})
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the clipboard page answered with a redirect, which isn't followed: try pasting on the clipboard page itself"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	io.Copy(w, io.LimitReader(res.Body, 1<<20))
}

// pastesWaiting is how many pastes are waiting to go to the agent with the
// next message, and whether that could be told: the clipboard page says,
// and is asked at most every couple of seconds. When it can't be told, the
// page says nothing of pastes; the clipboard page's own trouble is shown
// beside the conversation.
func (s *Server) pastesWaiting() (int, bool) {
	base := s.clipboardAt()
	if base == "" {
		return 0, false
	}
	n := cached(s.cache, "pastes-waiting "+base, 2*time.Second, func() int {
		res, err := probeClient.Get(base + "/api/items")
		if err != nil {
			return -1
		}
		defer res.Body.Close()
		var body struct {
			Items []struct {
				Delivered *string `json:"delivered"`
			} `json:"items"`
		}
		if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body) != nil {
			return -1
		}
		waiting := 0
		for _, item := range body.Items {
			if item.Delivered == nil {
				waiting++
			}
		}
		return waiting
	})
	return n, n >= 0
}

// sameOrigin refuses a request made from another site: the token stops
// most, but a page's calls need never come from anywhere else.
func sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request refused"})
		return false
	}
	return true
}
