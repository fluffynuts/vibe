package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"vibe/internal/prompt"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// The states --list shows, in the order it groups them. Each is padded to
// the same width, so the columns after it line up.
const (
	listRunning  = "RUNNING"
	listSleeping = "SLEEPING"
	listUnknown  = "UNKNOWN"
	listStopped  = "STOPPED"
	listGone     = "GONE"
)

var listOrder = map[string]int{listRunning: 0, listSleeping: 1, listUnknown: 2, listStopped: 3, listGone: 4}

var listColor = map[string]string{
	listRunning:  colorGreen,
	listSleeping: colorCyan,
	listUnknown:  colorYellow,
	listStopped:  colorRed,
	listGone:     colorGrey,
}

// listRow is one line of --list: state, owner (vibe, for a sandbox vibe has
// a record of; sbx for one made with sbx directly) and name, with the
// folder a GONE sandbox was made for, and what sbx said of an UNKNOWN one.
type listRow struct {
	state, owner, name string
	folder             string
	sbxState           string
}

func doList(vibeHome string) error {
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	records, err := state.List(vibeHome)
	if err != nil {
		return err
	}
	rows := listRows(statuses, records, func(target string) []running.Claim { return running.Claims(vibeHome, target) })
	if len(rows) == 0 {
		note("no sandboxes found")
		return nil
	}
	color := os.Getenv("NO_COLOR") == "" && prompt.IsTerminal(os.Stdout)
	writeList(os.Stdout, rows, color)
	explainList(rows)
	return nil
}

// listRows is every sandbox sbx has, and every one vibe has a record of
// that sbx no longer has, grouped by state and sorted by name within each.
// One sbx says is stopped is SLEEPING when a vibe is attached to it (see
// planStopAll); claims are the vibes' claims on a folder.
func listRows(statuses []sbxrun.Status, records []state.Instance, claims func(target string) []running.Claim) []listRow {
	recorded := map[string]state.Instance{}
	for _, r := range records {
		recorded[r.Name] = r
	}
	var rows []listRow
	inSbx := map[string]bool{}
	for _, s := range statuses {
		inSbx[s.Name] = true
		row := listRow{state: listStateOf(s.State), owner: "sbx", name: s.Name, sbxState: s.State}
		if r, ok := recorded[s.Name]; ok {
			row.owner = "vibe"
			if row.state == listStopped && len(sessions(claims(r.Target))) > 0 {
				row.state = listSleeping
			}
		}
		rows = append(rows, row)
	}
	for _, r := range records {
		if !inSbx[r.Name] {
			rows = append(rows, listRow{state: listGone, owner: "vibe", name: r.Name, folder: r.Target})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.state != b.state {
			return listOrder[a.state] < listOrder[b.state]
		}
		if fa, fb := strings.ToLower(a.name), strings.ToLower(b.name); fa != fb {
			return fa < fb
		}
		return a.name < b.name
	})
	return rows
}

func listStateOf(sbxState string) string {
	switch sbxState {
	case "running":
		return listRunning
	case "stopped":
		return listStopped
	}
	return listUnknown
}

// writeList prints rows as "STATE  OWNER  NAME", the name last so that only
// it varies in width; a GONE row adds its folder, lined up after the names.
// Only the state is coloured, and only when color is set, so what a script
// reads is the same either way.
func writeList(w io.Writer, rows []listRow, color bool) {
	nameWidth := 0
	for _, r := range rows {
		if r.folder != "" && len(r.name) > nameWidth {
			nameWidth = len(r.name)
		}
	}
	for _, r := range rows {
		label := fmt.Sprintf("%-8s", r.state)
		if color {
			label = "\x1b[" + listColor[r.state] + "m" + label + "\x1b[0m"
		}
		if r.folder == "" {
			fmt.Fprintf(w, "%s  %-4s  %s\n", label, r.owner, r.name)
		} else {
			fmt.Fprintf(w, "%s  %-4s  %-*s  %s\n", label, r.owner, nameWidth, r.name, r.folder)
		}
	}
}

// explainList says, on stderr where it doesn't get in a script's way, what
// a SLEEPING, UNKNOWN or GONE row means and what to do about it.
func explainList(rows []listRow) {
	var gone, remakeable, sleeping []string
	for _, r := range rows {
		switch r.state {
		case listSleeping:
			sleeping = append(sleeping, r.name)
		case listUnknown:
			if r.sbxState == "" {
				note("%s: sbx ls didn't say what state it is in", r.name)
			} else {
				note("%s: sbx says it is %q", r.name, r.sbxState)
			}
		case listGone:
			gone = append(gone, r.name)
			if folderGone(r.folder) == "" {
				remakeable = append(remakeable, r.folder)
			}
		}
	}
	if len(sleeping) > 0 {
		note("SLEEPING: sbx stopped %s while a vibe was attached, which is waiting for it — the vibe's companion page starts it again; 'vibe --stop' in its folder ends the vibe (saving its agent's memories) and leaves it stopped", strings.Join(sleeping, ", "))
	}
	if len(gone) == 0 {
		return
	}
	note("GONE: vibe made these, but sbx no longer has them — 'vibe --cleanup' clears away what vibe kept of them")
	if len(remakeable) > 0 {
		note("  or run 'vibe' in its folder to make a new one; these folders still exist:")
		for _, f := range remakeable {
			note("    %s", f)
		}
	}
}
