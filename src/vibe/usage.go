package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"vibe/internal/companion"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
	sandboxload "vibe/internal/usage"
)

// readSandboxLoad reads the sandbox's /proc/stat and /proc/meminfo together
// in one exec, as plain argv (sbx exec may join a shell script's words). A
// variable for tests. Whether it's running is asked first (sandboxRunning): a
// stopped one is never woken to be measured.
var readSandboxLoad = func(ctx context.Context, name string) (string, error) {
	return sbxrun.ExecStdout(ctx, name, "cat", "/proc/stat", "/proc/meminfo")
}

// usageFor reads how busy sandbox name is, for the companion page's CPU and
// memory bars, with what it was given from its record (the labels say "8g",
// not what the kernel reports having left to use).
func usageFor(vibeHome, name string) func(ctx context.Context) (companion.UsageReading, error) {
	var mu sync.Mutex
	var sampler sandboxload.Sampler
	return func(ctx context.Context) (companion.UsageReading, error) {
		running, ok := sandboxRunning(name)
		if !ok {
			return companion.UsageReading{}, fmt.Errorf("couldn't ask sbx whether sandbox '%s' is running", name)
		}
		if !running {
			// Its counters start again with it: a figure must never span the stop.
			mu.Lock()
			sampler = sandboxload.Sampler{}
			mu.Unlock()
			return companion.UsageReading{}, companion.ErrNotRunning
		}
		out, err := readSandboxLoad(ctx, name)
		if err != nil {
			return companion.UsageReading{}, fmt.Errorf("couldn't read the sandbox's load: %w", err)
		}
		mu.Lock()
		r, err := sampler.Add(out, time.Now())
		mu.Unlock()
		if err != nil {
			return companion.UsageReading{}, err
		}
		reading := companion.UsageReading{Cores: r.Cores, MemUsed: r.MemUsed, MemTotal: r.MemTotal, At: r.At}
		if r.HaveCPU {
			reading.CPU = &r.CPU
		}
		if inst, found, err := state.Load(vibeHome, name); err == nil && found {
			reading.CPUSetting, reading.MemSetting = inst.CPUs, inst.Memory
		}
		return reading, nil
	}
}
