package review

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain lets this test binary stand in for vibe as the helper reading
// repositories (see isolated).
func TestMain(m *testing.M) {
	ServeHelperIfAsked()
	os.Exit(m.Run())
}

// A repository's objects are the sandbox's to write: one built to inflate
// past all reason (a zlib bomb) costs the helper reading it, not vibe.
func TestARepositoryBuiltToExhaustMemoryCostsOnlyTheHelper(t *testing.T) {
	t.Setenv("VIBE_REVIEW_HELPER_MEMORY", "128")
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	var bomb bytes.Buffer
	z, _ := zlib.NewWriterLevel(&bomb, zlib.BestCompression)
	z.Write([]byte("commit 10\x00"))
	zeros := make([]byte, 1<<20)
	for i := 0; i < 1024; i++ {
		z.Write(zeros)
	}
	z.Close()
	object := filepath.Join(dir, ".git", "objects", head[:2], head[2:])
	os.Chmod(object, 0o644)
	if err := os.WriteFile(object, bomb.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := StatusOf(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "memory") {
		t.Errorf("err = %v, want it to say the memory ran out", err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.HeapAlloc > before.HeapAlloc+64<<20 {
		t.Errorf("vibe's own heap grew %d MB", (after.HeapAlloc-before.HeapAlloc)>>20)
	}
}

// Reading a repository stops when its time is up.
func TestReadingStopsWhenItsTimeIsUp(t *testing.T) {
	dir := repo(t, map[string]string{"a.txt": "a\n"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := StatusOf(ctx, dir); err == nil || !strings.Contains(err.Error(), "took too long") {
		t.Errorf("err = %v", err)
	}
}

// The helper's answer is copied in as os/exec copies a pipe: with io.Copy,
// which takes a writer's ReadFrom over its Write when it has one.
func TestCappedKeepsNoMoreThanItsMaxHoweverItIsCopiedInto(t *testing.T) {
	c := &capped{max: 10}
	if _, err := io.Copy(c, struct{ io.Reader }{strings.NewReader(strings.Repeat("x", 100))}); err != nil {
		t.Fatal(err)
	}
	if !c.over || c.Len() != 10 {
		t.Errorf("kept %d bytes, over = %v; want 10, and over", c.Len(), c.over)
	}
}

// Past the address space the helper's given, Go's runtime itself can be
// what fails an allocation, before watchMemory sees the heap grow: it dies
// with a trace of every goroutine. What vibe says is still that the memory
// ran out, not the trace's last line.
func TestAHelperTheRuntimeKilledForMemorySaysSo(t *testing.T) {
	died := "fatal error: runtime: out of memory\n\n" +
		"runtime stack:\nruntime.throw({0x9f3a1b?, 0x0?})\n\t/usr/local/go/src/runtime/panic.go:1101 +0x48\n\n" +
		"goroutine 1 gp=0xc000002380 m=0 mp=0x14a0e40 [running]:\n" +
		"runtime.mallocgc(0x40000000, 0x0, 0x0)\n\t/opt/hostedtoolcache/go/1.26.0/x64/src/runtime/mgc.go:1695 +0x105\n"
	if got := helperDied(died); !strings.Contains(got, "more memory than vibe allows") {
		t.Errorf("helperDied = %q, want it to say the memory ran out", got)
	}
	// Any other panic is said by its first line, not its trace's last.
	if got := helperDied("panic: something else\n\ngoroutine 1 [running]:\nmain.main()\n\t/x.go:3 +0x1\n"); got != "panic: something else" {
		t.Errorf("helperDied = %q, want the panic", got)
	}
	// A plain last word (watchMemory's own, say) is said as it is.
	if got := helperDied("starting\nreading needed more memory\n"); got != "reading needed more memory" {
		t.Errorf("helperDied = %q", got)
	}
}
