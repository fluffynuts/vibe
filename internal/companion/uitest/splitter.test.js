// The splitter between the conversation and the clipboard: dragged (or moved with the arrow keys) it shares the width between them,
// each keeping at least a fifth; the share is kept for the page. In jsdom (see page.js); dragging is measured in firefox.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

async function convoPage(storage = {}) {
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {}, clipboardUrl: 'http://localhost:5390/', clipboardReady: true },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
    storage: Object.assign({ 'vibe-companion-tab': 'conversation' }, storage),
  })
}

const split = (p) => p.$('convo-splitter')
const share = (p) => Number(split(p).getAttribute('aria-valuenow'))
const key = (p, k) => split(p).dispatchEvent(new p.window.KeyboardEvent('keydown', { key: k, bubbles: true }))

test('a splitter sits between the conversation and the clipboard, a separator reached by Tab', async () => {
  const p = await convoPage()
  try {
    const s = split(p)
    assert.equal(s.previousElementSibling.className, 'convo')
    assert.equal(s.nextElementSibling.className, 'convo-clip')
    assert.equal(s.getAttribute('role'), 'separator')
    assert.equal(s.getAttribute('aria-orientation'), 'vertical')
    assert.equal(s.getAttribute('tabindex'), '0')
    assert.equal(share(p), 67, "the conversation's share, as it was before")
  } finally { p.close() }
})

test('the arrow keys (and Home and End) move it, a fifth at least either side, and the share is kept for the page', async () => {
  const p = await convoPage()
  try {
    key(p, 'ArrowLeft')
    assert.equal(share(p), 62)
    const layout = p.$('panel-conversation').querySelector('.convo-layout')
    assert.deepEqual([layout.style.getPropertyValue('--convo-fr'), layout.style.getPropertyValue('--clip-fr')], ['62fr', '38fr'])
    for (let i = 0; i < 20; i++) key(p, 'ArrowLeft')
    assert.equal(share(p), 20)
    for (let i = 0; i < 30; i++) key(p, 'ArrowRight')
    assert.equal(share(p), 80)
    assert.equal(p.window.localStorage.getItem('vibe-convo-share'), '80')
    key(p, 'Home')
    assert.equal(share(p), 20, 'Home gives the conversation the least')
    key(p, 'End')
    assert.equal(share(p), 80, 'End, the most')
  } finally { p.close() }
})

test('the share kept is used when the page opens again, and a double-click puts it back', async () => {
  const p = await convoPage({ 'vibe-convo-share': '40' })
  try {
    assert.equal(share(p), 40)
    split(p).dispatchEvent(new p.window.MouseEvent('dblclick', { bubbles: true }))
    assert.equal(share(p), 67)
  } finally { p.close() }
})
