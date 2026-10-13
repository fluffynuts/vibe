package main

import (
	"errors"
	"testing"

	"vibe/internal/hostmem"
)

func TestPickMemoryWithoutAsking(t *testing.T) {
	onHost(t, 32)
	for want, got := range map[string]string{"12g": "12g", "6g": "6g", "24g": "16g", "lots": "16g"} {
		if m := pickMemory(want, false, ""); m != got {
			t.Errorf("pickMemory(%q) on 32 GB = %q, want %q", want, m, got)
		}
	}
	hostmem.Total = func() (uint64, error) { return 0, errors.New("no idea") }
	if m := pickMemory("24g", false, ""); m != "24g" {
		t.Errorf("pickMemory on an unknown machine = %q, want it kept", m)
	}
}
