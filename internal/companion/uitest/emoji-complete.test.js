// Completing :emoji: names in the prompt box, in jsdom: a pop-over like the slash commands', that puts the character itself
// in the prompt, and a setting to turn it off.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

async function pageWith(storage = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
      '/api/admin': { state: 'running', info: '' },
    },
    storage: Object.assign({ 'vibe-companion-tab': 'conversation' }, storage),
  })
  await p.settle()
  return p
}

const input = (p) => p.$('convo-input')
const list = (p) => p.$('convo-emoji')
const names = (p) => [...list(p).querySelectorAll('li[role=option]')].map((li) => li.dataset.name)

async function typeText(p, text) {
  input(p).value = text
  input(p).setSelectionRange(text.length, text.length)
  input(p).dispatchEvent(new p.window.Event('input', { bubbles: true }))
}

function key(p, k, extra = {}) {
  const ev = new p.window.KeyboardEvent('keydown', Object.assign({ key: k, bubbles: true, cancelable: true }, extra))
  input(p).dispatchEvent(ev)
  return ev
}

test('typing a colon and two letters offers the emoji, the closest first, with the character beside the name', async () => {
  const p = await pageWith()
  try {
    await typeText(p, 'go :roc')
    assert.equal(list(p).hidden, false)
    assert.equal(names(p)[0], 'rocket')
    assert.match(list(p).querySelector('li[role=option]').textContent, /🚀/)
    assert.match(list(p).querySelector('li[role=option]').textContent, /rocket/)
    assert.equal(input(p).getAttribute('aria-expanded'), 'true')
    assert.ok(list(p).querySelector('mark'), 'the part typed is not marked')
  } finally { p.close() }
})

test('nothing opens for a bare colon, digits, a smiley, a time, a URL or inside code', async () => {
  const p = await pageWith()
  try {
    for (const text of [':', 'at 12:30', 'see http://example.com', 'use `:roc', ':12', ':)']) {
      await typeText(p, text)
      assert.equal(list(p).hidden, true, JSON.stringify(text))
    }
  } finally { p.close() }
})

test('Tab puts the character in place of what was typed, with a space after it, and closes the list', async () => {
  const p = await pageWith()
  try {
    await typeText(p, 'go :roc')
    const ev = key(p, 'Tab')
    assert.equal(ev.defaultPrevented, true)
    assert.equal(input(p).value, 'go 🚀 ')
    assert.equal(input(p).selectionStart, 'go 🚀 '.length)
    assert.equal(list(p).hidden, true)
  } finally { p.close() }
})

// Enter picks again: ":D" and Enter is 😀, which is what was meant, now that the emoticon is what the list offers first.
test('Enter picks, as Tab does, and is then neither a new line nor a send', async () => {
  const p = await pageWith()
  try {
    await typeText(p, ':tad')
    const ev = key(p, 'Enter')
    assert.equal(ev.defaultPrevented, true)
    assert.equal(input(p).value, '🎉 ')
    assert.deepEqual(p.calls.filter((c) => c.method === 'POST' && c.path !== '/api/awake').map((c) => c.path), [], 'it sent the prompt')
  } finally { p.close() }
})

test(':D and Enter is the grinning face, the emoticon first in the list and marked as one', async () => {
  const p = await pageWith()
  try {
    await typeText(p, 'fine :D')
    const first = list(p).querySelector('li[role=option]')
    assert.equal(first.dataset.name, 'grinning')
    assert.match(first.textContent, /😀/)
    assert.match(first.textContent, /:D/)
    key(p, 'Enter')
    assert.equal(input(p).value, 'fine 😀 ')
  } finally { p.close() }
})

test('one letter opens the list, and +1 does too', async () => {
  const p = await pageWith()
  try {
    await typeText(p, 'hi :D')
    assert.equal(list(p).hidden, false)
    await typeText(p, 'ok :+1')
    assert.equal(list(p).hidden, false)
    assert.equal(names(p)[0], 'thumbsup')
    key(p, 'Tab')
    assert.equal(input(p).value, 'ok 👍 ')
  } finally { p.close() }
})

// Emoticons: converted when the word is finished (a space, Enter, or the prompt sent), never where they are not one.
async function typeAnd(p, before, char, data = char) {
  input(p).value = before + char
  input(p).setSelectionRange((before + char).length, (before + char).length)
  input(p).dispatchEvent(new p.window.InputEvent('input', { bubbles: true, inputType: 'insertText', data }))
}

test('an emoticon is turned into its emoji when the space after it is typed', async () => {
  const p = await pageWith()
  try {
    await typeAnd(p, 'ok :)', ' ')
    assert.equal(input(p).value, 'ok 🙂 ')
    assert.equal(input(p).selectionStart, 'ok 🙂 '.length)
    await typeAnd(p, '(: ', 'x')
    assert.equal(input(p).value, '(: x', 'a (: that was not at the end of the word it is in was changed')
    await typeAnd(p, '(:', ' ')
    assert.equal(input(p).value, '🙂 ')
    await typeAnd(p, 'sad :/', ' ')
    assert.equal(input(p).value, 'sad 😕 ')
    await typeAnd(p, 'wink ;)', ' ')
    assert.equal(input(p).value, 'wink 😉 ')
    await typeAnd(p, 'love <3', ' ')
    assert.equal(input(p).value, 'love ❤️ ')
  } finally { p.close() }
})

test('it is left alone where it is not an emoticon: a URL, a path, code', async () => {
  const p = await pageWith()
  try {
    for (const before of ['see http:/', 'C:/', 'at 12:(', 'use `a :)', 'x<3']) {
      await typeAnd(p, before, ' ')
      assert.equal(input(p).value, before + ' ', before)
    }
  } finally { p.close() }
})

test('Enter after an emoticon turns it into the emoji, and goes on to be a new line', async () => {
  const p = await pageWith()
  try {
    input(p).value = 'ok :)'
    input(p).setSelectionRange(5, 5)
    const ev = key(p, 'Enter')
    assert.equal(input(p).value, 'ok 🙂')
    assert.equal(ev.defaultPrevented, false, 'the new line was taken')
  } finally { p.close() }
})

test('an emoticon the prompt ends with is turned into the emoji when it is sent, by Ctrl+Enter or the button', async () => {
  for (const send of [(p) => key(p, 'Enter', { ctrlKey: true }), (p) => p.$('convo-form').dispatchEvent(new p.window.Event('submit', { bubbles: true, cancelable: true }))]) {
    const p = await pageWith()
    try {
      input(p).value = 'fine :D'
      input(p).setSelectionRange(7, 7)
      send(p)
      await p.settle()
      const sent = p.calls.filter((c) => c.method === 'POST' && c.path !== '/api/awake')
      assert.ok(sent.some((c) => JSON.stringify(c.body).includes('fine 😀')), JSON.stringify(sent.map((c) => c.path + ' ' + JSON.stringify(c.body))))
      assert.ok(!sent.some((c) => JSON.stringify(c.body).includes(':D')), 'the emoticon was sent as it was typed')
    } finally { p.close() }
  }
})

test('with the setting off no emoticon is changed, on a space, on Enter or when sent', async () => {
  const p = await pageWith({ 'vibe-emoji-complete': 'off' })
  try {
    await typeAnd(p, 'ok :)', ' ')
    assert.equal(input(p).value, 'ok :) ')
    input(p).value = 'ok :)'
    input(p).setSelectionRange(5, 5)
    key(p, 'Enter')
    assert.equal(input(p).value, 'ok :)')
    key(p, 'Enter', { ctrlKey: true })
    await p.settle()
    const sent = p.calls.filter((c) => c.method === 'POST' && c.path !== '/api/awake')
    assert.ok(sent.some((c) => JSON.stringify(c.body).includes('ok :)')))
  } finally { p.close() }
})

test('Escape keeps :D as it was typed in the prompt, until a space finishes it', async () => {
  const p = await pageWith()
  try {
    await typeText(p, ':D')
    key(p, 'Escape')
    assert.equal(list(p).hidden, true)
    assert.equal(input(p).value, ':D')
  } finally { p.close() }
})

test('the arrow keys choose another, and Escape closes the list and leaves the text', async () => {
  const p = await pageWith()
  try {
    await typeText(p, ':smi')
    const first = names(p)[0]
    assert.ok(names(p).length > 1)
    key(p, 'ArrowDown')
    assert.equal(list(p).querySelector('li[aria-selected=true]').dataset.name, names(p)[1])
    key(p, 'ArrowUp')
    assert.equal(list(p).querySelector('li[aria-selected=true]').dataset.name, first)
    key(p, 'Escape')
    assert.equal(list(p).hidden, true)
    assert.equal(input(p).value, ':smi')
  } finally { p.close() }
})

test('a click on one picks it, and the prompt keeps the focus', async () => {
  const p = await pageWith()
  try {
    await typeText(p, 'ok :rock')
    input(p).focus()
    const item = list(p).querySelector('li[data-name=rocket]')
    const ev = new p.window.MouseEvent('mousedown', { bubbles: true, cancelable: true })
    item.dispatchEvent(ev)
    assert.equal(ev.defaultPrevented, true, 'the mousedown would take the focus from the prompt')
    assert.equal(input(p).value, 'ok 🚀 ')
  } finally { p.close() }
})

test('typing the closing colon of a complete name turns it into the character', async () => {
  const p = await pageWith()
  try {
    input(p).value = 'nice :tada:'
    input(p).setSelectionRange(11, 11)
    input(p).dispatchEvent(new p.window.InputEvent('input', { bubbles: true, inputType: 'insertText', data: ':' }))
    assert.equal(input(p).value, 'nice 🎉')
    assert.equal(input(p).selectionStart, 'nice 🎉'.length)
  } finally { p.close() }
})

test('a name that is not one, or a time, is left as typed when the colon is', async () => {
  const p = await pageWith()
  try {
    for (const text of [':nope:', 'at 12:30:']) {
      input(p).value = text
      input(p).setSelectionRange(text.length, text.length)
      input(p).dispatchEvent(new p.window.InputEvent('input', { bubbles: true, inputType: 'insertText', data: ':' }))
      assert.equal(input(p).value, text)
    }
  } finally { p.close() }
})

test('a colon left behind by deleting is not turned into anything: only typing the colon is', async () => {
  const p = await pageWith()
  try {
    input(p).value = ':tada:'
    input(p).setSelectionRange(6, 6)
    input(p).dispatchEvent(new p.window.InputEvent('input', { bubbles: true, inputType: 'deleteContentBackward' }))
    assert.equal(input(p).value, ':tada:')
  } finally { p.close() }
})

test('the slash commands still complete, and a name after a command completes too', async () => {
  const p = await pageWith()
  try {
    await typeText(p, '/rev')
    assert.equal(list(p).hidden, true, 'emoji list opened for a slash command')
    await typeText(p, '/review :roc')
    assert.equal(list(p).hidden, false)
    assert.equal(p.$('convo-commands').hidden, true)
  } finally { p.close() }
})

// A setting, in the Settings card with "My prompts as Markdown": on unless turned off, and remembered.
test('it is on unless it was turned off in this browser', async () => {
  const on = await pageWith()
  const off = await pageWith({ 'vibe-emoji-complete': 'off' })
  try {
    assert.equal(on.$('settings-emoji').checked, true)
    assert.equal(off.$('settings-emoji').checked, false)
    await typeText(off, 'go :roc')
    assert.equal(list(off).hidden, true)
    off.window.document.getElementById('convo-input').value = 'go :tada:'
    off.window.document.getElementById('convo-input').dispatchEvent(new off.window.InputEvent('input', { bubbles: true, inputType: 'insertText', data: ':' }))
    assert.equal(input(off).value, 'go :tada:', 'it converted with the setting off')
  } finally { on.close(); off.close() }
})

test('turning it off closes the list at once and is remembered, and turning it on again is too', async () => {
  const p = await pageWith()
  try {
    await typeText(p, 'go :roc')
    assert.equal(list(p).hidden, false)
    const box = p.$('settings-emoji')
    box.checked = false
    box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    assert.equal(list(p).hidden, true)
    assert.equal(p.window.localStorage.getItem('vibe-emoji-complete'), 'off')
    await typeText(p, 'go :roc')
    assert.equal(list(p).hidden, true)
    box.checked = true
    box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    assert.equal(p.window.localStorage.getItem('vibe-emoji-complete'), 'on')
    await typeText(p, 'go :roc')
    assert.equal(list(p).hidden, false)
  } finally { p.close() }
})

test('the setting is in the Settings card with the other, and says what it does', async () => {
  const p = await pageWith()
  try {
    const box = p.$('settings-emoji')
    const card = box.closest('section.card')
    assert.equal(card.querySelector('h2').textContent, 'Settings')
    assert.ok(card.contains(p.$('settings-prompts-markdown')))
    assert.match(box.closest('label').textContent, /emoji/i)
  } finally { p.close() }
})

// Where it is drawn is the browser's to say (see firefox.test.js): without layout, here, it still gets a position of its own,
// and never one that is not a number.
test('the list is given a position near the caret, which is a number, and the slash list is not', async () => {
  const p = await pageWith()
  try {
    await typeText(p, 'go :roc')
    assert.match(list(p).style.left, /^-?\d+(\.\d+)?px$/)
    assert.match(list(p).style.top, /^-?\d+(\.\d+)?px$/)
    assert.doesNotMatch(list(p).style.cssText, /NaN/)
    await typeText(p, '/rev')
    assert.equal(p.$('convo-commands').style.top, '')
  } finally { p.close() }
})

test('the measuring leaves nothing behind on the page', async () => {
  const p = await pageWith()
  try {
    const before = p.window.document.body.children.length
    for (let i = 0; i < 5; i++) await typeText(p, 'go :roc' + 'k'.repeat(i))
    assert.equal(p.window.document.body.children.length, before)
  } finally { p.close() }
})
