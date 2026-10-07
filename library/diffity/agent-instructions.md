## Diffity

`diffity` is installed and its skills are available, but no session is
running at startup. Comments are anchored to a specific diff, so a session
started before a commit goes stale as soon as one is made — start a fresh
session when review is wanted.

- `/diffity-diff <ref>` starts a session and opens the diff viewer.
- `/diffity-review <ref>` leaves your own inline comments, tagged
  `[must-fix]`, `[suggestion]`, `[nit]` or `[question]`.
- `/diffity-resolve` reads all open comments — the user's or your own —
  and applies the requested changes.
- `diffity agent list --status open --json` reads the current session's
  comments directly.

There is no browser in this sandbox: always pass `--no-open`, and let the
viewer bind its default port 5391, which is the only container port published
to the host.

When starting `diffity` yourself, give refs as options, with `--no-open`
after them: `diffity --base master --compare HEAD --no-open`. After a
positional ref, `--no-open` is read as a second git ref and the launch fails.

5391 is not the port the user reaches it on. It is published to a host port
allocated when the sandbox was created, which differs between sandboxes — so
never quote 5391 to the user, and never guess the URL.

End every command that produces something to look at — `/diffity-diff`,
`/diffity-review`, `/diffity-tree`, `/diffity-tour` — by running:

    diffity-url <ref>

and giving the user exactly what it prints, e.g. `http://localhost:5396/diff` for the
uncommitted changes, or, after reviewing against
master: `http://localhost:5396/diff?ref=master`. Pass the same ref the
command used, and no argument when it had none. Don't rewrite it into
`http://localhost:5396?ref=...`: the root drops the ref on its way to
`/diff`, and the user's terminal cuts that form off at the `..` of a range. Do the same after anything that
changes a review — resolving, replying to or dismissing comments — so the
user always has the link to go and look. This overrides the diffity skills'
own "running at http://localhost:5391" and "check your browser" endings.

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
re-run `/diffity-review`, which only adds your own comments. The same
happens to your own comments when the running viewer is on a different ref
from the one you reviewed: restart it with `--new`, and check that
`~/.diffity/<hash>/current-session` names the ref the user is looking at.
