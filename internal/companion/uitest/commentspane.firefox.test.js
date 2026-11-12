// The comments pane's cards in a real Firefox (Playwright's), for their layout, which jsdom can't show. Run by
// `node --test` from firefox_test.go (TestTheCommentsPaneInFirefox), which serves the page (VIBE_PAGE_URL) for a
// repository with a long-pathed file the agent has commented on (so its thread waits on the user's answer), and a short
// one the user has.
// Needs `npm ci` and `npx playwright install firefox` in this folder.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { firefox } = require('playwright')

const URL = process.env.VIBE_PAGE_URL

let browser
test.before(async () => { browser = await firefox.launch() })
test.after(async () => { if (browser) await browser.close() })

async function commentsPane() {
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } })
  await page.goto(URL)
  await page.click('#tab-review')
  await page.click('#review-comments-open')
  await page.waitForSelector('#review-comments-list .comment-card .pill', { state: 'visible' })
  // The pane slides in: what is measured on the way in is somewhere it won't stay.
  await page.locator('#review-comments').evaluate((el) => Promise.all(el.getAnimations({ subtree: true }).map((a) => a.finished)))
  return page
}

// Beside a long path, the pill used to be squeezed into a few lines at the card's edge. It is on a line of its own
// above the path, whole, and the card with nothing to say has no such line.
test("a card's state is one line, above the path, and not squeezed by a long one", async () => {
  const page = await commentsPane()
  try {
    // Measured in one step inside the page: the pane draws its cards afresh on every poll of the review, and a
    // handle held across that (as a locator's boundingBox is) comes back null when the redraw falls between two steps,
    // which a slow machine makes likely.
    const m = await page.evaluate(() => {
      const rect = (el) => { const r = el.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height } }
      const awaiting = document.querySelector('#review-comments-list .comment-card.awaiting')
      const pills = awaiting.querySelectorAll('.comment-card-state .pill')
      const pill = pills[0]
      const probe = pill.cloneNode(true)
      probe.style.cssText = 'position:absolute;visibility:hidden;white-space:nowrap;width:auto'
      pill.parentNode.appendChild(probe)
      const natural = probe.getBoundingClientRect().width
      probe.remove()
      return {
        pills: pills.length,
        pill: rect(pill),
        path: rect(awaiting.querySelector('.comment-card-where code')),
        natural,
        stateLinesOnCardsWithNothingToSay: document.querySelectorAll('.comment-card:not(.awaiting) .comment-card-state').length,
      }
    })
    assert.equal(m.pills, 1, 'a card waiting on you has its state on a line of its own')
    assert.ok(m.pill.y + m.pill.height <= m.path.y + 1, `the pill (${JSON.stringify(m.pill)}) isn't above the path (${JSON.stringify(m.path)})`)
    assert.ok(m.pill.height < 26, `the pill is ${m.pill.height}px tall: it has wrapped onto more than one line`)
    // Its text is all of "awaiting your answer", on that one line: as wide as the words take.
    assert.ok(Math.abs(m.pill.width - m.natural) < 2, `the pill is ${m.pill.width}px wide, and its words take ${m.natural}px`)
    // The path starts where the pill does: under it, not indented past it.
    assert.ok(Math.abs(m.path.x - m.pill.x) < 1, `the path starts at ${m.path.x} and the pill at ${m.pill.x}`)
    // A card with nothing to say has no line for it.
    assert.equal(m.stateLinesOnCardsWithNothingToSay, 0)
  } finally { await page.close() }
})
