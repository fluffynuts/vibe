// Tests of the companion page in a real Firefox (Playwright's), for what
// jsdom can't show: layout, hover, and how a real browser hands out pointer
// events. Run by `node --test` from firefox_test.go, which serves the page
// (VIBE_PAGE_URL) for a repository where a.txt's lines 20-22 are changed,
// and main.go's line 4.
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

// Code is coloured once its file is near the screen, and the colours show.
// Colours arrive a file at a time, each redrawing the diff: so each step
// here is one call into the page, finding what it needs as it then is.
// (Polled from here: the page's policy forbids the eval Playwright's own
// waiting uses.)
test('code is coloured by its language', async () => {
  const page = await reviewPage()
  try {
    await page.evaluate(() => document.querySelector('.review-file[data-path="main.go"]').scrollIntoView())
    const look = () => page.evaluate(() => {
      const kw = document.querySelector('.review-file[data-path="main.go"] .hl-kw')
      return kw && [getComputedStyle(kw).color, getComputedStyle(kw.closest('td')).color]
    })
    let colours = null
    for (let i = 0; i < 100 && !(colours = await look()); i++) await page.waitForTimeout(50)
    assert.ok(colours, 'main.go was coloured within 5s')
    assert.notEqual(colours[0], colours[1], 'a keyword stands out from plain code')
  } finally { await page.close() }
})

// The comments pane slides in over the diff, below the review's bar: the
// diff isn't resized for it.
test('the comments pane slides in over the diff, without resizing it', async () => {
  const page = await reviewPage()
  try {
    const box = (sel) => page.evaluate((sel) => { const r = document.querySelector(sel).getBoundingClientRect(); return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width } }, sel)
    const before = await box('.review-main')
    await page.click('#review-comments-open')
    let pane = null
    for (let i = 0; i < 40; i++) {
      pane = await box('#review-comments')
      if (Math.abs(pane.right - 1400) < 1) break
      await page.waitForTimeout(50)
    }
    assert.ok(Math.abs(pane.right - 1400) < 1, 'flush with the right edge once in: ' + JSON.stringify(pane))
    const bar = await box('#panel-review > .bar')
    assert.ok(pane.top >= bar.bottom - 1, 'below the bar')
    assert.equal((await box('.review-main')).width, before.width, 'the diff not resized')
  } finally { await page.close() }
})

// A toast is never hidden: not by the comments pane, nor a dialog.
test('a toast shows over the comments pane', async () => {
  const page = await reviewPage()
  try {
    await page.click('#review-comments-open')
    // Once it's all the way in.
    for (let i = 0; i < 40 && Math.abs(await page.evaluate(() => document.getElementById('review-comments').getBoundingClientRect().right) - 1400) >= 1; i++) await page.waitForTimeout(50)
    // A paste with no clipboard page to take it: the page says so, in a toast.
    // (Firefox ignores data given to a made-up paste event: it's set on it.)
    await page.evaluate(() => {
      const ev = new Event('paste', { bubbles: true, cancelable: true })
      Object.defineProperty(ev, 'clipboardData', { value: { files: [new File(['png'], 'shot.png', { type: 'image/png' })], getData: () => '' } })
      document.querySelector('#review-files').dispatchEvent(ev)
    })
    const onTop = () => page.evaluate(() => {
      const t = document.getElementById('toast')
      if (t.hidden) return null
      const r = t.getBoundingClientRect()
      const at = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)
      return !!at && (at === t || t.contains(at))
    })
    let seen = null
    for (let i = 0; i < 40 && (seen = await onTop()) === null; i++) await page.waitForTimeout(50)
    assert.equal(seen, true, 'the toast is what shows where it is')
    const where = await page.evaluate(() => { const r = document.getElementById('toast').getBoundingClientRect(); return [Math.round(innerWidth - r.right), Math.round(innerHeight - r.bottom)] })
    assert.deepEqual(where, [16, 16], 'in its place, bottom right')
  } finally { await page.close() }
})

// conversationPage opens the page on its Conversation tab, the model and
// effort drop-downs drawn, at width pixels wide.
async function conversationPage(width) {
  const page = await browser.newPage({ viewport: { width, height: 900 } })
  await page.goto(URL)
  await page.click('#tab-conversation')
  await page.waitForSelector('#choice-model option:nth-child(3)', { state: 'attached' })
  return page
}

const box = (page, sel) => page.locator(sel).boundingBox()

test('the model and effort stay where they are while Stop comes and goes, wide and narrow', async () => {
  for (const width of [1500, 1000, 600]) {
    const page = await conversationPage(width)
    try {
      const before = await box(page, '#convo-choice')
      await page.evaluate(() => { document.getElementById('convo-stop').hidden = false })
      const during = await box(page, '#convo-choice')
      assert.deepEqual(during, before, `at ${width}px wide, Stop appearing moved them`)
      const send = await box(page, '#convo-send')
      const stop = await box(page, '#convo-stop')
      assert.ok(stop.x + stop.width <= during.x || stop.y >= during.y + during.height, `at ${width}px, Stop overlaps them`)
      assert.ok(send.x + send.width <= page.viewportSize().width, `at ${width}px, Send is off the page`)
    } finally { await page.close() }
  }
})

test('the model and effort are centred under the prompt when there is room, and on a row of their own when there is not', async () => {
  const wide = await conversationPage(1500)
  try {
    const form = await box(wide, '#convo-form')
    const group = await box(wide, '#convo-choice')
    assert.ok(Math.abs((group.x + group.width / 2) - (form.x + form.width / 2)) <= 2, 'centred in the form')
    const send = await box(wide, '#convo-send')
    assert.ok(Math.abs(group.y + group.height / 2 - (send.y + send.height / 2)) <= 12, 'on the same row as Send')
  } finally { await wide.close() }
  const narrow = await conversationPage(600)
  try {
    const form = await box(narrow, '#convo-form')
    const group = await box(narrow, '#convo-choice')
    const send = await box(narrow, '#convo-send')
    assert.ok(group.y + group.height <= send.y, 'above Send')
    assert.ok(Math.abs((group.x + group.width / 2) - (form.x + form.width / 2)) <= 2, 'centred in the form')
    assert.ok(group.x >= 0 && group.x + group.width <= 600, 'on the page')
  } finally { await narrow.close() }
})

test('the bubble over the info button moves nothing, and stays on the page', async () => {
  for (const width of [1500, 600]) {
    const page = await conversationPage(width)
    try {
      const watched = ['#convo-input', '#convo-send', '#choice-model', '#choice-ran', '#convo-log']
      const before = await Promise.all(watched.map((sel) => box(page, sel)))
      await page.click('#choice-info')
      await page.waitForSelector('#choice-bubble:not([hidden])')
      const after = await Promise.all(watched.map((sel) => box(page, sel)))
      assert.deepEqual(after, before, `at ${width}px wide, opening the bubble moved something`)
      const bubble = await box(page, '#choice-bubble')
      assert.ok(bubble.x >= 0 && bubble.x + bubble.width <= width, `at ${width}px, the bubble runs off the page: ${JSON.stringify(bubble)}`)
      assert.ok(bubble.y >= 0, `at ${width}px, the bubble runs off the top`)
    } finally { await page.close() }
  }
})

test('the drop-downs are the same size whatever they hold, so the lists arriving moves nothing', async () => {
  const page = await conversationPage(1500)
  try {
    const sizes = () => Promise.all(['#choice-model', '#choice-effort', '#convo-choice'].map((sel) => box(page, sel)))
    const full = await sizes()
    await page.evaluate(() => {
      for (const id of ['choice-model', 'choice-effort']) {
        const select = document.getElementById(id)
        select.replaceChildren(Object.assign(document.createElement('option'), { value: '', textContent: 'Default' }))
      }
    })
    const bare = await sizes()
    assert.deepEqual(bare.map((b) => b.width), full.map((b) => b.width), 'what they hold changed how wide they are')
    const group = bare[2]
    assert.deepEqual(await box(page, '#convo-choice'), group)
  } finally { await page.close() }
})

test('the spinner coming and going moves nothing in the widget', async () => {
  for (const width of [1500, 600]) {
    const page = await conversationPage(width)
    try {
      const watched = ['#convo-choice', '#choice-model', '#choice-effort', '#choice-info', '#convo-send']
      const read = () => Promise.all(watched.map((sel) => box(page, sel)))
      await page.evaluate(() => { document.getElementById('choice-spinner').hidden = true })
      const without = await read()
      await page.evaluate(() => { document.getElementById('choice-spinner').hidden = false })
      const spinning = await read()
      assert.deepEqual(spinning, without, `at ${width}px wide, the spinner moved something`)
      const spinner = await box(page, '#choice-spinner')
      const model = await box(page, '#choice-model')
      assert.ok(spinner.x + spinner.width <= model.x, `at ${width}px, the spinner isn't to the left of the model drop-down`)
    } finally { await page.close() }
  }
})
