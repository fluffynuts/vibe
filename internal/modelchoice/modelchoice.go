// Package modelchoice is what the companion page may ask of an agent about
// its model and effort: which there are (read from the agent itself, so the
// list is as live as the agent) and whether a choice is safe to put on a
// command line.
package modelchoice

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// maxModel bounds a model name: the longest real ones are well under it.
const maxModel = 100

// safeModel is what a model name may be made of. The name goes into a shell
// command, so this is checked before it is stored and again before it is
// used, and it is quoted as well: this is for refusing nonsense early, with
// a message, not the only guard.
var safeModel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]*$`)

// CheckModel says why model can't be used, or nil. Empty is fine: it means
// the agent's own default.
func CheckModel(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > maxModel || !safeModel.MatchString(model) {
		return fmt.Errorf("%q isn't a model name: it can have letters, digits and . _ : [ ] -, must start with a letter or digit, and be at most %d characters", model, maxModel)
	}
	return nil
}

// CheckEffort says why effort isn't one of allowed, or nil. Empty is fine:
// it means the agent's own default.
func CheckEffort(effort string, allowed []string) error {
	if effort == "" || slices.Contains(allowed, effort) {
		return nil
	}
	return fmt.Errorf("%q isn't an effort the agent takes: %s", effort, strings.Join(allowed, ", "))
}

// Models is what the agent says of its models.
type Models struct {
	// Plain are the plain names of models (an alias for the latest of a
	// family), to offer at once.
	Plain []string
	// Other are the rest of what the agent lists, to offer on request.
	Other []string
	// Current is the agent's own name for the model it would use with no
	// choice made, as it says it ("Opus 5.5 (default)"); empty if it didn't.
	Current string
}

// notAFamily are listed names that aren't one model's: "default" is what no
// choice already means, and the others pick by a rule, not a model.
var notAFamily = map[string]bool{"best": true, "opusplan": true}

// listedDefault is the name the agent gives to its default, which is what
// no choice is, so it isn't offered as another.
const listedDefault = "default"

var (
	currentModel = regexp.MustCompile("Current model: `([^`]+)`")
	availableRun = regexp.MustCompile(`Available: ([^\n]*)`)
	plainName    = regexp.MustCompile(`^[a-z]+$`)
)

// ParseModels reads what a "/model" prompt answers: "Current model: `X`"
// and "Available: a, b, c, or a full model ID." Anything it can't find the
// models in is an error, so a changed answer is said, not read as an empty
// list.
func ParseModels(answer string) (Models, error) {
	var out Models
	if m := currentModel.FindStringSubmatch(answer); m != nil {
		out.Current = m[1]
	}
	found := availableRun.FindStringSubmatch(answer)
	if found == nil {
		return Models{}, fmt.Errorf("the agent's answer to /model doesn't list the models it has: %q", firstLine(answer))
	}
	for _, name := range strings.Split(strings.TrimSuffix(strings.TrimSpace(found[1]), "."), ",") {
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, "or ") {
			break
		}
		switch {
		case name == "" || name == listedDefault:
		case plainName.MatchString(name) && !notAFamily[name]:
			out.Plain = append(out.Plain, name)
		default:
			if err := CheckModel(name); err != nil {
				return Models{}, fmt.Errorf("the agent lists a model that can't be used: %w", err)
			}
			out.Other = append(out.Other, name)
		}
	}
	if len(out.Plain)+len(out.Other) == 0 {
		return Models{}, fmt.Errorf("the agent's answer to /model lists no models: %q", firstLine(answer))
	}
	return out, nil
}

// effortFlag finds the --effort flag in the agent's --help, with the list of
// values it takes in the parentheses its description ends with.
var effortFlag = regexp.MustCompile(`--effort\s+<[^>]+>[^(]*\(([a-z][a-z, ]*)\)`)

// ParseEfforts reads the values --effort takes from the agent's --help.
func ParseEfforts(help string) ([]string, error) {
	m := effortFlag.FindStringSubmatch(help)
	if m == nil {
		return nil, fmt.Errorf("the agent's --help doesn't list the values --effort takes")
	}
	var out []string
	for _, v := range strings.Split(m[1], ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 120 {
		line = line[:120] + "…"
	}
	return line
}
