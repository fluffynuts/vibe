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
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"vibe/internal/sbxfind"
)

// Runner runs `sbx` with args and returns its stdout. Tests swap it for a
// fake; Exec is the real one.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// ErrNotSignedIn is returned when sbx says it needs `sbx login` first,
// which it does for every policy command.
var ErrNotSignedIn = errors.New("sbx is not signed in to Docker — run 'sbx login'")

// Exec is the Runner that runs the real sbx.
func Exec(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, sbxfind.Path(), args...)
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
	// BlockedNow is an Allowed entry whose host a deny rule blocks now: the log only
	// records requests, so a host blocked from the page after its last request still
	// has an Allowed entry as its latest, and would say so.
	BlockedNow = "blocked-now"
	Unknown    = ""
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
	// AllowedBy is, for an Unblocked or an Allowed entry, the rules that allow
	// its host: what Block says it removes (those for exactly the host, which
	// sbx refuses a deny beside) and what it leaves.
	AllowedBy []RuleRef `json:"allowedBy,omitempty"`
	// BlockedBy is, for an entry that is Blocked or BlockedNow, the deny rules that cover its host: empty when only the default blocks it.
	BlockedBy []RuleRef `json:"blockedBy,omitempty"`
	// Superseded says a later entry of the same host decided otherwise (see markSuperseded): this one says what was so, not what is.
	Superseded bool `json:"superseded,omitempty"`
	// Raw is the entry as sbx printed it.
	Raw json.RawMessage `json:"raw"`
}

// RuleRef is a rule as a log entry names it: enough to say what it is and
// where it comes from, and to remove it.
type RuleRef struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Resources []string `json:"resources"`
	Source    string   `json:"source,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	// Removable is Rule.Removable.
	Removable bool `json:"removable"`
	// Wider says the rule allows more than the host it is named for (a
	// wildcard, or another host in the same rule), so removing it blocks
	// those too.
	Wider bool `json:"wider"`
	// Identical says one of the rule's resources is exactly the host, port
	// and all (case aside): if the rule is the sandbox's own, sbx refuses a
	// deny of the host beside it.
	Identical bool `json:"identical"`
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
	// Reason is what sbx says of a rule it made itself: the fallback that
	// denies what no policy decides says so here, and the rules people add
	// say nothing.
	Reason string `json:"reason,omitempty"`
	// Layer is the policy layer the rule is from (local, for sbx's own and
	// the user's); editable is whether sbx says it can be edited, nil when it
	// didn't say.
	Layer    string `json:"layer,omitempty"`
	editable *bool
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
		log.Entries = markSuperseded(log.Entries)
		return log, nil
	}
	return Resolve(log, rules), nil
}

// implicitFallback says whether r is sbx's own rule denying whatever no policy
// decides (from sbx v0.51 on): where a request ends up when no rule allows it,
// not a rule against any host, so it can't be what stops an allow. It matches
// every host, so taking it for a deny that stands would leave nothing ever
// unblocked; and taking a deny that stands for it would show a host as open
// that a rule is blocking, so what counts is narrow:
//   - its shape: a deny of "**" alone that sbx says can't be edited, on the
//     local layer. That is what it is, whatever words it explains itself in,
//     and a rule a user added (editable), an organisation's (another layer), or
//     one that doesn't say whether it can be edited, is not it;
//   - or its own account of itself: a reason that starts "Implicit fallback".
func (r Rule) implicitFallback() bool {
	if r.Decision != Blocked {
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Reason)), "implicit fallback") {
		return true
	}
	if r.editable == nil || *r.editable || r.Layer != "local" || len(r.Resources) == 0 {
		return false
	}
	for _, res := range r.Resources {
		if strings.TrimSpace(res) != "**" {
			return false
		}
	}
	return true
}

// active says whether a rule counts: not one that is off, and not sbx's
// own fallback (implicitFallback).
func (r Rule) active() bool {
	st := strings.ToLower(r.Status)
	return !strings.Contains(st, "inactive") && !strings.Contains(st, "suppress") && !r.implicitFallback()
}

// sameResource says a rule's resource is exactly host, port and all.
func sameResource(resource, host string) bool {
	return strings.EqualFold(strings.TrimSpace(resource), strings.TrimSpace(host))
}

func identicalTo(resources []string, host string) bool {
	for _, res := range resources {
		if sameResource(res, host) {
			return true
		}
	}
	return false
}

// Resolve marks as Unblocked each blocked entry whose host is allowed by a
// rule and denied by none: a deny wins over an allow. The fallback deny sbx
// lists (implicitFallback) is none: it matches every host. Each entry that
// is, or has become, allowed says which rules allow it (AllowedBy).
func Resolve(log Log, rules Rules) Log {
	out := log
	out.Entries = make([]LogEntry, len(log.Entries))
	copy(out.Entries, log.Entries)
	for i, e := range out.Entries {
		if e.Decision != Blocked && e.Decision != Allowed {
			continue
		}
		allowed, denied := false, false
		var allowedBy, blockedBy []RuleRef
		for _, r := range rules.Rules {
			if !r.active() {
				continue
			}
			listed := false
			for _, res := range r.Resources {
				if !hostMatches(res, e.Host) {
					continue
				}
				if r.Decision == Blocked {
					denied = true
					if !listed {
						listed = true
						blockedBy = append(blockedBy, RuleRef{ID: r.ID, Name: r.Name, Resources: r.Resources, Source: r.Source, Scope: r.Scope,
							Removable: r.Removable, Wider: reachesFurther(r.Resources, e.Host), Identical: identicalTo(r.Resources, e.Host)})
					}
				} else if r.Decision == Allowed {
					allowed = true
					if !listed {
						listed = true
						allowedBy = append(allowedBy, RuleRef{ID: r.ID, Name: r.Name, Resources: r.Resources, Source: r.Source, Scope: r.Scope,
							Removable: r.Removable, Wider: reachesFurther(r.Resources, e.Host), Identical: identicalTo(r.Resources, e.Host)})
					}
				}
			}
		}
		if allowed && !denied {
			if e.Decision == Blocked {
				out.Entries[i].Decision = Unblocked
			}
			out.Entries[i].AllowedBy = allowedBy
		} else if denied {
			if e.Decision == Allowed {
				out.Entries[i].Decision = BlockedNow
			}
			out.Entries[i].BlockedBy = blockedBy
		}
	}
	out.Entries = markSuperseded(out.Entries)
	return out
}

// markSuperseded marks, in a copy of entries, each entry of a host that is older than the host's latest entry and decides the
// other way: the log is a history with an entry for each (host, decision, rule text), so a host blocked and then allowed shows
// both, and the blocked one is old news. What the latest entry says is so; an entry that agrees with it is not superseded by one
// in between that was itself replaced. Allowed and Unblocked are the same decision here (the host gets through). Entries at the
// latest moment that disagree, or whose time is not known, settle nothing: nothing says which is later.
func markSuperseded(entries []LogEntry) []LogEntry {
	out := make([]LogEntry, len(entries))
	copy(out, entries)
	gets := func(e LogEntry) bool { return e.Decision != Blocked && e.Decision != BlockedNow }
	type latest struct {
		at           time.Time
		gets, blocks bool
	}
	hosts := map[string]*latest{}
	times := make([]time.Time, len(out))
	for i, e := range out {
		t, err := time.Parse(time.RFC3339Nano, e.LastSeen)
		if err != nil {
			continue
		}
		times[i] = t
		l := hosts[e.Host]
		if l == nil || t.After(l.at) {
			l = &latest{at: t}
			hosts[e.Host] = l
		}
		if t.Equal(l.at) {
			l.gets = l.gets || gets(e)
			l.blocks = l.blocks || !gets(e)
		}
	}
	for i, e := range out {
		if times[i].IsZero() {
			continue
		}
		l := hosts[e.Host]
		if l.gets != l.blocks {
			out[i].Superseded = times[i].Before(l.at) && gets(e) != l.gets
		}
	}
	return out
}

// reachesFurther says a rule's resources allow something other than host
// alone: ports and case don't make a resource another host.
func reachesFurther(resources []string, host string) bool {
	host = stripPort(strings.ToLower(strings.TrimSpace(host)))
	for _, res := range resources {
		if stripPort(strings.ToLower(strings.TrimSpace(res))) != host {
			return true
		}
	}
	return false
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

// Block stops the sandbox reaching host, whatever allows it now. sbx refuses a deny beside an allow of the identical resource
// in the same scope, and beside no other (a deny beats any allow, wider or global ones included), so:
//   - the sandbox's own allow rules for exactly host are removed, and removing is the ids of those the user was shown: if the
//     rules are not those now, nothing is done;
//   - a deny is then added, unless the removal left nothing that allows host (a host no rule allowed is blocked already) or a
//     deny already covers it.
//
// Global rules are never touched. It returns the last sbx command run, nil if none was needed. A removal made before a
// failure is said in the error.
func (c *Client) Block(host string, removing []string) ([]string, error) {
	if err := c.check(host); err != nil {
		return nil, err
	}
	for _, id := range removing {
		if err := ValidRuleID(id); err != nil {
			return nil, err
		}
	}
	rules, err := c.NetworkRules()
	if err != nil {
		return nil, err
	}
	own := "sandbox:" + c.Sandbox
	var conflicting []Rule
	stillAllowed, denied := false, false
	for _, r := range rules.Rules {
		if !r.active() {
			continue
		}
		covers := false
		for _, res := range r.Resources {
			covers = covers || hostMatches(res, host)
		}
		switch {
		case r.Decision == Blocked && covers:
			denied = true
		case r.Decision == Allowed && covers && r.Scope == own && identicalTo(r.Resources, host):
			conflicting = append(conflicting, r)
		case r.Decision == Allowed && covers:
			stillAllowed = true
		}
	}
	found := make([]string, len(conflicting))
	for i, r := range conflicting {
		found[i] = r.ID
	}
	if !sameIDs(found, removing) {
		return nil, Refused{"the rules for " + host + " have changed since you were shown them: look at the log again"}
	}
	for _, r := range conflicting {
		if !r.Removable {
			return nil, Refused{fmt.Sprintf("a rule that allows %s (%s) can't be removed from here, and sbx won't add a block beside it", host, ruleName(r))}
		}
	}
	var last []string
	var removed []string
	for _, r := range conflicting {
		last = c.RemoveCommand(r.ID)
		if _, err := c.run(last...); err != nil {
			if len(removed) > 0 {
				return last, fmt.Errorf("removed %s, but removing %s failed: %w", strings.Join(removed, ", "), ruleName(r), err)
			}
			return last, err
		}
		removed = append(removed, ruleName(r))
	}
	if denied || (len(conflicting) > 0 && !stillAllowed) {
		return last, nil
	}
	last = c.DenyCommand(host)
	if _, err := c.run(last...); err != nil {
		if len(removed) > 0 {
			return last, fmt.Errorf("removed %s, but the block could not be added, so %s is still allowed by another rule: %w", strings.Join(removed, ", "), host, err)
		}
		return last, err
	}
	return last, nil
}

// Refused is a change vibe would not make, as opposed to one sbx failed to: nothing was run.
type Refused struct{ Why string }

func (r Refused) Error() string { return r.Why }

func ruleName(r Rule) string {
	if r.Name != "" && r.Name != r.ID {
		return r.Name + " (" + r.ID + ")"
	}
	return r.ID
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, id := range a {
		seen[id]++
	}
	for _, id := range b {
		seen[id]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// AllowRemoving lets the sandbox reach host, whatever blocks it now. A deny beats any allow, so adding an allow beside a deny does
// nothing (and sbx refuses it beside a deny of the identical resource): the deny rules that cover host are removed first, and
// removing is the ids of those the user was shown (if they are not those now, nothing is done). Then the allow is added, unless
// one covers host already. A deny that can't be removed from here, one of the global policy or one sbx says can't be edited,
// stops it all before anything is changed. It returns the last sbx command run.
func (c *Client) AllowRemoving(host string, removing []string) ([]string, error) {
	if err := c.check(host); err != nil {
		return nil, err
	}
	for _, id := range removing {
		if err := ValidRuleID(id); err != nil {
			return nil, err
		}
	}
	rules, err := c.NetworkRules()
	if err != nil {
		return nil, err
	}
	own := "sandbox:" + c.Sandbox
	var denies []Rule
	allowedAlready := false
	for _, r := range rules.Rules {
		if !r.active() {
			continue
		}
		covers := false
		for _, res := range r.Resources {
			covers = covers || hostMatches(res, host)
		}
		switch {
		case covers && r.Decision == Blocked:
			denies = append(denies, r)
		case covers && r.Decision == Allowed:
			allowedAlready = true
		}
	}
	found := make([]string, len(denies))
	for i, r := range denies {
		found[i] = r.ID
	}
	if !sameIDs(found, removing) {
		return nil, Refused{"the rules for " + host + " have changed since you were shown them: look at the log again"}
	}
	for _, r := range denies {
		switch {
		case r.Scope != own && r.ID != "":
			return nil, Refused{fmt.Sprintf("%s blocks %s for every sandbox, so it can't be removed from here (in a terminal, for every sandbox: sbx policy rm network --id %s)", ruleName(r), host, r.ID)}
		case !r.Removable:
			return nil, Refused{fmt.Sprintf("%s blocks %s and can't be removed from here, and while it stands an allow does nothing", ruleName(r), host)}
		}
	}
	var last []string
	var removed []string
	for _, r := range denies {
		last = c.RemoveCommand(r.ID)
		if _, err := c.run(last...); err != nil {
			if len(removed) > 0 {
				return last, fmt.Errorf("removed %s, but removing %s failed: %w", strings.Join(removed, ", "), ruleName(r), err)
			}
			return last, err
		}
		removed = append(removed, ruleName(r))
	}
	if allowedAlready && len(denies) > 0 {
		return last, nil
	}
	last = c.AllowCommand(host)
	if _, err := c.run(last...); err != nil {
		if len(removed) > 0 {
			return last, fmt.Errorf("removed %s, but the allow could not be added, so %s is blocked as it was before: %w", strings.Join(removed, ", "), host, err)
		}
		return last, err
	}
	return last, nil
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
	if tooBroad(host) {
		return fmt.Errorf("refusing a rule that matches far more than one site: %q — use the sbx CLI for that", host)
	}
	return nil
}

// bracketedIPv6 is an IPv6 address in brackets, with a port perhaps.
var bracketedIPv6 = regexp.MustCompile(`^\[[0-9A-Fa-f:.]+\](:\d+)?$`)

// tooBroad says whether host is a pattern or range that takes in far more
// than one site: an address range wider than an IPv4 /8 or an IPv6 /16, or
// a wildcard with less than a domain and its top level fixed (every .com,
// every host on a port). A host to allow can come from the network log,
// which is whatever the sandbox asked to reach.
func tooBroad(host string) bool {
	if _, network, err := net.ParseCIDR(host); err == nil {
		ones, bits := network.Mask.Size()
		return (bits == 32 && ones < 8) || (bits == 128 && ones < 16)
	}
	if bracketedIPv6.MatchString(host) || strings.Count(host, ":") > 1 {
		return false
	}
	name, _, _ := strings.Cut(host, ":")
	if !strings.ContainsAny(name, "*?[]!") {
		return false
	}
	fixed := 0
	for _, label := range strings.Split(name, ".") {
		if label != "" && !strings.ContainsAny(label, "*?[]!") {
			fixed++
		}
	}
	return fixed < 2
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
			Reason    string   `json:"reason"`
			Layer     string   `json:"layer"`
			Editable  *bool    `json:"editable"`
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
			Reason:    r.Reason,
			Layer:     r.Layer,
			editable:  r.Editable,
			Removable: r.Editable != nil && *r.Editable && r.ID != "",
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
