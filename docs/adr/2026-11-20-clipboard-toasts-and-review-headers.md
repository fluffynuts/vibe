# Less room for what's said: the clipboard page's status line becomes toasts, and a Review file's header stays one line

- **Status:** accepted, built 2026-11-20 on `feat/status-balloon` (shipped with the status balloon, as "UI enhancements"). Tried by hand by
  the user the same day (`./test-clean` for the clipboard page): the toasts, the page's gaps, the one-line headers, going to a comment.
- **Decided:** 2026-11-20, with the user; the toast's live regions, the count of pastes on their way, and the toast as a button came from
  the branch's review.
- **Came from:** the last sections of `docs/plan-status-balloon.md`, now deleted: `git show 332c1f8:docs/plan-status-balloon.md`.

## The clipboard page's status line

**Context:** a line under the drop area (`#status`) said what was just done. Mostly empty, it still took its height above the list, and what
it said stayed until something replaced it ("Copied to your clipboard." lingered). It wasn't given a balloon: a balloon is for what is going
on, and these are the outcomes of one action.

**Decision:** the line is gone, and the page's toast says what it did:
- what was just done ("Copied…", "Already pasted…"): 3 s, as "Deleted." already was;
- a problem ("Couldn't add that…", "Not kept…", "The clipboard had nothing…"): 8 s, in the problem colour;
- the one to act on (open the page as `localhost`, not by IP address): until dismissed;
- "Sending N items…": while pastes are on their way, counting every paste, gone when the last lands (not over the empty-file warning);
- any toast goes when clicked. It is a button ("Click to dismiss"), so the one that stays can be dismissed from the keyboard: Escape
  would reach it only with focus in the clipboard's frame, seldom where it is.
- It is announced from two live regions that are always there, an outcome politely and a problem as an alert, and cleared when it goes:
  changing one element's role as its words change is heard unreliably.

The held box (pastes waiting for the server) is state, not an outcome, and stays. The page's gaps are all the gap between cards (12px).

## A Review file's header

**Context:** a long path, with the reading order's label, wrapped a file's header onto two lines, and going to a comment in that file
(Ctrl+Enter, the arrows) left the comment's top under the header: only the first file's header was measured.

**Decision:**
- **One line.** The path is its folders and its name, each the header's own item: the folders, the label and the "not coloured" note share
  what the rest leaves, giving way when there isn't enough (the folders from the left, `…vibe/name.html`, so the folder nearest the name
  stays; the others from their ends). The name never gives way: on a window too narrow even for it, the header wraps. All are whole in their
  titles, and the path is still there to select or find. The tree shows where the file is.
- **Going to a comment allows for its own file's header**, which can still wrap on a narrow window; which comment the page is at counts from
  under the tallest header.

## Not verified

The toasts with a screen reader; whether clicking a toast to dismiss it is obvious enough (the user is living with it to see); the left-cut
path in Safari and Chromium-based browsers (Firefox's tests check it).
