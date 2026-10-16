package review

import (
	"bytes"
	"compress/zlib"
	"context"
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
