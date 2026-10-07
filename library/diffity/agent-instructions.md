## Diffity

`diffity` is installed, but no session is running at startup. Comments are
anchored to a specific diff, so a session started before a commit goes stale
as soon as one is made — start a fresh session when review is wanted.

There are no diffity skills or slash commands: use the `diffity` CLI directly.

**When the user asks for a code review** (of uncommitted changes, a commit, a
range or a branch), do it with diffity rather than only replying in chat:

1. Start a fresh viewer on the ref under review, e.g.
   `diffity --base master --compare HEAD --no-open --new` (omit both options
   to review the uncommitted changes).
2. Read the change with `diffity agent diff`, plus whole files and callers as
   needed — not just the hunks.
3. Leave findings as inline comments:
   `diffity agent comment --file <path> --line <n> --body "[must-fix] ..."`,
   tagged `[must-fix]`, `[suggestion]`, `[nit]` or `[question]`. Use
   `diffity agent general-comment --body ...` for diff-level remarks. Check
   `diffity agent list --status open --json` first so you don't duplicate
   comments already there.
4. Give the user the viewer link (see below), with a short summary of what
   you found.

When the user has left comments to act on, read them with
`diffity agent list --status open --json`, make the changes, then
`diffity agent resolve <id> --summary "..."`, `reply` or `dismiss` each one.

There is no browser in this sandbox: always pass `--no-open`, and let the
viewer bind its default port 5391, which is the only container port published
to the host.

When starting `diffity` yourself, give refs as options, with `--no-open`
after them: `diffity --base master --compare HEAD --no-open`. After a
positional ref, `--no-open` is read as a second git ref and the launch fails.

5391 is not the port the user reaches it on. It is published to a host port
allocated when the sandbox was created, which differs between sandboxes — so
never quote 5391 to the user, and never guess the URL.

End every review, diff, tree or tour you set up for the user to look at by running:

    diffity-url <ref>

and giving the user exactly what it prints, e.g. `http://localhost:5396/diff` for the
uncommitted changes, or, after reviewing against
master: `http://localhost:5396/diff?ref=master`. Pass the same ref the
command used, and no argument when it had none. Don't rewrite it into
`http://localhost:5396?ref=...`: the root drops the ref on its way to
`/diff`, and the user's terminal cuts that form off at the `..` of a range. Do the same after anything that
changes a review — resolving, replying to or dismissing comments — so the
user always has the link to go and look. Never say the viewer is "running at http://localhost:5391" or tell
the user to check their browser.

The script reads the URL vibe checks against sbx's live port mapping at
every session start, falling back to `VIBE_DIFFITY_URL` from when the sandbox
was created. It warns on stderr whenever it can't vouch for the port; pass
that warning on with the URL rather than presenting it as certain.

A hook holds you to this: a reply in a turn that used diffity, while a
viewer is running, is sent back if it lacks the URL or quotes another
localhost port.

If the user says they left comments but `diffity agent list` shows none,
the session's ref no longer matches the diff they commented on (usually
because of an intervening commit). Say so rather than asking them to
re-run the review, which only adds your own comments. The same
happens to your own comments when the running viewer is on a different ref
from the one you reviewed: restart it with `--new`, and check that
`~/.diffity/<hash>/current-session` names the ref the user is looking at.
