package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"vibe/internal/backup"
	"vibe/internal/browser"
	"vibe/internal/companion"
	"vibe/internal/layout"
	agentmem "vibe/internal/memory"
	"vibe/internal/portalloc"
	"vibe/internal/sbxrun"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// What can be picked for deleting, as the IDs the CLI's checklist and the
// Profiles page select by: a whole profile, one sandbox vibe has a record
// of, or a sandbox it hasn't.
const (
	pickProfile = "profile:"
	pickSandbox = "sandbox:"
	pickUnknown = "unknown:"
)

// profileFlags are what's worth knowing about p at a glance when deciding
// whether it's trash.
func profileFlags(p inventoryProfile) []string {
	var flags []string
	switch {
	case !p.exists:
		flags = append(flags, "profile gone")
	case p.bundled:
		flags = append(flags, "vibe's own")
	}
	if p.exists && len(p.sandboxes) == 0 {
		flags = append(flags, "no sandboxes")
	} else if len(p.sandboxes) > 0 {
		allGone := true
		for _, s := range p.sandboxes {
			if !s.likelyTrash() {
				allGone = false
			}
		}
		if allGone {
			flags = append(flags, "every sandbox or folder gone")
		}
	}
	switch p.standing.state {
	case profileOutOfDate:
		flags = append(flags, "out of date")
	case profileEdited:
		flags = append(flags, "edited by hand")
	case profileMissingFeatures:
		flags = append(flags, "features missing from the library")
	}
	return flags
}

// sandboxFlags are what's worth knowing about s at a glance.
func sandboxFlags(s inventorySandbox) []string {
	var flags []string
	switch {
	case !s.inSbx:
		flags = append(flags, "gone from sbx")
	case s.running:
		flags = append(flags, "running")
	default:
		flags = append(flags, "stopped")
	}
	if s.folderGone {
		flags = append(flags, "folder gone")
	}
	if s.attached {
		flags = append(flags, "a vibe is attached")
	}
	if s.outOfDate != "" {
		flags = append(flags, "out of date")
	}
	return flags
}

// removal is what a selection comes to: whole profiles (with all their
// sandboxes), single sandboxes (their profiles kept), and sandboxes vibe
// has no record of.
type removal struct {
	profiles  []inventoryProfile
	sandboxes []inventorySandbox
	unknown   []string
}

func (d removal) empty() bool {
	return len(d.profiles) == 0 && len(d.sandboxes) == 0 && len(d.unknown) == 0
}

// whyKept says why sandbox s can't be deleted, or "" when it can. own is
// the sandbox whose page is asking ("" from a terminal): deleting it would
// end the page.
func whyKept(s inventorySandbox, own string) string {
	switch {
	case own != "" && s.inst.Name == own:
		return "it's the sandbox this page is for — use 'vibe --manage', or another sandbox's page"
	case s.attached:
		return "a vibe is attached to it — stop it first ('vibe --stop " + s.inst.Target + "')"
	}
	return ""
}

// whyProfileKept says why profile p can't be deleted, or "" when it can.
func whyProfileKept(p inventoryProfile, own string) string {
	switch {
	case !p.exists:
		return "it's already gone"
	case p.bundled:
		return "it ships with vibe, and the next upgrade would bring it back"
	}
	for _, s := range p.sandboxes {
		if why := whyKept(s, own); why != "" {
			return fmt.Sprintf("its sandbox '%s' can't be deleted: %s", s.inst.Name, why)
		}
	}
	return ""
}

// planRemoval turns picked IDs into what's to be deleted, refusing any
// that can't be, and dropping a sandbox picked along with its profile.
func planRemoval(inv inventory, picked []string, own string) (removal, error) {
	profiles := map[string]inventoryProfile{}
	sandboxes := map[string]inventorySandbox{}
	for _, p := range inv.profiles {
		profiles[p.name] = p
		for _, s := range p.sandboxes {
			sandboxes[s.inst.Name] = s
		}
	}
	unknown := map[string]bool{}
	for _, s := range inv.unknown {
		unknown[s.Name] = true
	}

	var d removal
	wholeProfile := map[string]bool{}
	var singles []inventorySandbox
	for _, id := range picked {
		switch {
		case strings.HasPrefix(id, pickProfile):
			p, ok := profiles[strings.TrimPrefix(id, pickProfile)]
			if !ok {
				return removal{}, fmt.Errorf("there's no profile '%s'", strings.TrimPrefix(id, pickProfile))
			}
			if why := whyProfileKept(p, own); why != "" {
				return removal{}, fmt.Errorf("profile '%s' can't be deleted: %s", p.name, why)
			}
			if !wholeProfile[p.name] {
				wholeProfile[p.name] = true
				d.profiles = append(d.profiles, p)
			}
		case strings.HasPrefix(id, pickSandbox):
			s, ok := sandboxes[strings.TrimPrefix(id, pickSandbox)]
			if !ok {
				return removal{}, fmt.Errorf("vibe has no record of a sandbox '%s'", strings.TrimPrefix(id, pickSandbox))
			}
			if why := whyKept(s, own); why != "" {
				return removal{}, fmt.Errorf("sandbox '%s' can't be deleted: %s", s.inst.Name, why)
			}
			singles = append(singles, s)
		case strings.HasPrefix(id, pickUnknown):
			name := strings.TrimPrefix(id, pickUnknown)
			if !unknown[name] {
				return removal{}, fmt.Errorf("sbx has no unrecorded sandbox '%s'", name)
			}
			d.unknown = append(d.unknown, name)
		default:
			return removal{}, fmt.Errorf("'%s' isn't something that can be picked", id)
		}
	}
	seen := map[string]bool{}
	for _, s := range singles {
		if !wholeProfile[s.inst.Profile] && !seen[s.inst.Name] {
			seen[s.inst.Name] = true
			d.sandboxes = append(d.sandboxes, s)
		}
	}
	return d, nil
}

// memoryStoreOf is where sandbox inst's agent memories are kept on the
// host, or "" when that's not a folder of its own (which only happens if a
// record were edited to point elsewhere): one shared with anything else is
// never deleted.
func memoryStoreOf(lay layout.Layout, inst state.Instance) string {
	store := inst.MemoryStore
	if store == "" {
		store = agentmem.StoreFor(lay.HomePath("memories"), inst.Name)
	}
	if filepath.Base(store) != inst.Name {
		return ""
	}
	return store
}

// profileBackups are the backups of profile p: taken under its own name, or
// a sandbox of its.
func profileBackups(lay layout.Layout, p inventoryProfile) []string {
	names := []string{p.name}
	for _, s := range p.sandboxes {
		names = append(names, s.inst.Name)
	}
	var paths []string
	seen := map[string]bool{}
	for _, name := range names {
		list, _ := backup.List(lay.HomePath("backup"), name)
		for _, b := range list {
			if !seen[b.Path] {
				seen[b.Path] = true
				paths = append(paths, b.Path)
			}
		}
	}
	return paths
}

// describe lists everything d deletes, by name and path, for the user to
// confirm: exactly what will go.
func (d removal) describe(lay layout.Layout) []string {
	var lines []string
	sandboxLines := func(s inventorySandbox, indent string) {
		what := "its record"
		if s.inSbx {
			what = "the sandbox, and its record"
		}
		lines = append(lines, fmt.Sprintf("%ssandbox '%s' (for %s): %s", indent, s.inst.Name, displayPath(s.inst.Target), what))
		if store := memoryStoreOf(lay, s.inst); store != "" && fileExists(store) {
			lines = append(lines, indent+"  agent memories: "+displayPath(store))
		}
		if dir := state.ClipboardPath(lay.Home, s.inst.Name); fileExists(dir) {
			lines = append(lines, indent+"  clipboard history: "+displayPath(dir))
		}
		if file := state.ConversationPath(lay.Home, s.inst.Name); fileExists(file) {
			lines = append(lines, indent+"  conversation: "+displayPath(file))
		}
		if s.inst.GitToken != "" {
			lines = append(lines, indent+"  its GitHub token")
		}
	}
	for _, p := range d.profiles {
		lines = append(lines, fmt.Sprintf("profile '%s': %s", p.name, displayPath(lay.HomePath("profiles", p.name))))
		if backups := profileBackups(lay, p); len(backups) > 0 {
			lines = append(lines, fmt.Sprintf("  its %d backup(s), in %s", len(backups), displayPath(lay.HomePath("backup"))))
		}
		for _, s := range p.sandboxes {
			sandboxLines(s, "  ")
		}
	}
	for _, s := range d.sandboxes {
		sandboxLines(s, "")
	}
	for _, name := range d.unknown {
		lines = append(lines, fmt.Sprintf("sandbox '%s' (vibe has no record of it)", name))
	}
	return lines
}

// run deletes everything d holds, carrying on past a failure, and says
// what it couldn't delete. A profile is kept when one of its sandboxes
// can't be removed, so that deleting it can be tried again. remove is
// sbxrun.Remove, and a stand-in under test.
func (d removal) run(lay layout.Layout, remove func(name string, force bool) error) error {
	var failed []string
	for _, p := range d.profiles {
		ok := true
		for _, s := range p.sandboxes {
			if err := deleteSandbox(lay, s, remove); err != nil {
				note("  WARNING: %s", err)
				failed = append(failed, "sandbox '"+s.inst.Name+"'")
				ok = false
			}
		}
		if !ok {
			note("  kept profile '%s', as not all its sandboxes could be removed", p.name)
			failed = append(failed, "profile '"+p.name+"'")
			continue
		}
		for _, path := range profileBackups(lay, p) {
			if err := os.Remove(path); err != nil {
				note("  WARNING: couldn't delete backup %s: %s", displayPath(path), err)
			}
		}
		if err := os.RemoveAll(lay.HomePath("profiles", p.name)); err != nil {
			note("  WARNING: couldn't delete profile '%s': %s", p.name, err)
			failed = append(failed, "profile '"+p.name+"'")
			continue
		}
		note("deleted profile '%s'", p.name)
	}
	for _, s := range d.sandboxes {
		if err := deleteSandbox(lay, s, remove); err != nil {
			note("  WARNING: %s", err)
			failed = append(failed, "sandbox '"+s.inst.Name+"'")
		}
	}
	for _, name := range d.unknown {
		note("removing sandbox '%s'", name)
		if err := remove(name, true); err != nil {
			note("  WARNING: couldn't remove sandbox '%s': %s", name, err)
			failed = append(failed, "sandbox '"+name+"'")
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("couldn't delete %s — see above", strings.Join(failed, ", "))
	}
	return nil
}

// deleteSandbox removes sandbox s, when sbx still has it, and everything
// vibe keeps for it. Its record goes too: a record outliving its sandbox
// would hold its published ports against every sandbox made after it. When
// sbx can't remove it, nothing of vibe's is deleted, so it can be tried
// again.
func deleteSandbox(lay layout.Layout, s inventorySandbox, remove func(name string, force bool) error) error {
	name := s.inst.Name
	if s.inSbx {
		note("removing sandbox '%s'", name)
		// Forced: whether it's running was shown when it was picked.
		if err := remove(name, true); err != nil {
			return fmt.Errorf("couldn't remove sandbox '%s': %w", name, err)
		}
	}
	forgetGitSecret(lay.Home, name)
	if err := state.Remove(lay.Home, name); err != nil {
		return fmt.Errorf("sandbox '%s': %w", name, err)
	}
	if store := memoryStoreOf(lay, s.inst); store != "" {
		if err := os.RemoveAll(store); err != nil {
			return fmt.Errorf("sandbox '%s': couldn't delete its memories: %w", name, err)
		}
	}
	note("deleted sandbox '%s'", name)
	return nil
}

// companionManager is the Profiles tab's view of the machine: own is the
// sandbox whose page it's on ("" for the page served on its own), which
// it won't delete.
type companionManager struct {
	lay layout.Layout
	own string
}

// takeStock is the inventory, giving up after statusWithin, as the Admin
// tab's status does: a stuck sbx mustn't leave the page waiting for good.
func (m *companionManager) takeStock() (inventory, error) {
	ctx, cancel := context.WithTimeout(context.Background(), statusWithin)
	defer cancel()
	inv, err := gatherInventory(ctx, m.lay)
	if ctx.Err() != nil {
		err = fmt.Errorf("sbx didn't answer within %s, so what it has is unknown — 'sbx ls' in a terminal shows whether sbx itself is stuck", statusWithin)
	}
	return inv, err
}

func (m *companionManager) Inventory() companion.ManageInventory {
	out := companion.ManageInventory{Profiles: []companion.ManageProfile{}, Unknown: []companion.ManageUnknown{}}
	inv, err := m.takeStock()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	for _, p := range inv.profiles {
		mp := companion.ManageProfile{
			ID: pickProfile + p.name, Name: p.name, Features: nonNil(p.features), Flags: nonNil(profileFlags(p)),
			Trash: p.likelyTrash(), Kept: whyProfileKept(p, m.own), Actions: profileActions(p), Sandboxes: []companion.ManageSandbox{},
		}
		for _, s := range p.sandboxes {
			mp.Sandboxes = append(mp.Sandboxes, companion.ManageSandbox{
				ID: pickSandbox + s.inst.Name, Name: s.inst.Name, Folder: displayPath(s.inst.Target),
				Flags: nonNil(sandboxFlags(s)), Trash: s.likelyTrash(), Kept: whyKept(s, m.own), Own: s.inst.Name == m.own,
				Actions: sandboxActions(s, m.own),
			})
		}
		out.Profiles = append(out.Profiles, mp)
	}
	for _, s := range inv.unknown {
		out.Unknown = append(out.Unknown, companion.ManageUnknown{ID: pickUnknown + s.Name, Name: s.Name, Running: s.Running})
	}
	return out
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func (m *companionManager) Plan(ids []string) ([]string, error) {
	inv, err := m.takeStock()
	if err != nil {
		return nil, err
	}
	plan, err := planRemoval(inv, ids, m.own)
	if err != nil {
		return nil, err
	}
	return plan.describe(m.lay), nil
}

// Delete deletes what ids pick, as --cleanup does, taking stock afresh: what
// was shown may have changed since (a vibe attached to a sandbox, say).
func (m *companionManager) Delete(ctx context.Context, ids []string, log func(string)) error {
	teeNotes(log)
	defer teeNotes(nil)
	inv, err := m.takeStock()
	if err != nil {
		return err
	}
	plan, err := planRemoval(inv, ids, m.own)
	if err != nil {
		return err
	}
	err = plan.run(m.lay, func(name string, force bool) error { return sbxrun.RemoveToContext(ctx, name, force, actionOutput()) })
	if ctx.Err() != nil {
		return fmt.Errorf("cancelled — what was deleted before then is gone; the list shows what's left")
	}
	return err
}

// doManage is --manage: the Profiles page, served on its own (no sandbox,
// no folder) until Ctrl-C. Its port and token are kept, so its URL stays the
// same; when another vibe is already serving it, that one is opened.
func doManage(lay layout.Layout) error {
	token, err := state.ManageToken(lay.Home)
	if err != nil {
		return err
	}
	instances, err := state.List(lay.Home)
	if err != nil {
		return err
	}
	port, served, err := chooseManagePort(instances, state.ManagePort(lay.Home), portalloc.InUse,
		func(p int) bool { return companion.Running(p, "") })
	if err != nil {
		return err
	}
	if err := state.SaveManagePort(lay.Home, port); err != nil {
		note("WARNING: couldn't remember the page's port, so its URL may change next time: %s", err)
	}
	user, err := settings.Load(lay.File("settings.yaml"))
	if err != nil {
		return err
	}
	mode, err := companionMode(false, user.Companion)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://localhost:%d/#%s", port, token)
	if served {
		note("the Profiles page is already being served by another vibe: %s", url)
		if mode == companionOpen {
			browser.Open(url)
		}
		return nil
	}
	srv, err := companion.Start(companion.Config{Token: token, Port: port, Manage: &companionManager{lay: lay}})
	if err != nil {
		return fmt.Errorf("couldn't serve the Profiles page on port %d: %w", port, err)
	}
	defer srv.Close()
	note("Profiles page: %s", url)
	if mode == companionOpen && !browser.Open(url) {
		note("no browser to open it in from here — open the URL above yourself")
	}
	note("serving it until Ctrl-C")
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	<-interrupts
	note("stopped serving the Profiles page")
	return nil
}

// sandboxActions are what the Profiles tab offers to do to s, besides
// deleting it. The page's own sandbox is the Admin tab's to work on. One
// with a vibe attached, or whose folder is gone, can only be stopped.
func sandboxActions(s inventorySandbox, own string) []string {
	switch {
	case !s.inSbx || s.inst.Name == own:
		return []string{}
	case s.attached || s.folderGone:
		if s.running {
			return []string{companion.ManageStop}
		}
		return []string{}
	}
	actions := []string{companion.ManageStart}
	if s.running {
		actions = []string{companion.ManageStop}
	}
	actions = append(actions, companion.ManageRebuild)
	if s.outOfDate != "" {
		actions = append(actions, companion.ManageUpdate)
	}
	return actions
}

// profileActions are what the Profiles tab offers to do to p: re-compose a
// guided profile of the user's, and bring it and its sandboxes up to date
// when any of them are out of date.
func profileActions(p inventoryProfile) []string {
	actions := []string{}
	if !p.exists || p.bundled || p.standing.state == profileNotComposed || p.standing.state == profileMissingFeatures {
		return actions
	}
	actions = append(actions, companion.ManageRecompose)
	stale := p.standing.state == profileOutOfDate
	for _, s := range p.sandboxes {
		if s.outOfDate != "" {
			stale = true
		}
	}
	if stale {
		actions = append(actions, companion.ManageUpdate)
	}
	return actions
}

// rowFor finds the row id picks in inv, and the actions it offers now.
func (m *companionManager) rowFor(inv inventory, id string) (p *inventoryProfile, s *inventorySandbox, actions []string) {
	for i := range inv.profiles {
		prof := &inv.profiles[i]
		if id == pickProfile+prof.name {
			return prof, nil, profileActions(*prof)
		}
		for j := range prof.sandboxes {
			if id == pickSandbox+prof.sandboxes[j].inst.Name {
				return prof, &prof.sandboxes[j], sandboxActions(prof.sandboxes[j], m.own)
			}
		}
	}
	return nil, nil, nil
}

// check finds what action is for, in a fresh inventory, and says why it
// can't be done when it can't.
func (m *companionManager) check(action companion.ManageAction) (inventory, *inventoryProfile, *inventorySandbox, error) {
	inv, err := m.takeStock()
	if err != nil {
		return inv, nil, nil, err
	}
	p, s, actions := m.rowFor(inv, action.ID)
	if p == nil {
		kind, name, _ := strings.Cut(action.ID, ":")
		return inv, nil, nil, fmt.Errorf("there's no %s '%s' any more", kind, name)
	}
	if !slices.Contains(actions, action.Kind) {
		what := "profile '" + p.name + "'"
		if s != nil {
			what = "sandbox '" + s.inst.Name + "'"
		}
		return inv, nil, nil, fmt.Errorf("%s can't be %s now: %s", what, pastTense[action.Kind], whyNot(p, s, m.own))
	}
	return inv, p, s, nil
}

var pastTense = map[string]string{
	companion.ManageStart: "started", companion.ManageStop: "stopped", companion.ManageRebuild: "rebuilt",
	companion.ManageUpdate: "updated", companion.ManageRecompose: "re-composed",
}

// whyNot says why a row doesn't offer an action.
func whyNot(p *inventoryProfile, s *inventorySandbox, own string) string {
	if s != nil {
		switch {
		case !s.inSbx:
			return "sbx no longer has it"
		case s.inst.Name == own:
			return "it's this page's sandbox: the Admin tab works on it"
		case s.attached:
			return "a vibe is attached to it"
		case s.folderGone:
			return folderGone(s.inst.Target)
		case s.outOfDate == "":
			return "it's already up to date, or not in the state that needs it"
		}
		return "it's not in the state that needs it"
	}
	switch {
	case p.bundled:
		return "it ships with vibe"
	case p.standing.why != "":
		return p.standing.why
	}
	return "it and its sandboxes are already up to date"
}

func (m *companionManager) CanDo(action companion.ManageAction) error {
	_, _, _, err := m.check(action)
	return err
}

// Do carries out action, checking afresh that it can be done: what was shown
// may have changed since. A profile's action goes on to each of its
// sandboxes that can take it, saying which can't, and why.
func (m *companionManager) Do(ctx context.Context, action companion.ManageAction, log func(string)) error {
	teeNotes(log)
	defer teeNotes(nil)
	_, p, s, err := m.check(action)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err = m.do(ctx, action.Kind, p, s)
	if ctx.Err() != nil {
		return fmt.Errorf("cancelled — the list shows where that left things")
	}
	return err
}

func (m *companionManager) do(ctx context.Context, kind string, p *inventoryProfile, s *inventorySandbox) error {
	if s != nil {
		switch kind {
		case companion.ManageStart:
			note("starting sandbox '%s'", s.inst.Name)
			if !sbxrun.StartContext(ctx, s.inst.Name) {
				return fmt.Errorf("sbx couldn't start sandbox '%s' — 'sbx ls' shows its state", s.inst.Name)
			}
			note("sandbox '%s' is running", s.inst.Name)
			return nil
		case companion.ManageStop:
			stop := holdingStopper(m.lay.Home, s.inst.Target)
			stop.stop = func(name string) (int, error) {
				if err := sbxrun.StopQuietContext(ctx, name); err != nil {
					return 1, err
				}
				return 0, nil
			}
			_, err := stopSandbox(stop, s.inst.Name, s.inst.Target, true)
			return err
		case companion.ManageRebuild:
			note("rebuilding sandbox '%s'", s.inst.Name)
			return rebuildAsItWas(ctx, m.lay, s.inst, s.running)
		case companion.ManageUpdate:
			if _, err := updateProfile(m.lay, s.inst.Profile, s.inst.Name); err != nil {
				return err
			}
			_, err := updateSandbox(ctx, m.lay, s.inst, s.running)
			return err
		}
		return fmt.Errorf("a sandbox can't be %s", kind)
	}

	backupName := p.name
	if len(p.sandboxes) > 0 {
		backupName = p.sandboxes[0].inst.Name
	}
	switch kind {
	case companion.ManageRecompose:
		note("re-composing profile '%s' from %s", p.name, describeFeatures(p.features))
		if err := replaceProfile(m.lay, p.name, backupName, p.features); err != nil {
			return err
		}
	case companion.ManageUpdate:
		outcome, err := updateProfile(m.lay, p.name, backupName)
		if err != nil {
			return err
		}
		if outcome.needsYou() {
			note("WARNING: profile '%s' wasn't brought up to date: %s", p.name, outcome.why)
		}
	default:
		return fmt.Errorf("a profile can't be %s", kind)
	}
	var failed []string
	for _, sb := range p.sandboxes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if why := whyNotRebuilt(sb, m.own); why != "" {
			note("sandbox '%s' left as it is: %s", sb.inst.Name, why)
			continue
		}
		var err error
		if kind == companion.ManageRecompose {
			note("rebuilding sandbox '%s'", sb.inst.Name)
			err = rebuildAsItWas(ctx, m.lay, sb.inst, sb.running)
		} else {
			_, err = updateSandbox(ctx, m.lay, sb.inst, sb.running)
		}
		if err != nil {
			note("FAILED: sandbox '%s': %s", sb.inst.Name, err)
			failed = append(failed, sb.inst.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("couldn't rebuild %s — see above", strings.Join(failed, ", "))
	}
	return nil
}

// whyNotRebuilt says why a profile's action leaves its sandbox s alone, or
// "" when it doesn't.
func whyNotRebuilt(s inventorySandbox, own string) string {
	switch {
	case !s.inSbx:
		return "sbx no longer has it"
	case s.inst.Name == own:
		return "it's this page's sandbox — Rebuild on the Admin tab rebuilds it"
	case s.attached:
		return "a vibe is attached to it — it's brought up to date when its next session starts"
	case s.folderGone:
		return folderGone(s.inst.Target)
	}
	return ""
}
