package activity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KeepDays is how many days of the log and summaries are kept, today among them: a week past what the page steps back to,
// so nothing the user can still look at goes.
const KeepDays = 14

// FirstKept is the first day kept on now's day.
func FirstKept(now time.Time) time.Time {
	return midnightOf(now).AddDate(0, 0, -(KeepDays - 1))
}

// Prune removes the log's days before first's. Only the day folders go: whatever else is kept beside them stays.
func (l Log) Prune(first time.Time) error {
	entries, err := os.ReadDir(l.Dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	keepFrom := first.Format(time.DateOnly)
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !isDate(e.Name()) || e.Name() >= keepFrom {
			continue
		}
		errs = append(errs, os.RemoveAll(filepath.Join(l.Dir, e.Name())))
	}
	return errors.Join(errs...)
}

func isDate(name string) bool {
	_, err := time.Parse(time.DateOnly, name)
	return err == nil
}

// Prune removes the summaries made before first: they summarise days pruned from the log, or earlier.
func (s *Summariser) Prune(first time.Time) error {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if info.ModTime().Before(first) {
			if err := os.Remove(filepath.Join(s.Dir, e.Name())); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
