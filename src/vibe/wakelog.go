package main

import (
	"os"
	"path/filepath"

	"vibe/internal/sbxrun"
)

// maxWakeLog is how big the wake log may grow before it starts again.
const maxWakeLog = 256 << 10

func wakeLogFile() string { return filepath.Join(vibeHomeDir(), "sbx-wake.log") }

// keepWakeLog has what sbx prints when a sandbox is woken, and fails or is slow, kept in ~/.vibe/sbx-wake.log: what sbx does
// for a sandbox it can't wake (signed out, say) is only learnt from what it said.
func keepWakeLog() {
	sbxrun.WakeLog = func(entry string) { appendWakeNote(wakeLogFile(), entry, maxWakeLog) }
}

// appendWakeNote adds entry to file, which is emptied first once it is past limit. A log that can't be written is not
// worth failing for: it is only for finding out.
func appendWakeNote(file, entry string, limit int64) {
	if info, err := os.Stat(file); err == nil && info.Size() > limit {
		os.Remove(file)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(entry + "\n\n")
}
