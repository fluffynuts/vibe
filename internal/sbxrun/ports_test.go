package sbxrun

import (
	"reflect"
	"testing"
)

// Only the shape sbx prints is read: one that isn't, from some other sbx,
// is an error to see, not something to make a guess at.
func TestParsePortsRejectsShapesSbxDoesNotPrint(t *testing.T) {
	for _, doc := range []string{
		`[{"sandboxPort":5391,"hostPort":5396,"hostIP":"127.0.0.1","protocol":"tcp"}]`,
		`[{"sandbox_port":"5391/tcp","host_port":"5396","host_ip":"127.0.0.1"}]`,
		`{"sandbox":"proj","ports":[{"ContainerPort":5391,"HostPort":5396}]}`,
		`{"5391/tcp":[{"HostIp":"127.0.0.1","HostPort":"5396"}]}`,
	} {
		if got, err := parsePorts([]byte(doc)); err == nil {
			t.Errorf("parsePorts(%s) = %v, want an error", doc, got)
		}
	}
}

// What sbx v0.48 really prints: the tcp4 mapping asked for at creation, then
// the dual-stack one sbx adds on an ephemeral port at every start. Both
// are kept, in order, and the second's IPv4 and IPv6 rows count once.
func TestParsePortsKeepsEveryMappingOfAPort(t *testing.T) {
	doc := `[
  {"host_ip": "127.0.0.1", "host_port": 5391, "sandbox_port": 5391, "protocol": "tcp4"},
  {"host_ip": "127.0.0.1", "host_port": 5405, "sandbox_port": 5390, "protocol": "tcp4"},
  {"host_ip": "127.0.0.1", "host_port": 32777, "sandbox_port": 5391, "protocol": "tcp"},
  {"host_ip": "::1", "host_port": 32777, "sandbox_port": 5391, "protocol": "tcp"},
  {"host_ip": "127.0.0.1", "host_port": 32778, "sandbox_port": 5390, "protocol": "tcp"},
  {"host_ip": "::1", "host_port": 32778, "sandbox_port": 5390, "protocol": "tcp"}
]`
	got, err := parsePorts([]byte(doc))
	if err != nil {
		t.Fatalf("parsePorts: %v", err)
	}
	want := map[int][]int{5391: {5391, 32777}, 5390: {5405, 32778}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePorts = %v, want %v", got, want)
	}
}

func TestParsePortsWithNothingPublishedIsEmpty(t *testing.T) {
	for _, doc := range []string{`[]`, `null`, ``} {
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
