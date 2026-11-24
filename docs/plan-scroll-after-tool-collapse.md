# The conversation left scrolled up after a tool group collapses (to investigate, not decided)

Raised 2026-11-24 by the user, seen a few times: the agent runs tools, the group of tool uses collapses when it is done (as
designed), and the log is left scrolled a little up, the "talking" dots below the fold, though the user hadn't scrolled away.

## Wanted

While the dots show and the user hasn't scrolled up on purpose, they stay in view at the bottom of the log. The check: if the
dots were in view before the collapse, they are after it. Only while the dots are still in the log: once the turn has
ended and they're gone, nothing is held in view (holding the last place would pin the log to the agent's last tool use, the
user's point). Whatever is written last decides, as now.

## Where to look (read from the code 2026-11-24, not yet reproduced)

- `endToolGroup` (`app.js`) sets `group.open = false` without asking `nearBottom(log)` first or scrolling to the end after.
  Every other change to the log (`showTyping`, appending events) takes `stick = nearBottom(log)` before and scrolls after.
- Collapsing shrinks the log; the browser clamps `scrollTop`, which should land at the bottom, unless something else is drawn in
  the same turn and measured between the two (e.g. the next event appended with `stick` read while the group was still open
  and the log scrolled to the old bottom, or the dots moved before the collapse). The order of collapse / append / `stick` reads
  is the first thing to check.
- `nearBottom` allows 80 px: a collapse bigger than that, read after, says "not near the bottom" and nothing follows.

## Tests first

- jsdom can't lay out: give the log a stand-in `scrollHeight`/`clientHeight` (as the other scroll tests in `page.test.js` do)
  and pin that the log is scrolled to its end after a group collapses when it was at the end before, and left alone when the
  user had scrolled up, and nothing held once the dots are gone.
- Firefox: a real collapse of a tall group with the dots showing leaves the dots in view.
