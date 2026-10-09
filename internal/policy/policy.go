// Package policy reads and changes a sandbox's network and filesystem
// policy through `sbx policy`, for the companion page.
//
// sbx's JSON isn't a documented contract, and it is still moving, so what
// comes back is read tolerantly: each field is looked for under the names
// sbx is known (or likely) to use, and the entry as sbx printed it is kept
// alongside, so the page can show it when a field didn't map. An entry vibe
// can't make a host out of is dropped, but counted, so the page can say so
// rather than quietly showing less than sbx knows.
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
	// Source says where the rule comes from (local, org, kit...), and Scope
	// what it applies to (global, or this sandbox), when sbx says.
	Source string `json:"source,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Status string `json:"status,omitempty"`
	// Removable is true for a rule the page offers to remove: one sbx gave
	// an ID for and doesn't say comes from the org or a kit. The removal is
	// always scoped to the sandbox, so a global rule is never touched from
	// the page: sbx refuses, and says so.
	Removable bool            `json:"removable"`
	Raw       json.RawMessage `json:"raw"`
}

// Log is what `sbx policy log` knows about the sandbox.
type Log struct {
	Entries []LogEntry `json:"entries"`
	// Skipped counts entries in sbx's output that had no host vibe could
	// read.
	Skipped int `json:"skipped"`
}

// Rules is a list of rules, with the same Skipped count as Log.
type Rules struct {
	Rules   []Rule `json:"rules"`
	Skipped int    `json:"skipped"`
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
// Unblocked. If the rules can't be read the log is returned as sbx gave it:
// a host that shows as blocked is still true to what happened.
func (c *Client) ResolvedLog() (Log, error) {
	log, err := c.Log()
	if err != nil {
		return log, err
	}
	rules, err := c.NetworkRules()
	if err != nil {
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
// rules.
func (c *Client) RemoveCommand(id string) []string {
	return []string{"policy", "rm", "network", "--sandbox", c.Sandbox, "--id", id}
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

// ParseLog reads `sbx policy log --json`.
func ParseLog(data []byte) (Log, error) {
	objs, err := objects(data, "entries", "logs", "log", "items", "data")
	if err != nil {
		return Log{}, err
	}
	log := Log{Entries: []LogEntry{}}
	for _, o := range objs {
		host := str(o.m, "host", "hostname", "resource", "target", "address", "domain_name")
		if host == "" {
			log.Skipped++
			continue
		}
		e := LogEntry{
			Host:      host,
			Decision:  decisionOr(o),
			Hits:      num(o.m, "count", "count_since", "hits", "requests", "request_count", "total"),
			LastSeen:  when(o.m, "last_seen", "lastSeen", "last_seen_at", "lastSeenAt", "timestamp", "time"),
			Rule:      str(o.m, "rule", "rule_name", "ruleName"),
			ProxyType: str(o.m, "proxy_type", "proxyType", "proxy"),
			Reason:    str(o.m, "reason", "decision_reason", "decisionReason"),
			Raw:       o.raw,
		}
		log.Entries = append(log.Entries, e)
	}
	sort.SliceStable(log.Entries, func(i, j int) bool {
		return log.Entries[i].LastSeen > log.Entries[j].LastSeen
	})
	return log, nil
}

// ParseRules reads `sbx policy ls --json`.
func ParseRules(data []byte) (Rules, error) {
	objs, err := objects(data, "rules", "policies", "items", "data")
	if err != nil {
		return Rules{}, err
	}
	rules := Rules{Rules: []Rule{}}
	for _, o := range objs {
		resources := strs(o.m, "resources", "resource", "hosts", "host", "pattern", "paths", "path")
		if len(resources) == 0 {
			rules.Skipped++
			continue
		}
		r := Rule{
			ID:        str(o.m, "rule_id", "ruleId", "id"),
			Name:      str(o.m, "rule", "rule_name", "ruleName", "name"),
			Resources: resources,
			Decision:  decisionOr(o),
			Protocol:  str(o.m, "protocol"),
			Source:    str(o.m, "source", "policy", "policy_name", "policyName"),
			Scope:     str(o.m, "scope", "sandbox", "applies_to", "target"),
			Status:    str(o.m, "status", "state"),
			Raw:       o.raw,
		}
		r.Removable = r.ID != "" && (r.Source == "" || strings.EqualFold(r.Source, "local"))
		rules.Rules = append(rules.Rules, r)
	}
	return rules, nil
}

type object struct {
	m   map[string]interface{}
	raw json.RawMessage
	// key is the name of the list the record was found in
	// ("blocked_hosts"...), which is all some sbx output says about a
	// decision.
	key string
}

// objects finds the records in a document sbx printed: the objects, at any
// depth, that look like a record (they hold a scalar of some kind), without
// descending into one that has been taken. A top-level array is the common
// shape; an object wrapping one under a key in preferred, or under any key,
// is the other.
func objects(data []byte, preferred ...string) ([]object, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("sbx printed something that isn't JSON: %w", err)
	}
	var found []object
	var walk func(node interface{}, key string, depth int)
	walk = func(node interface{}, key string, depth int) {
		switch n := node.(type) {
		case []interface{}:
			for _, item := range n {
				walk(item, key, depth+1)
			}
		case map[string]interface{}:
			if isRecord(n) {
				raw, _ := json.Marshal(n)
				found = append(found, object{m: n, raw: raw, key: key})
				return
			}
			for _, k := range preferred {
				if v, ok := n[k]; ok {
					walk(v, k, depth+1)
				}
			}
			keys := make([]string, 0, len(n))
			for k := range n {
				if !contains(preferred, k) {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(n[k], k, depth+1)
			}
		}
	}
	walk(doc, "", 0)
	return found, nil
}

// isRecord says whether m is a record rather than a wrapper: a wrapper
// holds a list of objects (a policy and its rules, say), a record holds
// values — however many names or addresses are in a list of strings.
func isRecord(m map[string]interface{}) bool {
	scalars := 0
	for _, v := range m {
		switch v := v.(type) {
		case string, float64, bool:
			scalars++
		case []interface{}:
			for _, item := range v {
				if _, ok := item.(map[string]interface{}); ok {
					return false
				}
			}
		}
	}
	return scalars >= 2
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func str(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return fmt.Sprintf("%g", v)
		}
	}
	return ""
}

func strs(m map[string]interface{}, keys ...string) []string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return []string{v}
			}
		case []interface{}:
			var out []string
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func num(m map[string]interface{}, keys ...string) int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(v)
		case string:
			var n int64
			if _, err := fmt.Sscan(v, &n); err == nil {
				return n
			}
		}
	}
	return 0
}

// decisionOr is the record's own decision or, when it has none, what the
// list it sits in says: sbx groups the log into blocked_hosts and
// allowed_hosts and leaves the decision off the entries.
func decisionOr(o object) string {
	if d := decision(o.m); d != Unknown {
		return d
	}
	k := strings.ToLower(o.key)
	switch {
	case strings.Contains(k, "block"), strings.Contains(k, "deny"), strings.Contains(k, "denied"):
		return Blocked
	case strings.Contains(k, "allow"):
		return Allowed
	}
	return Unknown
}

// decision reads allowed/blocked from a string ("allow", "POLICY_DECISION_
// BLOCKED", "deny"...) or from a boolean field, whichever the entry has.
func decision(m map[string]interface{}) string {
	for _, k := range []string{"decision", "status", "action", "effect", "result"} {
		if s, ok := m[k].(string); ok {
			switch d := strings.ToLower(s); {
			case strings.Contains(d, "block"), strings.Contains(d, "deny"), strings.Contains(d, "denied"), strings.Contains(d, "reject"):
				return Blocked
			case strings.Contains(d, "allow"), strings.Contains(d, "permit"):
				return Allowed
			}
		}
	}
	if b, ok := m["blocked"].(bool); ok && b {
		return Blocked
	}
	if b, ok := m["allowed"].(bool); ok {
		if b {
			return Allowed
		}
		return Blocked
	}
	return Unknown
}

// when reads a time sbx printed — RFC 3339, a Unix time in seconds or
// milliseconds, or protobuf's {seconds, nanos} — as RFC 3339 in UTC, so
// that times sort as text. A time in a shape it doesn't know is passed on
// as printed.
func when(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v == "" {
				continue
			}
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
				if t, err := time.Parse(layout, v); err == nil {
					return t.UTC().Format(time.RFC3339)
				}
			}
			return v
		case float64:
			if v > 1e12 {
				return time.UnixMilli(int64(v)).UTC().Format(time.RFC3339)
			}
			return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
		case map[string]interface{}:
			if secs, ok := v["seconds"].(float64); ok {
				return time.Unix(int64(secs), 0).UTC().Format(time.RFC3339)
			}
		}
	}
	return ""
}
