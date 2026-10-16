---
name: vibe-review
description: Review code changes for the user, with comments on the companion page's Review tab. Use when the user asks for a review of their changes, a branch, or a commit, or to read and answer their replies to one.
---

# Reviewing on the Review tab

The user reads reviews on the companion page's **Review** tab: the diff, with
your comments under the lines they're about, in threads they can reply to,
resolve or dismiss. You make the review with `vibe-review` (run it alone for
its usage).

## 1. Start

Say what the review is of, before anything else: the tab then shows it as in
progress, so the user doesn't take a review still being written for one
that found nothing.

    vibe-review start --note "<what this change is, in a few words>"

Uncommitted changes (the default), or `--ref <branch>` for a branch's
changes since it parted from that one, `--commit <sha>` for one commit, or
`--unpushed` for the commits not yet pushed to the branch's upstream.
Starting again replaces the last review (it's kept in the folder's history).

When the user started the review from the Review tab, the prompt says so:
then it's already started, so don't run `start` (it would replace theirs);
begin at step 2.

## 2. Read

    vibe-review diff

is exactly the diff the tab shows: review that, so your line numbers match
what the user sees. Read each changed file in full, not just its hunks, and
check callers of anything whose behaviour or signature changed. Read the
project's CLAUDE.md files: their rules are part of what you review against.

Look for, most serious first: what will break or give wrong results;
security and data loss; reliability (races, swallowed errors, hidden
dependence on time or environment); missing or weak tests for new
behaviour; then design and naming, and names or comments that no longer
match what the code does. Check a finding against the surrounding code
before you write it: many apparent bugs vanish in context. Leave out
style a linter would catch, and problems in code the change didn't touch.

## 3. Comment

One comment per finding, on the lines it's about:

    vibe-review comment --file <path> --line <n> [--end-line <n>] --body-file - <<'BODY'
    P2: <the problem>. <what it leads to>. <the fix>.
    BODY

- `--line` is the line's number in the new file; for a deleted line, add
  `--side old` and use its old number.
- Start with the severity: **P1** (must not merge: broken, insecure, loses
  data), **P2** (should fix: reliability, missing tests, real risk),
  **P3** (worth doing; mark a nit as a nit). Don't inflate.
- Lead with the problem, in two or three sentences: the problem, its
  consequence, the fix. A one- or two-line fix can go in a code block.
- Use `--body-file -` with a quoted heredoc, as above, for anything with
  quotes, backticks or several lines.
- A pattern rather than a one-off: comment on its first occurrence, say
  it's a pattern, and suggest a mechanical fix (a lint rule, a type, a
  test) rather than listing every instance.

Then, unless there are only one or two findings, one general comment: the
verdict and the themes, in at most five sentences, no recap and no praise.
With no findings, say so in one line.

    vibe-review general --body "<verdict>"

## 4. Set the reading order

Unless only one file changed, the order to read the change in: what
explains the rest first, its callers after, mechanical changes last.

    vibe-review order <<'ORDER'
    [{"file": "src/core.go", "line": 42, "label": "the primitive", "body": "what to understand here"},
     {"file": "src/use.go", "line": 10, "label": "first caller"}]
    ORDER

`label` is a few words on why the file is read at that point.

## 5. Finish

    vibe-review done

Always, including when you found nothing or stopped early: until then the
tab says the review is still in progress. Then tell the user it's on the
Review tab, with the counts by severity.

## Replies

The user answers on the tab. `vibe-review list` shows the review with every
reply. Answer with `vibe-review reply --thread <n> --body ...`, and
`vibe-review resolve --thread <n>` (with a body saying what was done) when
a finding has been dealt with. A review is read-only: don't change code
unless the user asks.
