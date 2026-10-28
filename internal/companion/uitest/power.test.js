// Tests of the power button at the top right of the page, which says whether vibe is reached and opens
// the menu of what can be done about it. See docs/plan-quality-of-life.md, "A power menu on the page".
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { openPage } = require('./page.js')

const reached = { '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} }, '/api/log': { entries: [] } }
const gone = { '/api/state': () => { throw new Error('refused') }, '/api/log': () => { throw new Error('refused') } }

const stateOf = (p) => ['up', 'down', 'connecting'].filter((s) => p.$('link').classList.contains(s))

test('the button says connecting until vibe has answered', async () => {
  const p = await openPage({ routes: reached, settled: false })
  try {
    assert.deepEqual(stateOf(p), ['connecting'])
    assert.match(p.$('link').title, /connecting/)
    await p.settle()
  } finally { p.close() }
})

test('with vibe reached, the button is up, and says so to a tooltip, a screen reader and the menu', async () => {
  const p = await openPage({ routes: reached })
  try {
    assert.deepEqual(stateOf(p), ['up'])
    assert.match(p.$('link').title, /connected/)
    assert.match(p.$('link').getAttribute('aria-label'), /connected/)
    assert.equal(p.$('link-text').textContent, 'connected')
    assert.equal(p.$('link-text').getAttribute('role'), 'status')
    assert.match(p.$('power-status').textContent, /^Connected/)
  } finally { p.close() }
})

test('with vibe gone, the button is down, and says what to do wherever it said "connected"', async () => {
  const p = await openPage({ routes: gone })
  try {
    assert.deepEqual(stateOf(p), ['down'])
    for (const said of [p.$('link').title, p.$('link').getAttribute('aria-label'), p.$('link-text').textContent, p.$('power-status').textContent]) {
      assert.match(said, /vibe is not running/, said)
    }
  } finally { p.close() }
})

test('the button opens the menu and does nothing else; Escape, an outside click and a second click close it', async () => {
  const p = await openPage({ routes: reached })
  try {
    const before = p.calls.length
    assert.equal(p.$('power-menu').hidden, true)
    await p.click('link')
    assert.equal(p.$('power-menu').hidden, false)
    assert.equal(p.$('link').getAttribute('aria-expanded'), 'true')
    assert.equal(p.calls.slice(before).filter((c) => c.method !== 'GET').length, 0, 'opening the menu acted')
    p.$('power-menu').dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.equal(p.$('power-menu').hidden, true)
    assert.equal(p.$('link').getAttribute('aria-expanded'), 'false')
    await p.click('link')
    p.window.document.body.dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    assert.equal(p.$('power-menu').hidden, true)
    await p.click('link')
    await p.click('link')
    assert.equal(p.$('power-menu').hidden, true)
  } finally { p.close() }
})

test('the menu still opens with vibe gone, because the issue links need no vibe', async () => {
  const p = await openPage({ routes: gone })
  try {
    await p.click('link')
    assert.equal(p.$('power-menu').hidden, false)
    assert.equal(p.$('power-menu').querySelectorAll('a').length, 3)
  } finally { p.close() }
})

const templates = fs.readdirSync(path.join(__dirname, '..', '..', '..', '.github', 'ISSUE_TEMPLATE'))

test('the menu links to the three issue forms of the repository, each of which exists, and passes nothing else on', async () => {
  const p = await openPage({ routes: reached })
  try {
    const links = [...p.$('power-menu').querySelectorAll('a')]
    assert.deepEqual(links.map((a) => a.textContent), ['Report a bug', 'Request a feature', 'Something else'])
    for (const a of links) {
      const url = new URL(a.href)
      assert.equal(url.origin + url.pathname, 'https://github.com/fluffynuts/vibe/issues/new', a.href)
      assert.deepEqual([...url.searchParams.keys()], ['template'], 'what the link carries: ' + a.href)
      assert.ok(templates.includes(url.searchParams.get('template')), url.searchParams.get('template') + ' is not in .github/ISSUE_TEMPLATE')
      assert.equal(a.target, '_blank')
      assert.equal(a.rel, 'noopener noreferrer')
    }
  } finally { p.close() }
})
