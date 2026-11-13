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

// stateWith serves /api/state as the server answers it, with patch merged in. A poll still being fetched when a test closes its page has the
// response disposed under it ("Response has been disposed"): that is the end of the test, not a failure, and left to throw it was an unhandled
// rejection that failed a whole CI run (Windows) in which every test had passed. Any other error is still the test's.
const stateWith = (page, patch) => async (route) => {
  try {
    const state = await (await route.fetch()).json()
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify(Object.assign(state, patch)) })
  } catch (e) {
    if (!page.isClosed()) throw e
  }
}

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

// Stop is shown and every box read in one turn of the page: its conversation poll hides Stop again whenever the agent isn't
// running (every 700 ms), so a poll that landed between showing it and a later measurement left it unrendered, and its box
// null (seen on a slow CI runner).
const stopShownAndMeasured = (page) => page.evaluate(() => {
  const rect = (id) => { const r = document.getElementById(id).getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height } }
  const before = rect('convo-choice')
  document.getElementById('convo-stop').hidden = false
  return { before, during: rect('convo-choice'), send: rect('convo-send'), stop: rect('convo-stop') }
})

test('the model and effort stay where they are while Stop comes and goes, wide and narrow', async () => {
  for (const width of [1500, 1000, 600]) {
    const page = await conversationPage(width)
    try {
      const { before, during, send, stop } = await stopShownAndMeasured(page)
      assert.deepEqual(during, before, `at ${width}px wide, Stop appearing moved them`)
      assert.ok(stop.width > 0 && stop.height > 0, `at ${width}px, Stop wasn't drawn`)
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
      const watched = ['#convo-input', '#convo-send', '#choice-model-box', '#choice-ran', '#convo-log']
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
    const sizes = () => Promise.all(['#choice-model-box .fs-trigger', '#choice-effort', '#convo-choice'].map((sel) => box(page, sel)))
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
      const watched = ['#convo-choice', '#choice-model-box', '#choice-effort', '#choice-info', '#convo-send']
      const read = () => Promise.all(watched.map((sel) => box(page, sel)))
      await page.evaluate(() => { document.getElementById('choice-spinner').hidden = true })
      const without = await read()
      await page.evaluate(() => { document.getElementById('choice-spinner').hidden = false })
      const spinning = await read()
      assert.deepEqual(spinning, without, `at ${width}px wide, the spinner moved something`)
      const spinner = await box(page, '#choice-spinner')
      const model = await box(page, '#choice-model-box')
      assert.ok(spinner.x + spinner.width <= model.x, `at ${width}px, the spinner isn't to the left of the model drop-down`)
    } finally { await page.close() }
  }
})

// The model panel opens upwards from the foot of the page, and has to stay on the page whatever its width; typing narrows it.
test('the model panel opens on the page at any width, narrows as it is typed into, and closes with Escape', async () => {
  for (const width of [1500, 600]) {
    const page = await conversationPage(width)
    try {
      await page.click('#choice-model-box .fs-trigger')
      await page.waitForSelector('#choice-model-box .fs-panel:not([hidden])')
      const panel = await box(page, '#choice-model-box .fs-panel')
      assert.ok(panel.x >= 0 && panel.x + panel.width <= width, `at ${width}px, the panel runs off the side: ${JSON.stringify(panel)}`)
      assert.ok(panel.y >= 0, `at ${width}px, the panel runs off the top: ${JSON.stringify(panel)}`)
      const trigger = await box(page, '#choice-model-box .fs-trigger')
      assert.ok(panel.y + panel.height <= trigger.y + 1, `at ${width}px, the panel isn't above the button it opened from`)
      const all = await page.locator('#choice-model-box .fs-opt:visible').count()
      const texts = await page.locator('#choice-model-box .fs-opt').allTextContents()
      await page.keyboard.type('haiku')
      const narrowed = await page.locator('#choice-model-box .fs-opt:visible').count()
      assert.ok(narrowed >= 1 && narrowed < all, `typing didn't narrow the list (${all} then ${narrowed}); the models: ${JSON.stringify(texts)}; the filter has ${JSON.stringify(await page.inputValue('#choice-model-box .fs-filter'))}`)
      await page.keyboard.press('Escape')
      assert.ok(await page.locator('#choice-model-box .fs-panel').isHidden(), 'Escape left the panel open')
      assert.equal(await page.evaluate(() => document.activeElement.id), 'choice-model-button')
    } finally { await page.close() }
  }
})

// Arrowing through a long list has to keep the row it is on in view (jsdom has no scrolling, so only a browser can say).
test('arrowing down a long model list keeps the active row inside the list', async () => {
  const page = await conversationPage(1500)
  try {
    await page.evaluate(() => {
      const select = document.getElementById('choice-model')
      for (let i = 0; i < 40; i++) select.append(Object.assign(document.createElement('option'), { value: 'vendor/model-' + i, textContent: 'Vendor model ' + i }))
    })
    await page.click('#choice-model-box .fs-trigger')
    await page.waitForSelector('#choice-model-box .fs-opt:nth-of-type(30)', { state: 'attached' })
    for (let i = 0; i < 30; i++) {
      await page.keyboard.press('ArrowDown')
      const rows = await page.evaluate(() => {
        const list = document.querySelector('#choice-model-box .fs-list').getBoundingClientRect()
        const row = document.querySelector('#choice-model-box .fs-opt.active').getBoundingClientRect()
        return { listTop: list.top, listBottom: list.bottom, top: row.top, bottom: row.bottom }
      })
      assert.ok(rows.top >= rows.listTop - 1 && rows.bottom <= rows.listBottom + 1, `after ${i + 1} presses, the active row is outside the list: ${JSON.stringify(rows)}`)
    }
  } finally { await page.close() }
})

// A choice is saved with the control disabled; the keyboard user's place in the page must survive that.
test('after choosing with Enter, focus is on the model button, and Tab goes on to the effort drop-down', async () => {
  const page = await conversationPage(1500)
  try {
    await page.click('#choice-model-box .fs-trigger')
    await page.keyboard.type('haiku')
    await page.keyboard.press('Enter')
    // Saved when the button is let be used again (polled: the page's policy forbids waitForFunction).
    for (let i = 0; i < 60 && await page.evaluate(() => document.getElementById('choice-model-button').disabled); i++) await page.waitForTimeout(50)
    assert.ok(!await page.evaluate(() => document.getElementById('choice-model-button').disabled), 'the choice was never saved')
    const focused = await page.evaluate(() => document.activeElement.id || document.activeElement.tagName)
    assert.equal(focused, 'choice-model-button', `focus was lost after the choice was kept: it is on ${focused}`)
    await page.keyboard.press('Tab')
    assert.equal(await page.evaluate(() => document.activeElement.id), 'choice-effort')
  } finally { await page.close() }
})

test('the longest everyday entries fit their drop-downs without being cut off', async () => {
  const page = await conversationPage(1500)
  try {
    const fits = await page.evaluate(() => {
      const measure = document.createElement('canvas').getContext('2d')
      const out = {}
      for (const [id, texts] of [['choice-model', ['Default (Opus 5.5)', 'Sonnet 5.5', 'Fable 5.1', 'Haiku 5.5']], ['choice-effort', ['Default effort', 'Medium effort', 'Extra High effort']]]) {
        // The model drop-down is a button over the native select (see syncFilterSelect).
        const select = id === 'choice-model' ? document.querySelector('#choice-model-box .fs-trigger') : document.getElementById(id)
        const style = getComputedStyle(select)
        measure.font = style.font
        // room for the text: the box, less its padding and the arrow the browser draws
        const room = select.getBoundingClientRect().width - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) - 26
        out[id] = texts.map((t) => ({ t, width: Math.round(measure.measureText(t).width), room: Math.round(room) }))
      }
      return out
    })
    for (const rows of Object.values(fits)) for (const r of rows) assert.ok(r.width <= r.room, `"${r.t}" is ${r.width}px wide, and has ${r.room}px`)
  } finally { await page.close() }
})

test('Send stays on one line at every width, wherever the widget is on the row or above it', async () => {
  for (const width of [520, 700, 900, 1000, 1100, 1200, 1300, 1400, 1500, 1700, 1900]) {
    const page = await conversationPage(width)
    try {
      const send = await box(page, '#convo-send')
      const oneLine = await page.evaluate(() => {
        const b = document.getElementById('convo-send')
        return b.getBoundingClientRect().height <= parseFloat(getComputedStyle(b).lineHeight || 20) + parseFloat(getComputedStyle(b).paddingTop) * 2 + 8
      })
      assert.ok(oneLine, `at ${width}px wide, Send wraps (it is ${Math.round(send.width)}x${Math.round(send.height)}; the widget is ${Math.round((await box(page, '#convo-choice')).width)}px, the form ${Math.round((await box(page, '#convo-form')).width)}px)`)
      const group = await box(page, '#convo-choice')
      const form = await box(page, '#convo-form')
      assert.ok(group.x >= form.x - 1 && group.x + group.width <= form.x + form.width + 1, `at ${width}px wide, the widget runs out of the form: ${JSON.stringify(group)} in ${JSON.stringify(form)}`)
    } finally { await page.close() }
  }
})

// toolsPage opens the Conversation tab on a conversation that is whatever events() says it is, asked again
// every time the page polls.
async function toolsPage(events, running) {
  const page = await browser.newPage({ viewport: { width: 1100, height: 700 } })
  const at = { events, running }
  await page.route(/\/api\/conversation\?/, (route) => {
    const after = Number(new (require('node:url').URL)(route.request().url()).searchParams.get('after')) || 0
    route.fulfill({ contentType: 'application/json', body: JSON.stringify({ events: at.events.filter((e) => e.seq > after), last: at.events.reduce((m, e) => Math.max(m, e.seq), 0), running: at.running }) })
  })
  await page.goto(URL)
  await page.click('#tab-conversation')
  await page.waitForSelector('#convo-log .msg.agent')
  return Object.assign(page, { at })
}

const sayEv = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text }] } } })
const useEv = (seq) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash', input: { command: 'echo ' + seq } }] } } })
const outEv = (seq) => ({ seq, kind: 'agent', agent: { type: 'user', message: { content: [{ type: 'tool_result', content: 'out ' + seq }] } } })

test('a conversation of many tools is the words of the agent with a line for each group, and the tools open from it', async () => {
  const events = []
  let seq = 1
  for (let i = 1; i <= 12; i++) {
    for (let t = 0; t < 8; t++) { events.push(useEv(seq++), outEv(seq++)) }
    events.push(sayEv(seq++, 'comment number ' + i))
  }
  const page = await toolsPage(events, false)
  try {
    const group = await page.locator('details.tool-group').first().boundingBox()
    assert.ok(group.height < 30, `a group of 8 tools takes ${group.height}px: it should be one line`)
    assert.equal(await page.locator('details.tool-group[open]').count(), 0)
    await page.locator('details.tool-group > summary').first().click()
    assert.equal(await page.locator('details.tool-group[open] .msg.tool').count(), 8)
    assert.equal(await page.locator('details.tool-group > summary').first().textContent(), '⚙ 8 tool uses · Bash ×8')
  } finally { await page.close() }
})

// The emoji list is drawn over the page like the slash commands': above the prompt, on the page, with the characters drawn.
test('the emoji list opens above the prompt, on the page, with each emoji drawn beside its name, at every width', async () => {
  for (const width of [1200, 700, 380]) {
    const page = await browser.newPage({ viewport: { width, height: 800 } })
    try {
      await page.route(/\/api\/state$/, stateWith(page, { conversation: true }))
      await page.route(/\/api\/conversation\?/, (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ events: [], last: 0, running: false }) }))
      await page.goto(URL)
      await page.click('#tab-conversation')
      await page.fill('#convo-input', 'go :roc')
      await page.waitForSelector('#convo-emoji:not([hidden]) li')
      const box = await page.evaluate(() => {
        const list = document.getElementById('convo-emoji').getBoundingClientRect()
        const input = document.getElementById('convo-input').getBoundingClientRect()
        const glyph = document.querySelector('#convo-emoji li .glyph').getBoundingClientRect()
        return { list: { top: list.top, bottom: list.bottom, left: list.left, right: list.right }, inputTop: input.top, glyph: { w: glyph.width, h: glyph.height }, width: innerWidth }
      })
      assert.ok(box.list.left >= 0 && box.list.right <= box.width, `at ${width}px the list is across ${box.list.left}..${box.list.right}`)
      // At the colon, which is on the prompt's first line here: above that line, not over the whole prompt.
      assert.ok(box.list.bottom <= box.inputTop + 24, `at ${width}px the list (to ${box.list.bottom}) covers the prompt (from ${box.inputTop})`)
      assert.ok(box.list.top >= 0, `at ${width}px the list starts off the top of the page: ${box.list.top}`)
      assert.ok(box.glyph.w > 8 && box.glyph.h > 8, `the emoji has no room: ${JSON.stringify(box.glyph)}`)
    } finally { await page.close() }
  }
})

// The emoji list opens at the colon: just above its line, left edge there, and never outside the prompt box.
async function promptPage(width) {
  const page = await browser.newPage({ viewport: { width, height: 800 } })
  await page.route(/\/api\/state$/, stateWith(page, { conversation: true }))
  await page.route(/\/api\/conversation\?/, (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ events: [], last: 0, running: false }) }))
  await page.goto(URL)
  await page.click('#tab-conversation')
  return page
}

// listBox is where the list is, where the prompt is, and how high a line of it is.
const listBox = (page) => page.evaluate(() => {
  const ta = document.getElementById('convo-input')
  const t = ta.getBoundingClientRect()
  const l = document.getElementById('convo-emoji').getBoundingClientRect()
  const cs = getComputedStyle(ta)
  return {
    list: { left: l.left, right: l.right, top: l.top, bottom: l.bottom },
    ta: { left: t.left, right: t.right, top: t.top, bottom: t.bottom },
    pad: parseFloat(cs.paddingTop) + parseFloat(cs.borderTopWidth), line: parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.2,
  }
})

test('the emoji list opens at the colon, not at the left of the prompt', async () => {
  const page = await promptPage(1200)
  try {
    await page.fill('#convo-input', 'a few words before :roc')
    await page.waitForSelector('#convo-emoji:not([hidden]) li')
    const b = await listBox(page)
    assert.ok(b.list.left > b.ta.left + 100, `the list starts at ${b.list.left}, the prompt at ${b.ta.left}: it didn't follow the colon`)
    assert.ok(b.list.left < b.ta.left + 400, `the list starts at ${b.list.left}: far past a colon after a few words`)
  } finally { await page.close() }
})

test('at the right edge of the prompt the emoji list stays inside it, moved left of the colon', async () => {
  for (const width of [1200, 700, 420]) {
    const page = await promptPage(width)
    try {
      // As many x as put the colon about 40px short of the right edge of the first line, measured as the prompt draws them.
      const put = await page.evaluate(() => {
        const ta = document.getElementById('convo-input')
        const cs = getComputedStyle(ta)
        const ctx = document.createElement('canvas').getContext('2d')
        ctx.font = cs.font
        const inner = ta.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)
        let n = 1
        while (ctx.measureText('x'.repeat(n + 1) + ' ').width < inner - 40) n++
        return { n, colonX: ctx.measureText('x'.repeat(n) + ' ').width + parseFloat(cs.paddingLeft) }
      })
      await page.fill('#convo-input', 'x'.repeat(put.n) + ' :roc')
      await page.waitForSelector('#convo-emoji:not([hidden]) li')
      const b = await listBox(page)
      assert.ok(b.list.right <= b.ta.right + 1, `at ${width}px the list reaches ${b.list.right}, past the prompt's ${b.ta.right}`)
      assert.ok(b.list.left >= b.ta.left - 1, `at ${width}px the list starts at ${b.list.left}, before the prompt's ${b.ta.left}`)
      const listWidth = b.list.right - b.list.left
      // The one check that shows the clamping: it must not be skippable. If the colon is not near enough the edge for the list
      // to have run past it, this tests nothing, and is to be fixed by a closer colon, not passed over.
      assert.ok(put.colonX + listWidth > b.ta.right - b.ta.left, `at ${width}px the colon (${put.colonX}) is not near enough the edge for a ${listWidth}px list to run past it: this does not test the clamp`)
      assert.ok(b.list.left < b.ta.left + put.colonX - 10, `at ${width}px the list starts at the colon (${b.list.left}) and would run past the edge: it should have moved left`)
    } finally { await page.close() }
  }
})

test('on a later line of the prompt the emoji list is above that line, not above the prompt', async () => {
  const page = await promptPage(1200)
  try {
    await page.fill('#convo-input', 'first line\nsecond line\nthird :roc')
    await page.waitForSelector('#convo-emoji:not([hidden]) li')
    const b = await listBox(page)
    const thirdLineTop = b.ta.top + b.pad + 2 * b.line
    assert.ok(Math.abs(b.list.bottom - thirdLineTop) < 10, `the list ends at ${b.list.bottom}; the third line starts at ${thirdLineTop}`)
    await page.fill('#convo-input', ':roc')
    await page.waitForSelector('#convo-emoji:not([hidden]) li')
    const first = await listBox(page)
    assert.ok(first.list.bottom < b.list.bottom - b.line, 'the list did not move down with the caret')
  } finally { await page.close() }
})

test('a prompt that has scrolled puts the emoji list at the visible caret', async () => {
  const page = await promptPage(1200)
  try {
    const lines = Array.from({ length: 20 }, (_, i) => 'line ' + (i + 1)).join('\n')
    await page.fill('#convo-input', lines + '\nlast')
    // Typed at the end, as a person does: the prompt scrolls to the caret by itself.
    await page.focus('#convo-input')
    await page.keyboard.press('Control+End')
    await page.keyboard.type(' :roc')
    await page.waitForSelector('#convo-emoji:not([hidden]) li')
    assert.ok(await page.evaluate(() => document.getElementById('convo-input').scrollTop > 0), 'the prompt did not scroll, so this tests nothing')
    const b = await listBox(page)
    assert.ok(b.list.bottom <= b.ta.bottom + 1 && b.list.bottom >= b.ta.top, `the list ends at ${b.list.bottom}, the prompt spans ${b.ta.top}..${b.ta.bottom}: not at the caret in view`)
  } finally { await page.close() }
})

// The browser's own undo, which the page leaves alone: Ctrl+Z, but Cmd+Z on a Mac (where Control+z does nothing: macOS CI failed all four).
const undo = 'ControlOrMeta'

// Picking is an edit like any other: Ctrl+Z gives back what was typed, as it does for a person's own typing.
test('Ctrl+Z after picking an emoji gives back the name that was typed', async () => {
  const page = await promptPage(1200)
  try {
    await page.focus('#convo-input')
    await page.keyboard.type('hello :roc')
    await page.waitForSelector('#convo-emoji:not([hidden]) li')
    await page.keyboard.press('Tab')
    assert.equal(await page.inputValue('#convo-input'), 'hello 🚀 ')
    await page.keyboard.press(`${undo}+z`)
    assert.equal(await page.inputValue('#convo-input'), 'hello :roc')
  } finally { await page.close() }
})

test('Ctrl+Z after a :name: turns into its emoji gives back the name', async () => {
  const page = await promptPage(1200)
  try {
    await page.focus('#convo-input')
    await page.keyboard.type('nice :tada:')
    assert.equal(await page.inputValue('#convo-input'), 'nice 🎉')
    await page.keyboard.press(`${undo}+z`)
    assert.equal(await page.inputValue('#convo-input'), 'nice :tada:')
  } finally { await page.close() }
})

test('Ctrl+Z after picking a slash command gives back what was typed', async () => {
  const page = await promptPage(1200)
  try {
    await page.route(/\/api\/conversation\?/, (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ events: [], last: 0, running: false, commands: ['review', 'rebase'], commandsSeq: 1 }) }))
    await page.reload()
    await page.click('#tab-conversation')
    await page.focus('#convo-input')
    await page.keyboard.type('/rev')
    await page.waitForSelector('#convo-commands:not([hidden]) li')
    await page.keyboard.press('Tab')
    assert.equal(await page.inputValue('#convo-input'), '/review ')
    await page.keyboard.press(`${undo}+z`)
    assert.equal(await page.inputValue('#convo-input'), '/rev')
  } finally { await page.close() }
})

// Emoticons, typed by hand in a real browser: finished by a space or Enter, undone by Ctrl+Z.
test('typing an emoticon and a space gives the emoji, and Ctrl+Z gives the emoticon back', async () => {
  const page = await promptPage(1200)
  try {
    await page.focus('#convo-input')
    await page.keyboard.type('fine :) ')
    assert.equal(await page.inputValue('#convo-input'), 'fine 🙂 ')
    await page.keyboard.press(`${undo}+z`)
    assert.equal(await page.inputValue('#convo-input'), 'fine :) ')
  } finally { await page.close() }
})

test(':D and Enter is the grinning face, and a smiley mid-sentence is left alone until it is finished', async () => {
  const page = await promptPage(1200)
  try {
    await page.focus('#convo-input')
    await page.keyboard.type('great :D')
    await page.waitForSelector('#convo-emoji:not([hidden]) li')
    assert.equal(await page.inputValue('#convo-input'), 'great :D', 'changed before it was finished')
    await page.keyboard.press('Enter')
    assert.equal(await page.inputValue('#convo-input'), 'great 😀 ')
  } finally { await page.close() }
})

test('Enter after :) is the emoji and then a new line', async () => {
  const page = await promptPage(1200)
  try {
    await page.focus('#convo-input')
    await page.keyboard.type('nice :)')
    await page.keyboard.press('Enter')
    assert.equal(await page.inputValue('#convo-input'), 'nice 🙂\n')
  } finally { await page.close() }
})

test('a URL, a path and code are typed as they are, with the setting on', async () => {
  const page = await promptPage(1200)
  try {
    await page.focus('#convo-input')
    await page.keyboard.type('see http://example.com and C:/Users and `a :) ` ok ')
    assert.equal(await page.inputValue('#convo-input'), 'see http://example.com and C:/Users and `a :) ` ok ')
  } finally { await page.close() }
})

// viewerPage opens the Conversation tab on a conversation that mentions the first of files (path -> text), and
// the file viewer open on it, width pixels wide. The host is stubbed: it has a folder to show files from.
async function viewerPage(files, width) {
  const page = await browser.newPage({ viewport: { width, height: 900 } })
  const first = Object.keys(files)[0]
  const events = [{ seq: 1, kind: 'user', text: 'hi' }, sayEv(2, 'See `' + first + '`.')]
  const json = (route, body) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
  const query = (route) => new (require('node:url').URL)(route.request().url()).searchParams
  await page.route(/\/api\/state$/, stateWith(page, { viewFiles: true }))
  await page.route(/\/api\/conversation\?/, (route) => json(route, { events: events.filter((e) => e.seq > (Number(query(route).get('after')) || 0)), last: 2, running: false }))
  await page.route(/\/api\/workspace-files\?/, (route) => json(route, { files: query(route).getAll('path').filter((p) => p in files) }))
  await page.route(/\/api\/workspace-file\?/, (route) => json(route, { path: query(route).get('path'), text: files[query(route).get('path')] }))
  await page.goto(URL)
  await page.click('#tab-conversation')
  await page.waitForSelector('#convo-log button.file-ref')
  await page.click('#convo-log button.file-ref')
  await page.waitForSelector('#file-view-body h1')
  return page
}

const longFile = (title) => '# ' + title + '\n\n' + Array.from({ length: 150 }, (_, i) => 'Paragraph ' + i + ' of ' + title + '.').join('\n\n') + '\n\nOn to [the other](./b.md).'

test("the file viewer's bar is one line from wide to narrow: buttons, then the path between them and the close button", async () => {
  for (const width of [1100, 700, 420]) {
    const page = await viewerPage({ 'docs/a.md': longFile('A'), 'docs/b.md': '# B' }, width)
    try {
      const ids = ['file-view-back', 'file-view-forward', 'file-view-reload', 'file-view-path', 'file-view-copy', 'file-view-close']
      const b = {}
      for (const id of ids) b[id] = await box(page, '#' + id)
      const tops = ids.map((id) => b[id].y + b[id].height / 2)
      assert.ok(Math.max(...tops) - Math.min(...tops) < 8, `at ${width}px the bar is on more than one line: ${tops}`)
      for (let i = 1; i < ids.length; i++) {
        assert.ok(b[ids[i]].x >= b[ids[i - 1]].x + b[ids[i - 1]].width - 1, `at ${width}px ${ids[i]} is not after ${ids[i - 1]}`)
      }
      assert.ok(b['file-view-path'].width > 80, `at ${width}px the path bar is ${b['file-view-path'].width}px`)
      const close = b['file-view-close']
      assert.ok(close.x + close.width <= width, `at ${width}px the close button is off the page`)
      assert.ok(close.width < 40, 'the close button is an icon, not a word')
    } finally { await page.close() }
  }
})

test('the file viewer fills the space between its buttons with the path, however long the path is', async () => {
  const path = 'docs/' + 'a-long-folder-name/'.repeat(5) + 'a.md'
  const page = await viewerPage({ [path]: '# Long' }, 700)
  try {
    const dialog = await box(page, '#file-view')
    const close = await box(page, '#file-view-close')
    const url = await box(page, '#file-view-path')
    assert.ok(close.x + close.width <= dialog.x + dialog.width, 'the close button stays inside the dialog')
    assert.ok(url.x + url.width <= close.x, 'the path stops short of the close button')
    assert.equal(await page.inputValue('#file-view-path'), path, 'and is the whole path, to read by scrolling it')
  } finally { await page.close() }
})

// A path too long for the bar shows its end, the file's name, and cuts the start: two paths that differ only
// in the part cut look the same.
test("a path too long for the file viewer's bar is cut at its start, so the file's name shows", async () => {
  // Longer than the bar shows, so none of what differs is in view.
  const shared = '/shared-folder/and-another/and-a-third-one/and-then-a-fourth-one/the-file-with-a-name.md'
  const bars = []
  for (const start of ['a', 'b']) {
    const page = await viewerPage({ ['docs/' + start.repeat(40) + shared]: '# Long' }, 600)
    try {
      assert.ok(await page.evaluate(() => { const i = document.getElementById('file-view-path'); return i.scrollWidth > i.clientWidth }), 'the path fits the bar: the test shows nothing')
      assert.ok(await page.evaluate(() => { const i = document.getElementById('file-view-path'); return i.scrollLeft + i.clientWidth >= i.scrollWidth - 1 }), 'the bar is at the end of the path as soon as the file is shown')
      bars.push(await page.locator('#file-view-path').screenshot())
    } finally { await page.close() }
  }
  assert.ok(bars[0].equals(bars[1]), 'the bar shows the start of the path, which differs, and not its end')
})

// A bar drawn right to left would put a path's leading dot or underscore at the other end of it: .github/ci.md as github/ci.md.
test("the file viewer's path is drawn left to right, so a leading dot stays where it is", async () => {
  const page = await viewerPage({ '.github/workflows/ci.md': '# CI' }, 600)
  try {
    assert.equal(await page.evaluate(() => getComputedStyle(document.getElementById('file-view-path')).direction), 'ltr')
    assert.equal(await page.inputValue('#file-view-path'), '.github/workflows/ci.md')
  } finally { await page.close() }
})

// The power button is the one thing at the top right that says whether vibe is reached, so the states are told
// apart by brightness and glow as well as by hue (red and green look alike to many), and changing state moves nothing.
test('the power button changes state without moving the bar, and the states differ in glow and brightness, not only in colour', async () => {
  const page = await browser.newPage({ viewport: { width: 1000, height: 700 } })
  try {
    await page.goto(URL)
    const look = (state) => page.evaluate((s) => {
      const link = document.getElementById('link')
      for (const c of ['up', 'down', 'connecting']) link.classList.toggle(c, c === s)
      link.style.animation = 'none'
      const style = getComputedStyle(link)
      const box = link.getBoundingClientRect()
      const bar = document.querySelector('.top').getBoundingClientRect()
      return { glow: style.boxShadow, opacity: style.opacity, box: [box.x, box.y, box.width, box.height], bar: [bar.width, bar.height] }
    }, state)
    const up = await look('up')
    const down = await look('down')
    const connecting = await look('connecting')
    assert.deepEqual(down.box, up.box, 'the button moved between up and down')
    assert.deepEqual(connecting.box, up.box, 'the button moved between up and connecting')
    assert.deepEqual(down.bar, up.bar, 'the bar changed size between up and down')
    assert.notEqual(up.glow, 'none', 'up has no glow')
    assert.equal(down.glow, 'none', 'down glows')
    assert.ok(Number(down.opacity) < Number(up.opacity), `down (${down.opacity}) is not dimmer than up (${up.opacity})`)
  } finally { await page.close() }
})

// The menu opens from the button at the top right, whose room is the page's width: it has to stay on the page, long
// reasons and all, however narrow the page is.
test('the power menu stays on the page at every width, with a long reason beside a disabled item', async () => {
  for (const width of [1000, 600, 360]) {
    const page = await browser.newPage({ viewport: { width, height: 700 } })
    try {
      await page.goto(URL)
      await page.click('#link')
      // After the click, which sets them from what vibe says (nothing, for this page).
      await page.evaluate(() => {
        document.getElementById('power-actions').hidden = false
        const item = document.getElementById('power-restart')
        item.disabled = true
        item.querySelector('.power-why').textContent = ' (a vibe here has a terminal session, which a restart from the page cannot give back: run vibe --restart in a terminal)'
      })
      const box = await page.evaluate(() => {
        const r = document.getElementById('power-menu').getBoundingClientRect()
        return { left: r.left, right: r.right, bottom: r.bottom, width: innerWidth, height: innerHeight }
      })
      assert.ok(box.left >= 0 && box.right <= box.width, `at ${width}px the menu is across ${box.left}..${box.right}`)
      assert.ok(box.bottom <= box.height, `at ${width}px the menu runs off the bottom: ${box.bottom}`)
    } finally { await page.close() }
  }
})

// Every entry runs the width of the menu, so that the one under the pointer is lit across it, not as far as its words.
test('every power menu entry spans the width of the menu', async () => {
  const page = await browser.newPage({ viewport: { width: 1000, height: 700 } })
  try {
    await page.goto(URL)
    await page.click('#link')
    await page.evaluate(() => { document.getElementById('power-actions').hidden = false })
    const widths = await page.evaluate(() => {
      const menu = document.getElementById('power-menu')
      const style = getComputedStyle(menu)
      const inner = menu.getBoundingClientRect().width - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) - parseFloat(style.borderLeftWidth) - parseFloat(style.borderRightWidth)
      const items = [...menu.querySelectorAll('[role=menuitem]')].filter((el) => el.getBoundingClientRect().width > 0)
      return { inner, items: items.map((el) => [el.id || el.textContent.trim(), el.getBoundingClientRect().width]) }
    })
    assert.ok(widths.items.length >= 4, 'the menu shows ' + JSON.stringify(widths.items))
    for (const [name, width] of widths.items) {
      assert.ok(Math.abs(width - widths.inner) < 1, `${name} is ${width}px wide in a menu ${widths.inner}px wide inside`)
    }
  } finally { await page.close() }
})

// The status of the folder's git at the top of the page is read as the command-line prompt's is (powerline's):
// what is dirty, in yellow; what is pushed or kept, in green; what is new, in light blue; what is wrong, in red.
test("the git status's parts have the prompt's colours, in the light and the dark theme", async () => {
  const expected = {
    branch: '--ok', operation: '--bad', staged: '--amber', modified: '--amber', untracked: '--git-untracked',
    conflicted: '--bad', ahead: '--ok', behind: '--ok', stashed: '--ok', noupstream: '--muted',
  }
  for (const scheme of ['light', 'dark']) {
    const page = await browser.newPage({ viewport: { width: 1000, height: 700 }, colorScheme: scheme })
    try {
      await page.goto(URL)
      const seen = await page.evaluate((classes) => {
        const box = document.getElementById('git')
        box.replaceChildren(...Object.keys(classes).map((cls) => Object.assign(document.createElement('span'), { className: cls, textContent: cls })))
        const colourOf = (node) => getComputedStyle(node).color
        const probe = document.createElement('span')
        document.body.append(probe)
        const out = {}
        for (const [cls, variable] of Object.entries(classes)) {
          probe.style.color = 'var(' + variable + ')'
          out[cls] = { got: colourOf(box.querySelector('.' + cls)), want: colourOf(probe) }
        }
        probe.remove()
        return out
      }, expected)
      for (const [cls, colours] of Object.entries(seen)) {
        assert.equal(colours.got, colours.want, `${scheme}: .${cls} is ${colours.got}, not ${expected[cls]} (${colours.want})`)
      }
      // The ones the prompt makes the same are the same, and the ones it makes different are not.
      assert.notEqual(seen.untracked.got, seen.staged.got)
      assert.notEqual(seen.ahead.got, seen.modified.got, 'an unpushed commit reads as an uncommitted file')
    } finally { await page.close() }
  }
})

// The Admin tab's small cards after the wide one (the agent's instructions) share a row, when there is room for three:
// what they say is in settings.test.js, where they are is here.
test('the admin tab puts profile backups, settings and the GitHub token on one row, below the instructions', async () => {
  const page = await browser.newPage({ viewport: { width: 1600, height: 900 } })
  try {
    await page.goto(URL)
    const rects = await page.evaluate(() => {
      document.getElementById('panel-admin').hidden = false
      const rect = (heading) => {
        const h2 = [...document.querySelectorAll('#panel-admin .admin-grid > section.card > h2')].find((h) => h.textContent.trim().startsWith(heading))
        const r = h2.parentElement.getBoundingClientRect()
        return { top: r.top, left: r.left, bottom: r.bottom }
      }
      return { instructions: rect('Agent instructions'), backups: rect('Profile backups'), settings: rect('Settings'), git: rect('GitHub token') }
    })
    assert.ok(Math.abs(rects.backups.top - rects.settings.top) < 1 && Math.abs(rects.settings.top - rects.git.top) < 1,
      `the three aren't on one row: tops ${rects.backups.top}, ${rects.settings.top}, ${rects.git.top}`)
    assert.ok(rects.backups.left < rects.settings.left && rects.settings.left < rects.git.left, 'they are not in that order, left to right')
    assert.ok(rects.instructions.bottom <= rects.backups.top, 'the instructions are not above them')
  } finally { await page.close() }
})

// A side with no line for the other's is hatched. Each of its cells (the number, the sign, the code) paints its own
// copy, so the stripes only join where every copy starts from the same place, and the rows are all the height of the
// pattern: seen as one hatched block, not a band for each row.
test('the hatching of a side with no line is one continuous pattern, from row to row and across the cells of a row', async () => {
  const page = await browser.newPage({ viewport: { width: 1000, height: 700 } })
  try {
    await page.goto(URL)
    await page.evaluate(() => {
      const table = document.createElement('table')
      table.className = 'review-lines'
      table.id = 'hatch-test'
      table.style.cssText = 'position: fixed; z-index: 99999; top: 20px; left: 20px; width: 600px; table-layout: fixed; background: #fff;'
      table.innerHTML = '<colgroup><col class="num"><col class="sign"><col></colgroup><tbody>' +
        '<tr><td class="num blank"></td><td class="sign blank"></td><td class="code blank"></td></tr>'.repeat(4) + '</tbody>'
      for (const td of table.querySelectorAll('td')) td.style.height = '20px'
      document.body.prepend(table)
      // The same pattern on one box as wide as a row, for the rows to be compared with.
      const block = document.createElement('div')
      block.id = 'hatch-reference'
      const first = getComputedStyle(table.querySelector('td.num'))
      block.style.cssText = 'position: fixed; z-index: 99999; top: 200px; left: 20px; width: 600px; height: 20px; background: #fff; background-image: ' + first.backgroundImage + '; background-size: ' + first.backgroundSize + ';'
      document.body.prepend(block)
    })
    const rows = []
    for (let i = 1; i <= 4; i++) rows.push(await page.locator(`#hatch-test tr:nth-child(${i})`).screenshot())
    for (let i = 1; i < rows.length; i++) {
      assert.ok((await pixelsApart(page, rows[i], rows[0])) < seamPixels, `row ${i + 1}'s hatching doesn't continue row 1's: the stripes break at the row boundary`)
    }
    const reference = await page.locator('#hatch-reference').screenshot()
    assert.ok((await pixelsApart(page, rows[0], reference)) < seamPixels, "a row's three cells don't hatch as one: the stripes break between the cells")
  } finally { await page.close() }
})

// seamPixels is how many a seam between cells may move: a few of the 12,000 in a row, where a stripe's edge is anti-aliased.
const seamPixels = 20

// pixelsApart is how many pixels of two same-sized screenshots differ by more than the anti-aliasing of an edge would:
// the same stripes drawn from a different origin come out a few bytes apart, never equal.
async function pixelsApart(page, a, b) {
  return page.evaluate(async ([x, y]) => {
    const draw = async (base64) => {
      const img = new Image()
      img.src = 'data:image/png;base64,' + base64
      await img.decode()
      const canvas = document.createElement('canvas')
      canvas.width = img.width
      canvas.height = img.height
      const c = canvas.getContext('2d')
      c.drawImage(img, 0, 0)
      return c.getImageData(0, 0, img.width, img.height).data
    }
    const [p, q] = [await draw(x), await draw(y)]
    if (p.length !== q.length) return -1
    let apart = 0
    for (let i = 0; i < p.length; i += 4) {
      if (Math.max(Math.abs(p[i] - q[i]), Math.abs(p[i + 1] - q[i + 1]), Math.abs(p[i + 2] - q[i + 2])) > 6) apart++
    }
    return apart
  }, [a.toString('base64'), b.toString('base64')])
}

// eventually waits for check to say yes: polled, as the page's policy doesn't allow waitForFunction.
async function eventually(page, what, check) {
  for (let i = 0; i < 80; i++) {
    if (await check()) return
    await page.waitForTimeout(50)
  }
  assert.fail('never: ' + what)
}

test('Reload keeps the place in the file, and Back goes to where it was left in the one before', async () => {
  const page = await viewerPage({ 'docs/a.md': longFile('A'), 'docs/b.md': longFile('B') }, 1100)
  try {
    const place = () => page.evaluate(() => document.getElementById('file-view-body').scrollTop)
    const title = () => page.evaluate(() => (document.querySelector('#file-view-body h1') || {}).textContent)
    await page.evaluate(() => { document.getElementById('file-view-body').scrollTop = 600 })
    const was = await place()
    assert.ok(was > 300, `the file isn't long enough to scroll: ${was}`)
    await page.click('#file-view-reload')
    await eventually(page, 'the file reloaded, at its place', async () => Math.abs((await place()) - was) < 6)
    // A link is a button once the host has said its file is there: that comes a moment after the file is drawn.
    await eventually(page, 'the link to the next file is a button', () => page.evaluate(() => !!document.querySelector('#file-view-body button.file-ref')))
    // Not page.click, which scrolls the link into view first: it is the place at the click that is remembered.
    await page.evaluate(() => document.querySelector('#file-view-body button.file-ref').click())
    await eventually(page, 'the next file shown', async () => (await title()) === 'B')
    assert.equal(await place(), 0, 'a file arrived at is at its top')
    await page.click('#file-view-back')
    await eventually(page, 'the first file shown again', async () => (await title()) === 'A')
    await eventually(page, 'back at its place', async () => Math.abs((await place()) - was) < 6)
  } finally { await page.close() }
})

// The dark theme's CSS is written twice: for a system that is dark (CSS alone, no script), and for dark chosen in the power menu
// (a data-theme attribute, set by theme.js before the page is drawn). They are two copies of the same colours and must not drift
// apart: whatever the stylesheet sets as a custom property, and the glass over the page when vibe is gone, must come out the same
// whichever way the theme was come by, for light as for dark.
async function lookOf(colorScheme, chosen) {
  const page = await browser.newPage({ colorScheme, viewport: { width: 1000, height: 700 } })
  try {
    if (chosen) await page.addInitScript((c) => { document.cookie = 'vibe-theme=' + c + '; Path=/' }, chosen)
    await page.goto(URL)
    return await page.evaluate(async () => {
      const css = await (await fetch('/app.css')).text()
      const names = [...new Set(css.match(/--[a-z0-9-]+(?=\s*:)/g))].sort()
      const root = getComputedStyle(document.documentElement)
      const look = {}
      for (const name of names) look[name] = root.getPropertyValue(name).trim()
      const glass = document.getElementById('offline')
      look['offline background'] = getComputedStyle(glass).backgroundColor
      look['_names'] = names.length
      // Not part of the look compared: a system that is dark gets its native controls dark from the page's meta tag, so the
      // property reads "normal" there, where a choice sets it.
      look['_colorScheme'] = root.colorScheme
      return look
    })
  } finally { await page.close() }
}

test('dark chosen in the menu looks as a dark system does, and so does light', async () => {
  const systemDark = await lookOf('dark', null)
  const systemLight = await lookOf('light', null)
  assert.notDeepEqual(systemDark, systemLight, 'the two themes were not told apart: this test would prove nothing')
  assert.ok(systemDark._names > 20, 'only ' + systemDark._names + ' custom properties were found')
  const strip = ({ _colorScheme, ...look }) => look
  const darkChosen = await lookOf('light', 'dark')
  const lightChosen = await lookOf('dark', 'light')
  assert.deepEqual(strip(darkChosen), strip(systemDark), 'dark chosen on a light system')
  assert.deepEqual(strip(lightChosen), strip(systemLight), 'light chosen on a dark system')
  // A choice also tells the browser, for what it draws itself (scrollbars, form controls).
  assert.equal(darkChosen._colorScheme, 'dark')
  assert.equal(lightChosen._colorScheme, 'light')
})

// The choice is one for the whole host: kept in a cookie, which a browser shares between the ports of a host (a page's
// localStorage is its own), so it holds for every sandbox's page, each on a port of its own; and a page that is open follows a
// choice made in another. Here two pages on two ports of localhost, as two sandboxes' are (a second tab of one page is not let to
// stay open, so it is two pages and not two tabs).
const OTHER_URL = process.env.VIBE_PAGE_URL_OTHER

test('a theme chosen on one sandbox\'s page is followed by another\'s on its own port, and is kept as a host-wide cookie', async () => {
  assert.notEqual(new globalThis.URL(URL).port, new globalThis.URL(OTHER_URL).port, 'two pages on different ports')
  assert.equal(new globalThis.URL(URL).hostname, new globalThis.URL(OTHER_URL).hostname, 'on one host')
  const context = await browser.newContext({ colorScheme: 'dark', viewport: { width: 1000, height: 700 } })
  try {
    const one = await context.newPage()
    const two = await context.newPage()
    await one.goto(URL)
    await two.goto(OTHER_URL)
    const themeOf = (page) => page.evaluate(() => document.documentElement.getAttribute('data-theme'))
    assert.equal(await themeOf(two), null)
    await one.click('#link')
    await one.click('#theme-light')
    assert.equal(await themeOf(one), 'light')
    await eventually(two, 'the other port\'s page to follow', async () => (await themeOf(two)) === 'light')
    assert.equal(await two.getAttribute('#theme-light', 'aria-checked'), 'true')
    const cookie = (await context.cookies()).find((c) => c.name === 'vibe-theme')
    assert.ok(cookie, 'the choice is a cookie')
    assert.equal(cookie.value, 'light')
    assert.equal(cookie.path, '/')
    assert.equal(cookie.httpOnly, false, 'the head script reads it')
    assert.equal(cookie.sameSite, 'Lax')
    assert.ok(cookie.expires > Date.now() / 1000 + 300 * 24 * 3600, 'kept for about a year: expires ' + cookie.expires)
    // A page opened afterwards, on the other port (its first one closed: only one of a page is let to stay), starts in it: no flash
    // of the system's first.
    await two.close()
    const three = await context.newPage()
    // Sampled when <body> first exists: the head's scripts have run, and nothing has been drawn yet.
    await three.addInitScript(() => {
      new MutationObserver((_, observer) => {
        if (document.body) { window.__first = document.documentElement.getAttribute('data-theme'); observer.disconnect() }
      }).observe(document, { childList: true, subtree: true })
    })
    await three.goto(OTHER_URL)
    assert.equal(await themeOf(three), 'light')
    assert.equal(await three.evaluate(() => window.__first), 'light')
    // Back to the system's: the cookie goes, and that page follows.
    await one.click('#link')
    await one.click('#theme-system')
    await eventually(three, 'the other port\'s page to go back to the system', async () => (await themeOf(three)) === null)
    assert.equal((await context.cookies()).some((c) => c.name === 'vibe-theme'), false)
  } finally { await context.close() }
})

// The syntax colours of a block in the conversation are the page's own (the ones the Review tab's files have), in the light theme and
// in the dark: the classes the host's colours become are styled inside a conversation block, not only in a file.
test('a coloured block in the conversation has the syntax colours, in the light and the dark theme', async () => {
  for (const colorScheme of ['light', 'dark']) {
    const page = await browser.newPage({ colorScheme, viewport: { width: 1000, height: 700 } })
    try {
      await page.goto(URL)
      const found = await page.evaluate(() => {
        const md = document.createElement('div')
        md.className = 'md'
        md.innerHTML = '<div class="code-block"><pre><code><span class="hl-kw">a</span><span class="hl-str">b</span><span class="hl-comment">c</span><span class="hl-num">d</span><span class="hl-del">e</span><span class="hl-ins">f</span>plain</code></pre></div>'
        document.body.append(md)
        // What a colour variable is, as the browser computes a colour: through an element, as the page's style can only be set from script.
        const probe = (name) => { const p = document.createElement('span'); p.style.color = 'var(' + name + ')'; document.body.append(p); return getComputedStyle(p).color }
        const code = md.querySelector('code')
        const of = (cls) => getComputedStyle(md.querySelector('.' + cls)).color
        return {
          plain: getComputedStyle(code).color,
          kw: of('hl-kw'), str: of('hl-str'), comment: of('hl-comment'), num: of('hl-num'), del: of('hl-del'), ins: of('hl-ins'),
          wantKw: probe('--hl-kw'), wantStr: probe('--hl-str'), wantComment: probe('--hl-comment'), wantNum: probe('--hl-num'), wantDel: probe('--bad'), wantIns: probe('--ok'),
          italic: getComputedStyle(md.querySelector('.hl-comment')).fontStyle,
        }
      })
      assert.equal(found.kw, found.wantKw, colorScheme + ': keyword')
      assert.equal(found.str, found.wantStr, colorScheme + ': string')
      assert.equal(found.comment, found.wantComment, colorScheme + ': comment')
      assert.equal(found.num, found.wantNum, colorScheme + ': number')
      assert.equal(found.del, found.wantDel, colorScheme + ': deleted line of a diff')
      assert.equal(found.ins, found.wantIns, colorScheme + ': inserted line of a diff')
      assert.notEqual(found.del, found.ins, colorScheme + ': deleted and inserted are told apart')
      assert.notEqual(found.kw, found.plain, colorScheme + ': a keyword reads as one')
      assert.equal(found.italic, 'italic')
    } finally { await page.close() }
  }
})

test('New conversation stays at the right of the conversation bar, whatever the status beside it', async () => {
  const page = await conversationPage(1000)
  try {
    // A status that nearly fills the bar: it fits on the line, the button does not fit beside it, so the button wraps onto a line of its own.
    const measure = (nearlyFull) => page.evaluate((nearlyFull) => {
      const button = document.getElementById('convo-new')
      const status = document.getElementById('convo-status')
      const bar = button.parentElement
      status.textContent = nearlyFull ? 'The agent is compacting its context.' : ''
      status.style.flex = 'none'
      status.style.width = nearlyFull ? (bar.getBoundingClientRect().width - 10) + 'px' : ''
      return Math.round(bar.getBoundingClientRect().right - button.getBoundingClientRect().right)
    }, nearlyFull)
    assert.ok(Math.abs(await measure(false)) <= 1, 'with no status, the button is not at the right edge')
    assert.ok(Math.abs(await measure(true)) <= 1, 'with a status that fills the line, the button is not at the right edge')
  } finally { await page.close() }
})

// The note beside Refresh that the code has changed under the diff (see reviewstale.test.js) is one more thing in the Review bar
// when it shows: it must not push the controls after it off the page, or the page sideways, at any width.
test('the note that the code has changed fits the Review bar beside Refresh, at every width', async () => {
  for (const width of [1400, 1000, 700]) {
    const page = await browser.newPage({ viewport: { width, height: 900 } })
    try {
      await page.goto(URL)
      await page.click('#tab-review')
      await page.waitForSelector(plusSel('R20'), { state: 'attached' })
      const measure = () => page.evaluate(() => {
        const rect = (id) => { const r = document.getElementById(id).getBoundingClientRect(); return { left: r.left, right: r.right, top: r.top, bottom: r.bottom } }
        return { refresh: rect('review-refresh'), comments: rect('review-comments-open'), wide: document.documentElement.scrollWidth > innerWidth }
      })
      const before = await measure()
      await page.evaluate(() => { document.getElementById('review-stale').hidden = false })
      const after = await measure()
      assert.equal(after.wide, before.wide, `at ${width}px wide, showing the note made the page wider than the window`)
      assert.ok(after.comments.right <= width, `at ${width}px wide, the comments button is pushed off the page (${Math.round(after.comments.right)})`)
      assert.ok(after.refresh.right <= width, `at ${width}px wide, Refresh is pushed off the page`)
      const noteBox = await page.locator('#review-stale').boundingBox()
      assert.ok(noteBox && noteBox.width > 0 && noteBox.x + noteBox.width <= width, `at ${width}px wide, the note isn't drawn inside the window: ${JSON.stringify(noteBox)}`)
    } finally { await page.close() }
  }
})

// A Network log row whose rule is being changed says "Updating…" in its status cell, with a spinner. Wider than the words a status
// is usually (Unblocked), it wrapped, and the row was twice as tall for as long as sbx took; and a column that grows for it moves
// every other column as it comes and goes. The row is made as the page makes one, and its status cell swapped for what the page
// puts there, since the page's own log is empty here.
test('a Network log row says it is updating without growing, and without moving its columns', async () => {
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } })
  try {
    await page.goto(URL)
    await page.click('#tab-log')
    await page.waitForSelector('#log-table')
    const measured = await page.evaluate(() => {
      const body = document.querySelector('#log-table tbody')
      const row = document.createElement('tr')
      row.className = 'entry'
      const cell = (cls, html) => { const td = document.createElement('td'); if (cls) td.className = cls; td.innerHTML = html; return td }
      const status = cell('', '<span class="status unblocked">Unblocked</span>')
      row.append(cell('when', '13 Nov, 07:57:36'), cell('host', 'kernel.org:443'), cell('num', '1'), status,
        cell('mono', 'no applicable policies for op(action=net:connect:tcp, resource=net:domain:kernel.org:443)'), cell('', '<button type="button" class="small">Allow</button>'))
      body.append(row)
      const widths = () => Array.from(document.querySelectorAll('#log-table thead th')).map((th) => Math.round(th.getBoundingClientRect().width * 10) / 10)
      const before = { height: row.getBoundingClientRect().height, widths: widths() }
      status.innerHTML = '<span class="updating"><span class="spinner" aria-hidden="true"></span>Updating…</span>'
      const during = { height: row.getBoundingClientRect().height, widths: widths() }
      return { before, during }
    })
    assert.ok(measured.during.height <= measured.before.height + 1, `the row is ${measured.during.height}px tall while updating, ${measured.before.height}px otherwise`)
    assert.deepEqual(measured.during.widths, measured.before.widths, `a column changed width when the row began to say it is updating: ${JSON.stringify(measured.before.widths)} then ${JSON.stringify(measured.during.widths)}`)
  } finally { await page.close() }
})

// A host wrapped in the middle of its port ("kernel.org:44" then "3") and a rule's text broken in the middle of its words are hard to read,
// and both were so whenever a long rule text squeezed the other columns. The host keeps its width; the rule breaks between its words.
test('in the Network log a host stays on one line and a rule breaks between its words, not in them', async () => {
  const page = await browser.newPage({ viewport: { width: 1100, height: 900 } })
  try {
    await page.goto(URL)
    await page.click('#tab-log')
    await page.waitForSelector('#log-table')
    const measured = await page.evaluate(() => {
      const body = document.querySelector('#log-table tbody')
      const row = document.createElement('tr')
      row.className = 'entry'
      const cell = (cls, html) => { const td = document.createElement('td'); if (cls) td.className = cls; td.innerHTML = html; return td }
      const rule = 'denied: rule "local:49b1b3fa-a378-47c1-a4bd-4b3dc7c0f3fb" matched op(action=net:connect:tcp, resource=net:domain:mirrors.kernel.org:443)'
      const host = cell('host', 'mirrors.kernel.org:443')
      const text = cell('mono', rule)
      row.append(cell('when', '13 Nov, 15:14:21'), host, cell('num', '1'), cell('', '<span class="status blocked">Blocked</span>'), text, cell('', '<button type="button" class="small">Allow</button>'))
      body.append(row)
      // Lines a run of text sits on: more than one rectangle for it means it was broken.
      const lines = (td, from, to) => { const r = document.createRange(); r.setStart(td.firstChild, from); r.setEnd(td.firstChild, to); return r.getClientRects().length }
      const hostLines = lines(host, 0, host.textContent.length)
      const split = []
      let at = 0
      for (const word of rule.split(' ')) {
        if (word && lines(text, at, at + word.length) > 1 && word.length < 40) split.push(word)
        at += word.length + 1
      }
      return { hostLines, split, ruleLines: lines(text, 0, rule.length), cellWidth: text.getBoundingClientRect().width, hostWidth: host.getBoundingClientRect().width, hostMin: getComputedStyle(host).minWidth, hostFont: getComputedStyle(host).fontFamily + ' ' + getComputedStyle(host).fontSize }
    })
    assert.equal(measured.hostLines, 1, 'the host is broken over lines: ' + JSON.stringify(measured))
    assert.deepEqual(measured.split, [], `words of the rule were broken in the middle (cell ${measured.cellWidth}px wide)`)
  } finally { await page.close() }
})

// "Removing…" is wider than the Remove button it replaces, and the columns of the Network rules moved to make room for it, as the log's did for
// "Updating…": the last column keeps the width the words need.
test('a Network rules row says it is removing without moving the columns', async () => {
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } })
  try {
    await page.goto(URL)
    await page.click('#tab-rules')
    await page.waitForSelector('#rules-table')
    const measured = await page.evaluate(() => {
      const body = document.querySelector('#rules-table tbody')
      const row = document.createElement('tr')
      row.className = 'entry'
      const cell = (cls, html) => { const td = document.createElement('td'); if (cls) td.className = cls; td.innerHTML = html; return td }
      const action = cell('', '<button type="button" class="small ghost">Remove</button>')
      row.append(cell('', '<span class="status allowed">Allowed</span>'), cell('mono', 'test.example.org'), cell('', 'scoped'), cell('', 'sandbox:vibe'), cell('', ''), action)
      body.append(row)
      const widths = () => Array.from(document.querySelectorAll('#rules-table thead th')).map((th) => Math.round(th.getBoundingClientRect().width * 10) / 10)
      const before = { height: row.getBoundingClientRect().height, widths: widths() }
      action.innerHTML = '<span class="updating"><span class="spinner" aria-hidden="true"></span>Removing…</span>'
      const during = { height: row.getBoundingClientRect().height, widths: widths() }
      return { before, during }
    })
    assert.ok(measured.during.height <= measured.before.height + 1, `the row is ${measured.during.height}px tall while removing, ${measured.before.height}px otherwise`)
    assert.deepEqual(measured.during.widths, measured.before.widths, `a column changed width when the row began to say it is removing: ${JSON.stringify(measured.before.widths)} then ${JSON.stringify(measured.during.widths)}`)
  } finally { await page.close() }
})

test('a state poll cut off by its page closing is not an error, and any other failure of one still is', async () => {
  const gone = new Error('apiResponse.json: Response has been disposed')
  const route = { fetch: async () => { throw gone }, fulfill: async () => {} }
  const open = await browser.newPage()
  try {
    await assert.rejects(stateWith(open, {})(route), /disposed/, 'a failure while the page is open was swallowed')
  } finally { await open.close() }
  const closed = await browser.newPage()
  await closed.close()
  await stateWith(closed, {})(route)
})
