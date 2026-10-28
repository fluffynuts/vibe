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

test('the longest everyday entries fit their drop-downs without being cut off', async () => {
  const page = await conversationPage(1500)
  try {
    const fits = await page.evaluate(() => {
      const measure = document.createElement('canvas').getContext('2d')
      const out = {}
      for (const [id, texts] of [['choice-model', ['Default (Opus 5.5)', 'Sonnet 5.5', 'Fable 5.1', 'Haiku 5.5']], ['choice-effort', ['Default effort', 'Medium effort', 'Extra High effort']]]) {
        const select = document.getElementById(id)
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

// viewerPage opens the Conversation tab on a conversation that mentions the first of files (path -> text), and
// the file viewer open on it, width pixels wide. The host is stubbed: it has a folder to show files from.
async function viewerPage(files, width) {
  const page = await browser.newPage({ viewport: { width, height: 900 } })
  const first = Object.keys(files)[0]
  const events = [{ seq: 1, kind: 'user', text: 'hi' }, sayEv(2, 'See `' + first + '`.')]
  const json = (route, body) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
  const query = (route) => new (require('node:url').URL)(route.request().url()).searchParams
  await page.route(/\/api\/state$/, async (route) => json(route, Object.assign(await (await route.fetch()).json(), { viewFiles: true })))
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
