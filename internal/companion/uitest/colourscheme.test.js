// Tests of the page's choice of light or dark, in the power menu: the system's (the default), or one of the two forced. The CSS
// that does the colouring is checked in a real Firefox (firefox.test.js); this is the menu, what it sets, and what it keeps.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const KEY = 'vibe-companion-theme'
const reached = { '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} }, '/api/log': { entries: [] } }

const items = (p) => Array.from(p.$('power-menu').querySelectorAll('[role="menuitemradio"]'))
const checked = (p) => items(p).filter((i) => i.getAttribute('aria-checked') === 'true').map((i) => i.textContent.replace(/[✓✔]/g, '').trim())
const choice = (p) => p.window.document.documentElement.getAttribute('data-theme')
const stored = (p) => p.window.localStorage.getItem(KEY)

test('the power menu offers system, dark and light, under a divider of their own, with the system one chosen', async () => {
  const p = await openPage({ routes: reached })
  try {
    assert.deepEqual(items(p).map((i) => i.textContent.replace(/[✓✔]/g, '').trim()), ['System theme', 'Dark theme', 'Light theme'])
    assert.deepEqual(checked(p), ['System theme'])
    const first = items(p)[0]
    assert.equal(first.previousElementSibling.tagName, 'HR', 'a divider sets them apart')
    assert.equal(choice(p), null, 'the system theme is no override at all')
    assert.match(items(p)[0].title, /system/i, 'the system one says what it follows')
  } finally { p.close() }
})

test('choosing dark or light sets it, keeps it in the browser, and moves the tick', async () => {
  const p = await openPage({ routes: reached })
  try {
    await p.click(items(p)[1])
    assert.equal(choice(p), 'dark')
    assert.equal(stored(p), 'dark')
    assert.deepEqual(checked(p), ['Dark theme'])
    await p.click(items(p)[2])
    assert.equal(choice(p), 'light')
    assert.equal(stored(p), 'light')
    assert.deepEqual(checked(p), ['Light theme'])
  } finally { p.close() }
})

test('choosing the system theme takes the override and what was kept away', async () => {
  const p = await openPage({ routes: reached, storage: { [KEY]: 'dark' } })
  try {
    assert.equal(choice(p), 'dark')
    await p.click(items(p)[0])
    assert.equal(choice(p), null)
    assert.equal(stored(p), null)
    assert.deepEqual(checked(p), ['System theme'])
  } finally { p.close() }
})

test('what the browser kept is what the page opens in, and shows chosen', async () => {
  for (const [kept, label] of [['dark', 'Dark theme'], ['light', 'Light theme']]) {
    const p = await openPage({ routes: reached, storage: { [KEY]: kept } })
    try {
      assert.equal(choice(p), kept)
      assert.deepEqual(checked(p), [label])
    } finally { p.close() }
  }
})

test('anything else the browser kept is ignored', async () => {
  for (const kept of ['sepia', '', 'DARK', 'system']) {
    const p = await openPage({ routes: reached, storage: { [KEY]: kept } })
    try {
      assert.equal(choice(p), null, 'kept: ' + JSON.stringify(kept))
      assert.deepEqual(checked(p), ['System theme'])
    } finally { p.close() }
  }
})

test('the choices are radio items a screen reader and the keyboard can use, and choosing closes the menu', async () => {
  const p = await openPage({ routes: reached })
  try {
    p.$('link').click()
    assert.equal(p.$('power-menu').hidden, false)
    for (const item of items(p)) {
      assert.equal(item.tagName, 'BUTTON', 'a button is focusable and takes Enter and Space')
      assert.ok(['true', 'false'].includes(item.getAttribute('aria-checked')))
    }
    await p.click(items(p)[1])
    assert.equal(p.$('power-menu').hidden, true)
    assert.equal(p.$('link').getAttribute('aria-expanded'), 'false')
  } finally { p.close() }
})
