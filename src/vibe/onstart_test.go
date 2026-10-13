package main

import (
	"strings"
	"testing"

	"vibe/internal/state"
)

func TestPublishedURLsPreferTheLiveMapping(t *testing.T) {
	records := []state.PublishRecord{
		{Name: "diffity", ContainerPort: 5391, HostPort: 5396},
		{Name: "api", ContainerPort: 8080, HostPort: 8080},
		{Name: "api", ContainerPort: 8081, HostPort: 8082}, // not the entry's first port
	}
	live := map[int][]int{5391: {5399}}
	got := publishedURLs(records, live)
	if len(got) != 2 {
		t.Fatalf("publishedURLs = %+v, want one per publish entry", got)
	}
	if got[0].name != "diffity" || got[0].url() != "http://localhost:5399" || got[0].unverified {
		t.Errorf("diffity = %+v, want the live port 5399, verified", got[0])
	}
	if got[0].fileContent() != "http://localhost:5399\n" {
		t.Errorf("verified file content = %q", got[0].fileContent())
	}
	if got[1].name != "api" || got[1].url() != "http://localhost:8080" || !got[1].unverified {
		t.Errorf("api = %+v, want the recorded port 8080, marked unverified", got[1])
	}
	if c := got[1].fileContent(); !strings.HasPrefix(c, "http://localhost:8080\nunverified: ") {
		t.Errorf("unverified file content = %q", c)
	}
}

// sbx adds a mapping of its own on an ephemeral port at every start, which
// doesn't reach the service; while the recorded one is still listed, it is
// the one to hand out.
func TestPublishedURLsKeepTheRecordedMappingWhileSbxListsIt(t *testing.T) {
	records := []state.PublishRecord{{Name: "clipboard", ContainerPort: 5390, HostPort: 5405}}
	for _, live := range []map[int][]int{
		{5390: {5405, 32778}},
		{5390: {32778, 5405}},
	} {
		got := publishedURLs(records, live)
		if len(got) != 1 || got[0].url() != "http://localhost:5405" || got[0].unverified {
			t.Errorf("publishedURLs with %v = %+v, want the recorded port 5405, verified", live, got)
		}
	}
}
