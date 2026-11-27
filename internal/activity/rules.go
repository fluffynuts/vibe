package activity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"vibe/internal/fscopy"
)

// RulesFile is one of the summarising rules' files, for the page to edit: the day's or the week's.
type RulesFile struct {
	Which   string `json:"which"`
	Path    string `json:"path"`
	Text    string `json:"text"`
	Default string `json:"default"`
}

// ErrRulesChanged is a save refused because the file changed since the text being saved was read from it (by hand, or from
// another page): saving would lose that change.
var ErrRulesChanged = errors.New("the rules were changed since they were loaded")

// RulesFiles are the day's and the week's rules as they are now, each written from its default the first time.
func (s *Summariser) RulesFiles() []RulesFile {
	var out []RulesFile
	for _, which := range []string{"day", "week"} {
		p := s.periodOf(which)
		out = append(out, RulesFile{Which: which, Path: p.rules, Text: s.rules(p), Default: p.defaults})
	}
	return out
}

// SaveRules keeps text as which's rules ("day" or "week"), unless the file no longer holds base, the text it was read as. A
// summary made under other rules is made again when next looked at: the rules are part of its key.
func (s *Summariser) SaveRules(which, text, base string) (RulesFile, error) {
	if which != "day" && which != "week" {
		return RulesFile{}, fmt.Errorf("%q isn't the day's or the week's rules", which)
	}
	p := s.periodOf(which)
	if s.rules(p) != base {
		return RulesFile{}, ErrRulesChanged
	}
	if err := os.MkdirAll(filepath.Dir(p.rules), 0o700); err != nil {
		return RulesFile{}, err
	}
	if err := fscopy.WriteWholeMode(p.rules, []byte(text), 0o644); err != nil {
		return RulesFile{}, err
	}
	return RulesFile{Which: which, Path: p.rules, Text: text, Default: p.defaults}, nil
}

func (s *Summariser) periodOf(which string) period {
	if which == "week" {
		return s.week()
	}
	return s.day()
}
