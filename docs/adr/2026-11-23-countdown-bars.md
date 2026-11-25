# What closes itself shows its time running out; the balloon closes when emptied

- **Status:** accepted, built 2026-11-23 on `feat/bare-pr-numbers`. Tried by hand 2026-11-25: the companion page's and the clipboard page's toasts count down, hold while hovered, and start again from full; the rest is left to the tests.
- **Decided:** 2026-11-23, with the user.
- **Came from:** `docs/plan-bare-pr-numbers.md` (`git show 24c1c9e:docs/plan-bare-pr-numbers.md`).

## A countdown bar

**Context:** toasts and the status balloon close themselves, with nothing to say they will, or when.

- **A 2px line inside the top edge** of what closes itself, clear of its rounded corners, shrinking to nothing (anchored left) as its time
  runs out: the page's toasts (4 s, 8 s for a problem, 10 s with an action), the status balloon when it popped up of itself (5 s), and the
  clipboard page's toasts. Nothing that stays has one: the clipboard page's "Sending…", a balloon the user opened, one with a warning.
- **One length:** the bar's duration is a CSS variable (`--countdown-ms`) set from the number its timer uses, so they can't disagree.
- **Held, it stops:** hovering or focusing holds it, its bar full and still; let go, timer and bar start again from full. The balloon and
  the clipboard page's toasts gained the hold the page's toasts had.
- **Reduced motion:** still shown, as the slow linear shrink it is: it says something, it isn't decoration.
- jsdom runs no animations: a Firefox test reads the bar's animation, its place, and its width halfway through.

## The status balloon closes when its list empties

Amends `2026-11-18-status-balloon.md`: opened by the user, it stayed open once its work had ended, saying only "Work the agent is doing in
the background will appear here." Now, whenever the list goes from something to nothing, it closes however it was opened, focus inside it
going back to its button (as after Stop). Opened while already empty, it still says what it is for.

## A balloon the user opened stays open through news

Found on the way, fixed with the user's go-ahead: a balloon the user opened turned into one that closes itself when its list changed without
emptying (a task ending while another runs is news, which popped it up again, as if of itself). As `2026-11-18-status-balloon.md` says, one
the user opened now stays until they close it, or its list empties; news shows in it, with no countdown.
