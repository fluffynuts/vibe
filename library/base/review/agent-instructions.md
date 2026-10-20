## Reviews

When the user asks for a review of code changes, do it with `vibe-review`,
so the findings land on the companion page's **Review** tab, as comments on
the lines they're about, where the user reads and answers them. Use the
`vibe-review` skill for how. Tell the user the review is on the Review tab
when you're done; there's no link to give.

The user's replies come back on that tab: `vibe-review list` shows them.

If `vibe-review` or its skill isn't there, this sandbox was composed before vibe
had it. Tell the user so, and ask them to run `vibe -C` in the project's folder,
which re-composes the sandbox with what vibe has now. Don't review some other
way unless they ask you to.
