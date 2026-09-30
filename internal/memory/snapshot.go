package memory

import (
	"fmt"
	"os"
	"path/filepath"

	"vibe/internal/fscopy"
)

// Snapshot is a copy of a memory store taken before a backup writes into
// it, so an interrupted backup — which may have overwritten some memories
// and not others — can be undone.
type Snapshot struct {
	store, copy string
}

// TakeSnapshot copies what store holds now to a directory beside it. A
// store that doesn't exist yet is snapshotted as empty.
func TakeSnapshot(store string) (*Snapshot, error) {
	parent := filepath.Dir(store)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", parent, err)
	}
	copyDir, err := os.MkdirTemp(parent, "."+filepath.Base(store)+".before-save-")
	if err != nil {
		return nil, fmt.Errorf("making room for a copy of %s: %w", store, err)
	}
	if _, err := os.Stat(store); err == nil {
		if err := fscopy.Tree(store, copyDir); err != nil {
			os.RemoveAll(copyDir)
			return nil, fmt.Errorf("copying %s: %w", store, err)
		}
	}
	return &Snapshot{store: store, copy: copyDir}, nil
}

// Restore puts store back as it was when the snapshot was taken, and
// discards the snapshot. The store directory itself is emptied and
// refilled rather than replaced: it is mounted into the sandbox, and a
// directory swapped in on the host would not be the one the sandbox sees.
func (s *Snapshot) Restore() error {
	if err := os.MkdirAll(s.store, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.store)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(s.store, e.Name())); err != nil {
			return err
		}
	}
	if err := fscopy.Tree(s.copy, s.store); err != nil {
		return fmt.Errorf("%w — the original memories are still in %s", err, s.copy)
	}
	s.Discard()
	return nil
}

// Discard throws the snapshot away, once the backup it guarded is done
// with.
func (s *Snapshot) Discard() {
	os.RemoveAll(s.copy)
}
