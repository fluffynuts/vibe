// Tests of the companion page in a real Firefox (Playwright's), for what
// jsdom can't show: layout, hover, and how a real browser hands out pointer
// events. Run by `node --test` from firefox_test.go, which serves the page
// (VIBE_PAGE_URL) for a repository where a.txt's lines 20-22 are changed.
// Needs `npm ci` and `npx playwright install firefox` in this folder.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { firefox } = require('playwright')

const URL = process.env.VIBE_PAGE_URL
const plusSel = (label) => `[aria-label="Comment on line ${label}"]`

let browser
test.before(async () => { browser = await firefox.launch() })
test.after(async () => { if (browser) await browser.close() })

// reviewPage opens the page on its Review tab, the diff drawn.
async function reviewPage() {
  const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } })
  await page.goto(URL)
  await page.click('#tab-review')
  await page.waitForSelector(plusSel('R20'), { state: 'attached' })
  return page
}

// hoverPlus hovers a line's number, where its + shows, and gives the +.
async function hoverPlus(page, label) {
  const plus = page.locator(plusSel(label))
  await plus.evaluate((el) => el.closest('td').scrollIntoView({ block: 'center' }))
  await page.hover(`td:has(> ${plusSel(label)})`)
  return plus
}

const composerSays = (page) => page.locator('.review-compose-head').textContent()

test('Shift-click makes a range on one side', async () => {
  const page = await reviewPage()
  try {
    await (await hoverPlus(page, 'R20')).click()
    await (await hoverPlus(page, 'R22')).click({ modifiers: ['Shift'] })
    assert.equal(await composerSays(page), 'Add a comment on lines R20–R22')
  } finally { await page.close() }
})

// An unchanged line's + to click is the left column's, the old file's; a
// changed line's new + is in the middle column.
test('Shift-click from the left column to the middle one ranges on the side both rows share', async () => {
  const page = await reviewPage()
  try {
    await (await hoverPlus(page, 'L18')).click()
    await (await hoverPlus(page, 'R22')).click({ modifiers: ['Shift'] })
    assert.equal(await composerSays(page), 'Add a comment on lines R18–R22')
  } finally { await page.close() }
})

test("dragging one + onto another moves the comment being written, keeping its text", async () => {
  const page = await reviewPage()
  try {
    await (await hoverPlus(page, 'R20')).click()
    await page.fill('.review-compose-text', 'kept')
    const from = await (await hoverPlus(page, 'R20')).boundingBox()
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
    await page.mouse.down()
    const to = await (await hoverPlus(page, 'R23')).boundingBox()
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 5 })
    await page.mouse.up()
    assert.equal(await composerSays(page), 'Add a comment on line R23')
    assert.equal(await page.inputValue('.review-compose-text'), 'kept')
  } finally { await page.close() }
})

// The lines a comment is on stand out on their own side, over a changed
// line's colour too; the other side is left alone.
test("a range's lines are marked on their side, over the colour of changed lines", async () => {
  const page = await reviewPage()
  try {
    await (await hoverPlus(page, 'R20')).click()
    await (await hoverPlus(page, 'R21')).click({ modifiers: ['Shift'] })
    // The code cells of new lines 21 (in the range) and 22 (not), both
    // added, and of old line 21, beside the first.
    const look = (side, line) => page.evaluate(([side, line]) => {
      const plus = document.querySelector(`[aria-label="Comment on line ${side === 'new' ? 'R' : 'L'}${line}"]`)
      const num = plus.closest('td')
      const code = num.nextElementSibling.nextElementSibling
      const css = (el) => { const s = getComputedStyle(el); return s.backgroundColor + ' ' + s.backgroundImage + ' ' + s.boxShadow }
      return { num: css(num), code: css(code) }
    }, [side, line])
    const inRange = await look('new', 21)
    const outside = await look('new', 22)
    const otherSide = await look('old', 21)
    const otherSideOutside = await look('old', 22)
    assert.notEqual(inRange.code, outside.code, 'the code stands out')
    assert.notEqual(inRange.num, outside.num, 'and its number')
    assert.equal(otherSide.code, otherSideOutside.code, 'the old side is left alone')
  } finally { await page.close() }
})
