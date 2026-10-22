# A plans view (idea, not planned)

Raised 2026-10-22, while building the file viewer (a path the agent names in code is a button that shows the file read-only
over the conversation: `internal/companion/viewfile.go`, `fileRef`/`viewFile` in `app.js`).

A **Plans view** would list the plan files of the workspace (and so spare the user finding them), and could be where editing
comes in later. Decisions so far:

- Only files inside the workspace; the viewer stays read-only. Editing, if wanted, belongs with this view, not the viewer.
- No "open in editor" button that runs anything on the host: an agent-chosen path plus a program launched on the host (modelines,
  autocommands) is the risk. The viewer's copy button lets the user open it in an editor of their choosing.
- A path is a button only after the host confirms it is a text file in the workspace (`GET /api/workspace-files`, one ask per
  render), so config keys (`core.checkStat`) and files outside the workspace (`~/.gitconfig`) are plain code. Extensionless paths
  aren't offered. A path outside the workspace gives no hint that it was left out.
- Idea, not done: viewing files **inside the sandbox** that aren't in the workspace (`~/.gitconfig` there is the sandbox's, not the
  host's, so the host would need `sbx exec cat`); it widens the viewer from "the user's own folder" to "whatever the agent has".
