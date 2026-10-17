// Package policy reads and changes a sandbox's network and filesystem
// policy through `sbx policy`, for the companion page.
//
// What's read is the JSON sbx prints, in the shape sbx v0.48 prints it, and
// nothing else: output vibe can't use is an error the page shows, never a
// guess that shows less than sbx knows. Each entry is also kept as sbx
// printed it, for the page to show in full.
package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Runner runs `sbx` with args and returns its stdout. Tests swap it for a
// fake; Exec is the real one.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// ErrNotSignedIn is returned when sbx says it needs `sbx login` first,
// which it does for every policy command.
var ErrNotSignedIn = errors.New("sbx is not signed in to Docker — run 'sbx login'")

// Exec is the Runner that runs the real sbx.
func Exec(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sbx", args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if strings.Contains(msg, "not signed in") {
			return nil, ErrNotSignedIn
		}
		return nil, fmt.Errorf("sbx %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return out.Bytes(), nil
}

// Client runs policy commands for one sandbox.
type Client struct {
	Sandbox string
	Run     Runner
	// Timeout bounds each sbx call; zero means ten seconds.
	Timeout time.Duration
}

// Decision is what sbx's proxy did with a request, or what a rule says to do.
const (
	Allowed = "allowed"
	Blocked = "blocked"
	// Unblocked is a request that was blocked, whose host a rule now allows:
	// the request can't be re-issued from here, but doing it again in the
	// sandbox would get through.
	Unblocked = "unblocked"
	Unknown   = ""
)

// LogEntry is one host the sandbox has tried to reach.
type LogEntry struct {
	Host      string `json:"host"`
	Decision  string `json:"decision"`
	Hits      int64  `json:"hits"`
	LastSeen  string `json:"lastSeen,omitempty"`
	Rule      string `json:"rule,omitempty"`
	ProxyType string `json:"proxyType,omitempty"`
	Reason    string `json:"reason,omitempty"`
	// Raw is the entry as sbx printed it.
	Raw json.RawMessage `json:"raw"`
}

// Rule is one policy rule that applies to the sandbox.
type Rule struct {
	// ID is what `sbx policy rm network --id` takes; empty when sbx didn't
	// give one, and then the rule can't be removed from here.
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Resources []string `json:"resources"`
	Decision  string   `json:"decision"`
	Protocol  string   `json:"protocol,omitempty"`
	// Source is where sbx says the rule comes from (its origin: local, or
	// scoped to a sandbox), and Scope what it applies to (global, or
	// sandbox:<name>).
	Source string `json:"source,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Status string `json:"status,omitempty"`
	// Removable is true for a rule the page offers to remove: one sbx says
	// is editable. The removal is always scoped to the sandbox, so a global
	// rule is never touched from the page: sbx refuses, and says so.
	Removable bool            `json:"removable"`
	Raw       json.RawMessage `json:"raw"`
}

// Log is what `sbx policy log` knows about the sandbox.
type Log struct {
	Entries []LogEntry `json:"entries"`
	// Unresolved says why the entries couldn't be checked against the
	// rules, so that none is marked Unblocked; "" when they were.
	Unresolved string `json:"unresolved,omitempty"`
}

// Rules is a list of rules.
type Rules struct {
	Rules []Rule `json:"rules"`
}

func (c *Client) run(args ...string) ([]byte, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	run := c.Run
	if run == nil {
		run = Exec
	}
	return run(ctx, args...)
}

// Log reads the sandbox's network log.
func (c *Client) Log() (Log, error) {
	if err := ValidSandbox(c.Sandbox); err != nil {
		return Log{}, err
	}
	out, err := c.run("policy", "log", c.Sandbox, "--json", "--type", "network")
	if err != nil {
		return Log{}, err
	}
	return ParseLog(out)
}

// ResolvedLog is Log with each blocked host a current rule allows marked
// Unblocked. If the rules can't be read the log is returned as sbx gave it
// — a host that shows as blocked is still true to what happened — saying
// why in Unresolved.
func (c *Client) ResolvedLog() (Log, error) {
	log, err := c.Log()
	if err != nil {
		return log, err
	}
	rules, err := c.NetworkRules()
	if err != nil {
		log.Unresolved = "The rules couldn't be read, so blocked hosts a rule now allows aren't marked: " + err.Error()
		return log, nil
	}
	return Resolve(log, rules), nil
}

// Resolve marks as Unblocked each blocked entry whose host is allowed by a
// rule and denied by none: a deny wins over an allow.
func Resolve(log Log, rules Rules) Log {
	out := log
	out.Entries = make([]LogEntry, len(log.Entries))
	copy(out.Entries, log.Entries)
	for i, e := range out.Entries {
		if e.Decision != Blocked {
			continue
		}
		allowed, denied := false, false
		for _, r := range rules.Rules {
			st := strings.ToLower(r.Status)
			if strings.Contains(st, "inactive") || strings.Contains(st, "suppress") {
				continue
			}
			for _, res := range r.Resources {
				if !hostMatches(res, e.Host) {
					continue
				}
				if r.Decision == Blocked {
					denied = true
				} else if r.Decision == Allowed {
					allowed = true
				}
			}
		}
		if allowed && !denied {
			out.Entries[i].Decision = Unblocked
		}
	}
	return out
}

// hostMatches says whether a rule's resource covers host. Ports are ignored;
// "*" stands for anything but a dot and "**" for anything at all.
func hostMatches(resource, host string) bool {
	resource, host = stripPort(strings.ToLower(strings.TrimSpace(resource))), stripPort(strings.ToLower(strings.TrimSpace(host)))
	if resource == "" || host == "" {
		return false
	}
	var re strings.Builder
	re.WriteString("^")
	for i := 0; i < len(resource); i++ {
		switch {
		case strings.HasPrefix(resource[i:], "**"):
			re.WriteString(".*")
			i++
		case resource[i] == '*':
			re.WriteString("[^.]*")
		default:
			re.WriteString(regexp.QuoteMeta(string(resource[i])))
		}
	}
	re.WriteString("$")
	ok, err := regexp.MatchString(re.String(), host)
	return err == nil && ok
}

func stripPort(h string) string {
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i+1:], "]") && strings.Count(h, ":") == 1 {
		return h[:i]
	}
	return h
}

// NetworkRules lists the network rules that apply to the sandbox.
func (c *Client) NetworkRules() (Rules, error) {
	return c.rules("network")
}

// FilesystemRules lists the filesystem rules that apply to the sandbox.
func (c *Client) FilesystemRules() (Rules, error) {
	return c.rules("filesystem")
}

func (c *Client) rules(kind string) (Rules, error) {
	if err := ValidSandbox(c.Sandbox); err != nil {
		return Rules{}, err
	}
	out, err := c.run("policy", "ls", c.Sandbox, "--json", "--wide", "--type", kind)
	if err != nil {
		return Rules{}, err
	}
	return ParseRules(out)
}

// AllowCommand and DenyCommand are the sbx arguments that add a rule for
// this sandbox alone — never a global one: widening or narrowing what every
// sandbox may do is the CLI's to decide.
func (c *Client) AllowCommand(host string) []string {
	return []string{"policy", "allow", "network", "--sandbox", c.Sandbox, host}
}

func (c *Client) DenyCommand(host string) []string {
	return []string{"policy", "deny", "network", "--sandbox", c.Sandbox, host}
}

// RemoveCommand is the sbx arguments that remove one of this sandbox's own
// rules. --force skips sbx's own confirmation, which it can't ask for with
// no terminal (and fails without): the page has asked already.
func (c *Client) RemoveCommand(id string) []string {
	return []string{"policy", "rm", "network", "--sandbox", c.Sandbox, "--id", id, "--force"}
}

// Allow lets the sandbox reach host.
func (c *Client) Allow(host string) error {
	if err := c.check(host); err != nil {
		return err
	}
	_, err := c.run(c.AllowCommand(host)...)
	return err
}

// Deny stops the sandbox reaching host.
func (c *Client) Deny(host string) error {
	if err := c.check(host); err != nil {
		return err
	}
	_, err := c.run(c.DenyCommand(host)...)
	return err
}

// Remove removes one of the sandbox's own network rules.
func (c *Client) Remove(id string) error {
	if err := ValidSandbox(c.Sandbox); err != nil {
		return err
	}
	if err := ValidRuleID(id); err != nil {
		return err
	}
	_, err := c.run(c.RemoveCommand(id)...)
	return err
}

func (c *Client) check(host string) error {
	if err := ValidSandbox(c.Sandbox); err != nil {
		return err
	}
	return ValidHost(host)
}

var (
	sandboxName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9*?\[\]!:._/-]+$`)
	ruleID      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
)

// ValidSandbox says whether name is safe to hand to sbx as a sandbox name.
func ValidSandbox(name string) error {
	if !sandboxName.MatchString(name) {
		return fmt.Errorf("not a usable sandbox name: %q", name)
	}
	return nil
}

// ValidRuleID says whether id is safe to hand to `sbx policy rm --id`.
func ValidRuleID(id string) error {
	if len(id) > 128 || !ruleID.MatchString(id) {
		return fmt.Errorf("not a usable rule id: %q", id)
	}
	return nil
}

// ValidHost says whether host is a single host, domain pattern, address or
// CIDR that is safe to hand to `sbx policy allow/deny network`. It is
// stricter than sbx: no lists, nothing that could read as a flag, and no
// pattern made only of wildcards, since "allow everything" or "block
// everything" is a decision for the CLI, not a button on a page.
func ValidHost(host string) error {
	if host == "" || len(host) > 300 || !hostPattern.MatchString(host) || host[0] == '-' {
		return fmt.Errorf("not a usable host: %q", host)
	}
	if strings.Trim(host, "*?.") == "" {
		return fmt.Errorf("refusing a rule that matches every host: %q — use the sbx CLI for that", host)
	}
	return nil
}

// --- parsing ----------------------------------------------------------

// unusable is the error for output from sbx that vibe can't read.
func unusable(command string, data []byte, why string) error {
	return fmt.Errorf("couldn't use what sbx %s printed (%s) — vibe may need updating for this sbx:\n%s", command, why, bytes.TrimSpace(data))
}

// ParseLog reads `sbx policy log --json`: the hosts the sandbox has tried
// to reach, in a list for each decision — "blocked" and "allowed" from sbx
// v0.48, "blocked_hosts" and "allowed_hosts" before it. Nothing at all, or
// null, is an empty log.
func ParseLog(data []byte) (Log, error) {
	const command = "policy log --json"
	log := Log{Entries: []LogEntry{}}
	if isNothing(data) {
		return log, nil
	}
	doc, err := topLevel(command, data, "blocked", "allowed", "blocked_hosts", "allowed_hosts")
	if err != nil {
		return Log{}, err
	}
	for _, list := range []struct{ key, decision string }{
		{"blocked", Blocked}, {"allowed", Allowed}, {"blocked_hosts", Blocked}, {"allowed_hosts", Allowed},
	} {
		entries, err := listUnder(command, data, doc, list.key)
		if err != nil {
			return Log{}, err
		}
		for _, raw := range entries {
			var e struct {
				Host       string `json:"host"`
				ProxyType  string `json:"proxy_type"`
				Rule       string `json:"rule"`
				LastSeen   string `json:"last_seen"`
				Count      int64  `json:"count"`
				CountSince int64  `json:"count_since"`
				Reason     string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &e); err != nil {
				return Log{}, unusable(command, data, err.Error())
			}
			if e.Host == "" {
				return Log{}, unusable(command, data, "an entry has no host")
			}
			log.Entries = append(log.Entries, LogEntry{
				Host:      e.Host,
				Decision:  list.decision,
				Hits:      e.Count + e.CountSince, // an sbx prints one or the other
				LastSeen:  utc(e.LastSeen),
				Rule:      e.Rule,
				ProxyType: e.ProxyType,
				Reason:    e.Reason,
				Raw:       raw,
			})
		}
	}
	sort.SliceStable(log.Entries, func(i, j int) bool {
		return log.Entries[i].LastSeen > log.Entries[j].LastSeen
	})
	return log, nil
}

// ParseRules reads `sbx policy ls --json`. Nothing at all, or null, is no
// rules.
func ParseRules(data []byte) (Rules, error) {
	const command = "policy ls --json"
	rules := Rules{Rules: []Rule{}}
	if isNothing(data) {
		return rules, nil
	}
	doc, err := topLevel(command, data, "rules")
	if err != nil {
		return Rules{}, err
	}
	list, err := listUnder(command, data, doc, "rules")
	if err != nil {
		return Rules{}, err
	}
	for _, raw := range list {
		var r struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Scope     string   `json:"scope"`
			Decision  string   `json:"decision"`
			Resources []string `json:"resources"`
			Origin    string   `json:"origin"`
			Status    string   `json:"status"`
			Editable  bool     `json:"editable"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return Rules{}, unusable(command, data, err.Error())
		}
		if len(r.Resources) == 0 {
			return Rules{}, unusable(command, data, "a rule has no resources")
		}
		var decision string
		switch r.Decision {
		case "allow":
			decision = Allowed
		case "deny":
			decision = Blocked
		default:
			return Rules{}, unusable(command, data, fmt.Sprintf("a rule's decision is %q, not allow or deny", r.Decision))
		}
		rules.Rules = append(rules.Rules, Rule{
			ID:        r.ID,
			Name:      r.Name,
			Resources: r.Resources,
			Decision:  decision,
			Source:    r.Origin,
			Scope:     r.Scope,
			Status:    r.Status,
			Removable: r.Editable && r.ID != "",
			Raw:       raw,
		})
	}
	// The sandbox's own rules first, so the page doesn't bury them under
	// every global one.
	sort.SliceStable(rules.Rules, func(i, j int) bool {
		return rules.Rules[i].Scope != "global" && rules.Rules[j].Scope == "global"
	})
	return rules, nil
}

// topLevel is the object sbx printed, which has to hold at least one of
// keys: an object with none of them isn't one vibe knows how to read.
func topLevel(command string, data []byte, keys ...string) (map[string]json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, unusable(command, data, err.Error())
	}
	for _, k := range keys {
		if _, ok := doc[k]; ok {
			return doc, nil
		}
	}
	return nil, unusable(command, data, "it has no "+strings.Join(keys, " or "))
}

// listUnder is the list under key in doc: none when the key is missing or
// null.
func listUnder(command string, data []byte, doc map[string]json.RawMessage, key string) ([]json.RawMessage, error) {
	raw, ok := doc[key]
	if !ok || isNothing(raw) {
		return nil, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, unusable(command, data, key+" isn't a list")
	}
	return list, nil
}

func isNothing(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) == 0 || string(data) == "null"
}

// utc is a time sbx printed in RFC 3339, in UTC, so that times sort as
// text; one it can't read is passed on as printed.
func utc(t string) string {
	if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return t
}
