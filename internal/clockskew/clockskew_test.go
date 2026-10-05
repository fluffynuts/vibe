package clockskew

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSkewReadsTheReferenceServersDate(t *testing.T) {
	for _, want := range []time.Duration{0, 2 * time.Hour, -3 * time.Hour} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Date", time.Now().Add(-want).UTC().Format(http.TimeFormat))
		}))
		old := Reference
		Reference = srv.URL
		got, err := Skew()
		Reference = old
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if d := got - want; d < -2*time.Second || d > 2*time.Second {
			t.Errorf("Skew with the server %s behind = %s", want, got)
		}
	}
}

func TestSkewFailsWithoutADate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil // stop net/http adding one
	}))
	defer srv.Close()
	old := Reference
	Reference = srv.URL
	defer func() { Reference = old }()
	if _, err := Skew(); err == nil {
		t.Error("Skew with no Date header didn't fail")
	}
}

func TestWarning(t *testing.T) {
	tests := []struct {
		skew time.Duration
		want string
	}{
		{0, ""},
		{Tolerance, ""},
		{-Tolerance, ""},
		{2*time.Hour + 10*time.Second, "about 2h0m0s ahead of"},
		{-90 * time.Minute, "about 1h30m0s behind"},
	}
	for _, tt := range tests {
		got := Warning(tt.skew)
		if tt.want == "" {
			if got != "" {
				t.Errorf("Warning(%s) = %q, want none", tt.skew, got)
			}
			continue
		}
		if !strings.Contains(got, tt.want) || !strings.Contains(got, "timezone") {
			t.Errorf("Warning(%s) = %q, want it to say %q and mention the timezone", tt.skew, got, tt.want)
		}
	}
}
