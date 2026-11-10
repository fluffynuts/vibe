# `--update-sandboxes` a few sandboxes at a time (planned, not built)

The one part of the old "quality of life" plan that was not built; the rest (bold questions, the power menu, the issue links) is
`docs/adr/2026-10-23-page-ergonomics.md`. It is independent of everything else, and its real run is the point: do it whenever its measurement is
cheap to take. How a batch update works today is in `docs/adr/2026-10-14-profiles-and-sandboxes.md`.

## What is wanted (raised 2026-10-24)

**Now:** `doUpdateSandboxes` (`src/vibe/update.go`) brings the profiles up to date one after another, then the sandboxes one after another. With all of the user's sandboxes it takes about 12 to 15 minutes, while the machine is nearly idle: the time is spent waiting (on `sbx`, and what it does under it), not working.

**Proposed:** rebuild two or three sandboxes at once; leave the profiles sequential (they are quick, and a sandbox's rebuild reads its profile, which may have just been re-composed: "asked afresh by `updateSandbox`").

- **How many:** a default of 3, `--jobs N` (1 gives today's behaviour, for anyone who wants the output in order), and a `jobs` setting in settings.yaml for the default. Nothing is "as many as there are".
- **Measure first, with the real thing:** run `--update-sandboxes` as it is, then with 2 and 3 at once, on the user's sandboxes, and keep the times. If `sbx` serialises behind its daemon (one image pull or create at a time), there is nothing to gain and we say so and stop. Report the numbers, as the working agreement asks of any performance change.
- **What must stay true when they overlap**
  - A sandbox's folder is held while it is rebuilt (`running.Hold`, in `rebuildAsItWas`): two sandboxes of the same folder must not rebuild at once, so they are done one after the other within the same worker.
  - The tally (`updateTally`) is written by several goroutines: guarded, or collected from each worker's result.
  - **Output stays readable.** Today's lines ("sandbox 'x' is out of date …: rebuilding it") interleave with the rebuild's own output. Each line a worker prints carries its sandbox's name; the rebuild's own progress goes to a per-sandbox buffer shown whole when it finishes, or on failure. The user must always be able to tell what is under way: a line when each starts and finishes, and a count ("3 of 14 done").
  - **A failure stops nothing else**, as now, and is repeated in the summary at the end.
  - **Ctrl+C** stops starting new ones, lets those under way finish or stops them as a single rebuild would, and says which were not done.
- **Tests:** with fake rebuilds that block: at most N at once (a counter that fails above N), every one done, two of the same folder never overlap, the tally is right under `-race`, a failure in one doesn't stop the rest, `--jobs 1` is exactly today's order, an interrupted run says what was left. One real run to finish it, with the timings.
- **Not decided:** whether the number should adapt (back off on a failure that looks like the daemon being overloaded). Start with a fixed number; the measurement will say whether anything more is needed.
