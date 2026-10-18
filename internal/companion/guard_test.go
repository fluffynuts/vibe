package companion

import (
	"net/http"
	"testing"
)

// changingRoutes are the page's calls that change something: each needs
// the token, its feature, and a same-origin JSON request.
var changingRoutes = []string{
	"/api/conversation/send", "/api/conversation/stop", "/api/conversation/new",
	"/api/conversation/new/options", "/api/conversation/new/fetch",
	"/api/default-branch",
	"/api/admin/do", "/api/admin/upgrades", "/api/admin/cancel",
	"/api/admin/instructions", "/api/admin/instructions/optimise",
	"/api/review/do", "/api/review/ask", "/api/review/submit", "/api/review/start", "/api/review/afresh",
	"/api/manage/plan", "/api/manage/delete", "/api/manage/do", "/api/manage/cancel",
}

func TestEveryChangeNeedsItsFeature(t *testing.T) {
	s := newServer(&fakeSbx{})
	for _, path := range changingRoutes {
		rec := do(t, s, req{method: "POST", path: path, body: "{}", headers: authed(map[string]string{"Content-Type": "application/json"})})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s with no such feature: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestEveryChangeNeedsTheTokenAndASameOriginJSONRequest(t *testing.T) {
	s, _ := reviewTabServer(t)
	s.cfg.Converse = (&fakeAgent{}).converse
	s.cfg.Admin = &fakeAdmin{}
	s.cfg.Manage = &fakeManager{}
	for _, path := range changingRoutes {
		for _, c := range []struct {
			why     string
			headers map[string]string
			want    int
		}{
			{"no token", map[string]string{"Content-Type": "application/json"}, http.StatusUnauthorized},
			{"not JSON", authed(map[string]string{"Content-Type": "text/plain"}), http.StatusUnsupportedMediaType},
			{"another site", authed(map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example"}), http.StatusForbidden},
		} {
			if rec := do(t, s, req{method: "POST", path: path, body: "{}", headers: c.headers}); rec.Code != c.want {
				t.Errorf("%s, %s: %d %s, want %d", path, c.why, rec.Code, rec.Body, c.want)
			}
		}
	}
}
