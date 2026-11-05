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
    const awaiting = page.locator('.comment-card.awaiting').first()
    assert.equal(await awaiting.locator('.comment-card-state .pill').count(), 1, 'a card waiting on you has its state on a line of its own')
    const pill = await awaiting.locator('.comment-card-state .pill').boundingBox()
    const path = await awaiting.locator('.comment-card-where code').boundingBox()
    assert.ok(pill.y + pill.height <= path.y + 1, `the pill (${JSON.stringify(pill)}) isn't above the path (${JSON.stringify(path)})`)
    assert.ok(pill.height < 26, `the pill is ${pill.height}px tall: it has wrapped onto more than one line`)
    // Its text is all of "awaiting your answer", on that one line: as wide as the words take.
    const natural = await awaiting.locator('.comment-card-state .pill').evaluate((el) => {
      const probe = el.cloneNode(true)
      probe.style.cssText = 'position:absolute;visibility:hidden;white-space:nowrap;width:auto'
      el.parentNode.appendChild(probe)
      const w = probe.getBoundingClientRect().width
      probe.remove()
      return w
    })
    assert.ok(Math.abs(pill.width - natural) < 2, `the pill is ${pill.width}px wide, and its words take ${natural}px`)
    // The path starts where the pill does: under it, not indented past it.
    assert.ok(Math.abs(path.x - pill.x) < 1, `the path starts at ${path.x} and the pill at ${pill.x}`)
    // A card with nothing to say has no line for it.
    assert.equal(await page.locator('.comment-card:not(.awaiting) .comment-card-state').count(), 0)
  } finally { await page.close() }
})
