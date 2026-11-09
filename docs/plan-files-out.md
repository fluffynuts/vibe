# Getting files out of the sandbox (planned, not built, and not to be chased yet)

Raised 2026-11-09. The user's words: the "temp folder in the repo" trick is enough for the moment, but it is not convenient, and there is no clear idea yet of the flow they would like. This records what there is, the options, and what is not known, so it can be picked up later without starting again. Nothing here is decided.

## What happened

Another agent, in a sandbox of its own, took Playwright screenshots of a page (four of them: desktop and mobile, with and without a campaign) and said where they were: `/tmp/pw-out/v-{desk,mob}-{campaign,nocampaign}.png`. That is inside its sandbox, which the user cannot open. Nothing in vibe carries a file from the agent to the user.

## What there is

- **Into the sandbox:** the clipboard page (`library/base/clipboard-bridge`). The user pastes, drops or picks things there and they reach the agent with the next message, as paths under `~/.local/state/vibe/clipboard/items/`. Its server (`clipboard-server.js`) runs **in the sandbox**, on 5390, published to the host, so the page is served by the sandbox, on an origin of its own. Its history is kept on the host (`VIBE_CLIPBOARD_DIR`, a mounted folder, newest 25 items and 250 MB) so it outlives the sandbox.
- **Out of the sandbox:** nothing built. What reaches the host is what is in a folder the host shares: the workspace; the memory store; the clipboard history; the review inbox (the agent writes, the host reads) and view (read-only to the agent). `sandboxFolders` in `src/vibe/folders.go` is the one place they are decided.
- **The Review tab's file viewer** opens plain files from the workspace and shows them as text. It has no image handling (read in `viewfile.go`, not tried). Whether the conversation page would show an image from a path the agent writes is not known; nothing was seen that does it.
- **Today's way** is a folder in the workspace that git ignores (this repo's `tmp/` is), opened from the host's file explorer or IDE. Free, and enough, but the user has to go and look, the project needs the folder in its `.gitignore`, and the agent has to be told.

## What is wanted

Artefacts from testing (screenshots first; also traces, reports, logs, videos) that the user can see, and keep or download, through vibe, without going to a folder. How is not decided.

## Options

1. **Keep the folder in the workspace, and say so.** One line in the agent's instructions: put files for the user in `<workspace>/tmp/` (git-ignored; made if missing) and say the path. No code. The first thing to do if this matters before anything else is built.
2. **An outbox folder, mounted like the review inbox,** `~/.vibe/outbox/<name>/` on the host, `$VIBE_OUTBOX_DIR` in the sandbox, with a Files card or tab on the companion page: list, preview images, download, delete. The host then reads and serves files the agent made, so they are hostile input:
   - served as downloads (`Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`), except a short allow-list of raster images (png, jpeg, gif, webp) shown inline;
   - never HTML, SVG or PDF inline: the companion page holds the token, and an agent's HTML on its origin could use it;
   - opened as `os.Root`s and refusing links, as `viewfile.go` and `fscopy.TreeKeepTimes` do; caps on count and size, as the clipboard has.
3. **A second lane on the clipboard page, from the agent to the user.** The agent posts a file (a small command, say `vibe-share FILE [title]`) to the clipboard server, and the page lists it, previews images and offers a download. Because that server runs in the sandbox, agent content is served on the sandbox's own origin and never touches the companion's origin or the host's filesystem unless the user saves it. It could use the history folder already mounted, so files can outlive the sandbox, or not. Needs: the server, the page, the command, and an agent instruction; no change to what the host trusts.
4. **Cards in the conversation.** The agent says "here's the screenshot" and the page shows it there, with a download. The best flow for the case that started this, and the most work: the page has to learn of the file (through 2 or 3) and tie it to a place in the conversation. A later step on 2 or 3, not instead of them.
5. **Attached to a review comment** (`vibe-review comment --attach FILE`): an artefact tied to a finding, such as a screenshot of a layout fault. Narrow, but it is where a screenshot is most useful to a reviewer.

Something the agent can do now, without vibe: put the several images on **one page**, a single HTML or PNG contact sheet in a labelled grid, so a set of four is one thing to open.

## Leaning, not decided

Option 1 now, as the interim. Then option 3 first: it needs no new trust on the host, it uses a page the user already has open for pasting, and the origin separation comes free. Option 4 as the nicety after. Option 2 only if files should be kept and browsed on the host side with vibe's own UI.

## Not known

- Whether the clipboard history (items the user pasted, claimed by the agent's hook on the next prompt) can carry a second kind of item cleanly, or wants a list of its own.
- How big artefacts (videos, traces) behave through the published port.
- Whether the user wants files to outlive the sandbox (kept like the clipboard history), or to go with it.

## Open questions for the user

1. What would be most use: a gallery of the agent's latest screenshots, a list of the files from each turn, or the files inline in the conversation?
2. Should files outlive the sandbox?
3. Images only to begin with, or anything (traces, zips, HTML reports)? HTML reports are the hazard.
4. On the companion page, or on the clipboard page?
5. Download only, or also somewhere in the host's own folders to open them from?
