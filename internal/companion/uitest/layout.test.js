// The Conversation tab uses the height under the tab bar for the conversation and the clipboard: the status button and New
// conversation sit at the tab bar's end (on the Conversation tab alone), and the clipboard column has no bar of its own. In jsdom
// (see page.js); the layout itself is measured in firefox.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

async function convoPage(at = {}) {
  const now = Object.assign({ clipboardUrl: 'http://localhost:5390/', clipboardReady: true }, at)
  const p = await openPage({
    routes: {
      '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {},
        clipboardUrl: now.clipboardUrl, clipboardReady: now.clipboardReady }),
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': { instructions: {} },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  return Object.assign(p, { now })
}

test('the status button and New conversation sit at the tab bar\'s end, on the Conversation tab alone', async () => {
  const p = await convoPage()
  try {
    const tabbar = p.$('tabbar')
    for (const id of ['status-info', 'convo-new']) {
      assert.ok(tabbar.contains(p.$(id)), id + ' is not in the tab bar')
      assert.ok(p.$(id).closest('.tabbar-end'), id + ' is not at its end')
    }
    assert.equal(p.$('convo-actions').hidden, false, 'not shown on the Conversation tab')
    await p.click('tab-log')
    assert.equal(p.$('convo-actions').hidden, true, 'shown on another tab')
    await p.click('tab-conversation')
    assert.equal(p.$('convo-actions').hidden, false)
  } finally { p.close() }
})

test('the status balloon still opens from its button, and closes with Escape back to it', async () => {
  const p = await convoPage()
  try {
    await p.click('status-info')
    assert.equal(p.$('status-balloon').hidden, false)
    p.$('status-balloon').dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.equal(p.$('status-balloon').hidden, true)
    assert.equal(p.window.document.activeElement, p.$('status-info'))
  } finally { p.close() }
})

test('the Conversation tab has no bar of its own above the conversation, nor the clipboard a heading or Open on its own', async () => {
  const p = await convoPage()
  try {
    const panel = p.$('panel-conversation')
    assert.equal(panel.querySelector('.convo-bar'), null, 'the bar above the conversation is still there')
    const clip = panel.querySelector('.convo-clip')
    assert.ok(![...clip.querySelectorAll('*')].some((n) => n.children.length === 0 && n.textContent.trim() === 'Clipboard'), 'the Clipboard heading is still there')
  } finally { p.close() }
})

test('a clipboard page that stops answering is said in a banner over its frame, gone when it answers again', async () => {
  const p = await convoPage()
  try {
    await p.poll()
    const note = p.$('convo-clipboard-note')
    assert.equal(note.hidden, true, 'said while answering')
    p.now.clipboardReady = false
    await p.poll()
    assert.equal(note.hidden, false, 'not said')
    assert.match(note.textContent, /^The clipboard page isn.t answering just now/)
    assert.ok(note.classList.contains('clip-banner'), 'not a banner over the frame')
    assert.equal(note.parentElement, p.$('convo-clipboard-frame').parentElement, 'not beside the frame it is over')
    p.now.clipboardReady = true
    await p.poll()
    assert.equal(note.hidden, true)
  } finally { p.close() }
})

// The clipboard opens in a tab of its own from an icon at its frame's corner. On the companion page, not in the clipboard page:
// the frame is sandboxed without allow-popups (the page in it is the sandbox's), so a link in it opened nothing, without a word.
test('an icon at the clipboard frame\'s corner opens the clipboard in a tab of its own', async () => {
  const p = await convoPage()
  try {
    await p.poll()
    const link = p.$('convo-clipboard-open')
    assert.ok(link, 'no icon')
    assert.equal(link.parentElement, p.$('convo-clipboard-frame').parentElement, 'not over the frame')
    assert.equal(link.hidden, false)
    assert.equal(link.getAttribute('href'), 'http://localhost:5390/', 'not the page on its own (no ?framed)')
    assert.equal(link.target, '_blank')
    assert.match(link.rel, /noopener/)
    assert.ok(link.getAttribute('aria-label'), 'an icon with no name')
    assert.ok(!/allow-popups/.test(p.$('convo-clipboard-frame').getAttribute('sandbox')), 'the frame was let open popups')
  } finally { p.close() }
})

test('the icon is not shown while there is no clipboard page to open', async () => {
  const p = await convoPage({ clipboardUrl: '' })
  try {
    await p.poll()
    assert.equal(p.$('convo-clipboard-open').hidden, true)
  } finally { p.close() }
})
