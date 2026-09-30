package sbxrun

import (
	"reflect"
	"testing"
)

// sbx doesn't document its `ports --json` shape, so these are the shapes a
// ports listing plausibly takes; each has to come out the same way.
func TestParsePortsReadsTheLikelyShapes(t *testing.T) {
	want := map[int]int{5391: 5396, 8080: 8081}
	tests := []struct{ name, json string }{
		{"flat list, camelCase", `[{"sandboxPort":5391,"hostPort":5396,"hostIP":"127.0.0.1","protocol":"tcp"},
			{"sandboxPort":8080,"hostPort":8081,"hostIP":"127.0.0.1","protocol":"tcp"}]`},
		{"snake_case strings", `[{"sandbox_port":"5391/tcp","host_port":"5396","host_ip":"127.0.0.1"},
			{"sandbox_port":"8080/tcp","host_port":"8081","host_ip":"::1"}]`},
		{"wrapped, container-named", `{"sandbox":"proj","ports":[{"ContainerPort":5391,"HostPort":5396},
			{"ContainerPort":8080,"HostPort":8081}]}`},
		{"plain port key", `{"ports":[{"port":5391,"hostPort":5396},{"port":8080,"hostPort":8081}]}`},
		{"docker style", `{"5391/tcp":[{"HostIp":"127.0.0.1","HostPort":"5396"},{"HostIp":"::1","HostPort":"5396"}],
			"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"8081"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePorts([]byte(tt.json))
			if err != nil {
				t.Fatalf("parsePorts: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("parsePorts = %v, want %v", got, want)
			}
		})
	}
}

func TestParsePortsWithNothingPublishedIsEmpty(t *testing.T) {
	for _, doc := range []string{`[]`, `{}`, `null`, `{"ports":[]}`} {
		got, err := parsePorts([]byte(doc))
		if err != nil || len(got) != 0 {
			t.Errorf("parsePorts(%s) = %v, %v; want empty, nil", doc, got, err)
		}
	}
}

// Output that has content but no recognisable mapping must not be read as
// "nothing published" — the caller would then trust a stale port.
func TestParsePortsRejectsWhatItCannotRead(t *testing.T) {
	for _, doc := range []string{`[{"from":5396,"to":5391}]`, `not json`} {
		if got, err := parsePorts([]byte(doc)); err == nil {
			t.Errorf("parsePorts(%s) = %v, want an error", doc, got)
		}
	}
}
