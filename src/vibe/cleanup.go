package main

import (
	"context"
	"fmt"
	"strings"

	"vibe/internal/layout"
)

// cleanupRow is one line of --cleanup's checklist: a profile, one of its
// sandboxes, or a sandbox vibe has no record of.
type cleanupRow struct {
	id, label string
	ticked    bool
}

// cleanupRows lays inv out as --cleanup's checklist, grouped by profile,
// with what's likely trash ticked to start with. What can't be deleted
// isn't offered: kept says what, and why.
func cleanupRows(inv inventory) (rows []cleanupRow, kept []string) {
	for _, p := range inv.profiles {
		why := whyProfileKept(p, "")
		offered := why == ""
		if offered {
			rows = append(rows, cleanupRow{
				id:     pickProfile + p.name,
				label:  profileLabel(p),
				ticked: p.likelyTrash(),
			})
		} else if p.exists && !p.bundled {
			kept = append(kept, fmt.Sprintf("profile '%s': %s", p.name, why))
		}
		for _, s := range p.sandboxes {
			if why := whyKept(s, ""); why != "" {
				kept = append(kept, fmt.Sprintf("sandbox '%s': %s", s.inst.Name, why))
				continue
			}
			label := "    " + sandboxLabel(s)
			if !offered {
				label += fmt.Sprintf(" (profile %s: %s)", p.name, strings.Join(profileFlags(p), ", "))
			}
			rows = append(rows, cleanupRow{
				id:     pickSandbox + s.inst.Name,
				label:  label,
				ticked: s.likelyTrash() && !(offered && p.likelyTrash()),
			})
		}
	}
	for _, s := range inv.unknown {
		status := "stopped"
		if s.Running {
			status = "running"
		}
		rows = append(rows, cleanupRow{
			id:    pickUnknown + s.Name,
			label: fmt.Sprintf("sandbox %s [%s] — vibe has no record of it", s.Name, status),
		})
	}
	return rows, kept
}

// profileLabel is how a profile is shown in --cleanup's checklist.
func profileLabel(p inventoryProfile) string {
	label := "profile " + p.name
	if len(p.features) > 0 {
		label += " (" + strings.Join(p.features, ", ") + ")"
	}
	switch len(p.sandboxes) {
	case 0: // its flags say so
	case 1:
		label += " — 1 sandbox"
	default:
		label += fmt.Sprintf(" — %d sandboxes", len(p.sandboxes))
	}
	if flags := profileFlags(p); len(flags) > 0 {
		label += " [" + strings.Join(flags, ", ") + "]"
	}
	return label
}

// sandboxLabel is how a sandbox is shown in --cleanup's checklist.
func sandboxLabel(s inventorySandbox) string {
	return fmt.Sprintf("sandbox %s — %s [%s]", s.inst.Name, displayPath(s.inst.Target), strings.Join(sandboxFlags(s), ", "))
}

// pickCleanup asks which rows to delete, with the likely trash ticked;
// ok is false when the user quit. A variable so tests can stand in for the
// user.
var pickCleanup = func(labels []string, ticked []bool) ([]int, bool) {
	return checklistNoConfirmFrom("Select what to delete — a profile goes with all its sandboxes (likely trash is selected):", labels, ticked)
}

// confirmCleanup asks once, after listing everything that will go, whether
// to go ahead. A variable so tests can stand in for the user.
var confirmCleanup = func() bool {
	return confirmDefault(false, false, "Delete all of this? It can't be undone.")
}

// doCleanup is --cleanup: every profile, its sandboxes and the sandboxes
// vibe has no record of, as a checklist grouped by profile with the likely
// trash ticked; what's checked is listed in full, by name and path, and
// deleted once that's confirmed. Deleting a profile deletes everything of
// it: its sandboxes, their records, memories, clipboard history,
// conversations and GitHub tokens, and its backups.
func doCleanup(lay layout.Layout) error {
	inv, err := gatherInventory(context.Background(), lay)
	if err != nil {
		return err
	}
	rows, kept := cleanupRows(inv)
	for _, line := range kept {
		note("not offered: %s", line)
	}
	if len(rows) == 0 {
		note("no profiles or sandboxes to clean up")
		return nil
	}
	if !interactive() {
		return fmt.Errorf("--cleanup needs a terminal to pick on")
	}
	labels := make([]string, len(rows))
	ticked := make([]bool, len(rows))
	for i, r := range rows {
		labels[i], ticked[i] = r.label, r.ticked
	}
	picked, ok := pickCleanup(labels, ticked)
	if !ok {
		return fmt.Errorf("aborted — nothing deleted")
	}
	ids := make([]string, len(picked))
	for i, idx := range picked {
		ids[i] = rows[idx].id
	}
	plan, err := planRemoval(inv, ids, "")
	if err != nil {
		return err
	}
	if plan.empty() {
		note("nothing checked — nothing deleted")
		return nil
	}
	note("this deletes:")
	for _, line := range plan.describe(lay) {
		note("  %s", line)
	}
	if !confirmCleanup() {
		return fmt.Errorf("aborted — nothing deleted")
	}
	return plan.run(lay, removeSandbox)
}
