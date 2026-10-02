package hostmem

import (
	"reflect"
	"testing"
)

// reported is roughly what Linux reports for a machine with gib GB
// installed: a little under.
func reported(gib uint64) uint64 {
	return gib*GiB - 400<<20
}

func TestOptions(t *testing.T) {
	for _, tt := range []struct {
		total uint64
		want  []string
	}{
		{reported(16), []string{"4g", "8g"}},
		{16 * GiB, []string{"4g", "8g"}},
		{reported(32), []string{"4g", "8g", "12g", "16g"}},
		{reported(24), []string{"4g", "8g", "12g"}},
		{reported(8), []string{"4g"}},
		{reported(6), []string{"3g"}},
		{reported(1), []string{"512m"}},
	} {
		if got := Options(tt.total); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Options(%d) = %v, want %v", tt.total, got, tt.want)
		}
	}
}

func TestLimitStaysUnderWhatSbxAllows(t *testing.T) {
	for gib := uint64(2); gib <= 256; gib++ {
		total := reported(gib)
		if limit := Limit(total); limit > total*3/4 {
			t.Errorf("Limit(%d GiB machine) = %s, over sbx's 75%% of %d", gib, Format(limit), total)
		}
	}
}

func TestPick(t *testing.T) {
	total := reported(32) // 4g..16g
	for _, tt := range []struct {
		want    string
		options []string
		idx     int
		over    bool
	}{
		{"12g", []string{"4g", "8g", "12g", "16g"}, 2, false},
		{"12G", []string{"4g", "8g", "12g", "16g"}, 2, false},
		{"6g", []string{"4g", "6g", "8g", "12g", "16g"}, 1, false},
		{"2g", []string{"2g", "4g", "8g", "12g", "16g"}, 0, false},
		{"24g", []string{"4g", "8g", "12g", "16g"}, 3, true},
		{"", []string{"4g", "8g", "12g", "16g"}, 3, false},
		{"lots", []string{"4g", "8g", "12g", "16g"}, 3, false},
	} {
		options, idx, over := Pick(total, tt.want)
		if !reflect.DeepEqual(options, tt.options) || idx != tt.idx || over != tt.over {
			t.Errorf("Pick(%q) = %v, %d, %v; want %v, %d, %v", tt.want, options, idx, over, tt.options, tt.idx, tt.over)
		}
	}
}

func TestPickOnASmallHost(t *testing.T) {
	options, idx, over := Pick(reported(16), "12g")
	if !reflect.DeepEqual(options, []string{"4g", "8g"}) || idx != 1 || !over {
		t.Errorf("Pick(12g) on 16 GB = %v, %d, %v; want [4g 8g], 1, true", options, idx, over)
	}
}

func TestParse(t *testing.T) {
	for s, want := range map[string]uint64{
		"12g": 12 * GiB, "12G": 12 * GiB, "12gb": 12 * GiB, "12GiB": 12 * GiB,
		"512m": 512 << 20, "1t": 1 << 40, "2048k": 2 << 20, "1024": 1024,
	} {
		if got, err := Parse(s); err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"", "g", "lots", "-4g", "0g", "1.5g"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded", s)
		}
	}
}

func TestFormat(t *testing.T) {
	for size, want := range map[uint64]string{8 * GiB: "8g", 512 << 20: "512m", GiB + GiB/2: "1536m"} {
		if got := Format(size); got != want {
			t.Errorf("Format(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestTotal(t *testing.T) {
	total, err := Total()
	if err != nil {
		t.Skipf("can't read this host's memory: %v", err)
	}
	if total < 256<<20 {
		t.Errorf("Total = %d bytes — implausibly small", total)
	}
}
