// Tests of the model and effort drop-downs in the Conversation tab, in jsdom
// (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const OPTIONS = {
  models: ['sonnet', 'opus', 'haiku'], otherModels: ['best', 'opus[1m]'],
  efforts: ['low', 'medium', 'high'], current: 'Opus 5.5 (default)',
}

// choicePage is a page for a sandbox whose agent can be asked for a model
// and effort. at is what vibe holds, which a test can change: the choice, the
// model the agent said it ran, whether there is a session, the lists (or an
// error for them), and what the agent is doing.
async function choicePage(over = {}, { choice = true } = {}) {
  const at = Object.assign({ model: '', effort: '', running: '', session: false, options: OPTIONS, optionsError: null, busy: false, refuse: null }, over)
  const body = () => ({ model: at.model, effort: at.effort, running: at.running, session: at.session, options: at.haveOptions ? at.options : null })
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, choice, attention: {} },
    '/api/conversation': () => ({ events: [], last: 0, running: at.busy }),
    '/api/conversation/choice': (call) => {
      if (call.method === 'GET') return body()
      if (at.refuse) return { status: 400, body: { error: at.refuse } }
      // What vibe does: a changed model puts the effort back to Default.
      at.effort = call.body.model !== at.model ? '' : call.body.effort
      at.model = call.body.model
      return body()
    },
    '/api/conversation/choice/options': (call) => {
      if (at.optionsError && !(call.body && call.body.wake)) return { status: 502, body: { error: at.optionsError } }
      at.haveOptions = true
      return { options: at.options }
    },
    '/api/log': { entries: [] },
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': 'conversation' } })
  return Object.assign(p, { at })
}

const texts = (select) => Array.from(select.options).map((o) => o.textContent)
const values = (select) => Array.from(select.options).map((o) => o.value)
const shown = (p, id) => { const n = p.$(id); return !!n && !n.hidden && !n.closest('[hidden]') }

// choose picks value in select, as the user would.
async function choose(p, id, value) {
  const select = p.$(id)
  select.value = value
  select.dispatchEvent(new p.window.Event('change', { bubbles: true }))
  await p.settle()
}

test('a sandbox with nothing to choose has no drop-downs', async () => {
  const p = await choicePage({}, { choice: false })
  try {
    assert.ok(!shown(p, 'choice-model'))
    assert.deepEqual(p.calls.filter((c) => c.path.startsWith('/api/conversation/choice')), [])
  } finally { p.close() }
})

test('the drop-downs show what is chosen, from the lists the agent gave', async () => {
  const p = await choicePage({ model: 'opus', effort: 'high' })
  try {
    assert.ok(shown(p, 'choice-model') && shown(p, 'choice-effort'))
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', 'haiku', '__other__'])
    assert.equal(p.$('choice-model').value, 'opus')
    assert.deepEqual(values(p.$('choice-effort')), ['', 'low', 'medium', 'high'])
    assert.equal(p.$('choice-effort').value, 'high')
  } finally { p.close() }
})

test('Default says which model the agent uses then, as it names it', async () => {
  const p = await choicePage()
  try {
    assert.equal(texts(p.$('choice-model'))[0], 'Default (Opus 5.5)')
    assert.equal(p.$('choice-model').value, '')
    assert.equal(p.$('choice-effort').value, '')
  } finally { p.close() }
})

test('the lists are asked for when the page opens, without waking the sandbox', async () => {
  const p = await choicePage()
  try {
    assert.deepEqual(p.posted('/api/conversation/choice/options'), [{ wake: false }])
  } finally { p.close() }
})

test('a model chosen that the lists lack (a full name) is shown, not swapped for Default', async () => {
  const p = await choicePage({ model: 'claude-opus-4-1' })
  try {
    assert.equal(p.$('choice-model').value, 'claude-opus-4-1')
    assert.ok(texts(p.$('choice-model')).includes('claude-opus-4-1'))
  } finally { p.close() }
})

test('when the agent cannot be asked for its lists, the page says why, and Retry wakes the sandbox to ask', async () => {
  const p = await choicePage({ optionsError: 'the agent could not say what it has: sandbox is stopped', model: 'opus' })
  try {
    assert.match(p.$('choice-note').textContent, /sandbox is stopped/)
    assert.equal(p.$('choice-model').value, 'opus', 'what is chosen is still shown')
    await p.click(p.button('Retry', p.$('choice-note')))
    assert.deepEqual(p.posted('/api/conversation/choice/options'), [{ wake: false }, { wake: true }])
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', 'haiku', '__other__'])
    assert.ok(!/sandbox is stopped/.test(p.$('choice-note').textContent))
  } finally { p.close() }
})

test('changing the effort is kept at once, with no question', async () => {
  const p = await choicePage({ model: 'opus', session: true })
  try {
    await choose(p, 'choice-effort', 'low')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus', effort: 'low' }])
    assert.ok(!p.$('choice-warn').open)
    assert.equal(p.$('choice-effort').value, 'low')
  } finally { p.close() }
})

test('changing the model with nothing yet said is kept at once, and the effort goes back to Default', async () => {
  const p = await choicePage({ model: 'opus', effort: 'high', session: false })
  try {
    await choose(p, 'choice-model', 'sonnet')
    assert.ok(!p.$('choice-warn').open, 'nothing to re-read, so nothing to warn of')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'sonnet', effort: 'high' }])
    assert.equal(p.$('choice-model').value, 'sonnet')
    assert.equal(p.$('choice-effort').value, '', 'what vibe kept is what is shown')
  } finally { p.close() }
})

test('changing the model of a conversation under way asks first, and says what it costs', async () => {
  const p = await choicePage({ model: 'opus', session: true })
  try {
    await choose(p, 'choice-model', 'haiku')
    assert.ok(p.$('choice-warn').open)
    assert.match(p.$('choice-warn').textContent, /re-reads the whole conversation/)
    assert.match(p.$('choice-warn').textContent, /slower and costs more/)
    assert.deepEqual(p.posted('/api/conversation/choice'), [], 'nothing is changed until it is confirmed')
    p.$('choice-warn').close('go')
    await p.settle()
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'haiku', effort: '' }])
    assert.equal(p.$('choice-model').value, 'haiku')
  } finally { p.close() }
})

test('cancelling the question puts the drop-down back, and changes nothing', async () => {
  const p = await choicePage({ model: 'opus', session: true })
  try {
    await choose(p, 'choice-model', 'haiku')
    p.$('choice-warn').close('cancel')
    await p.settle()
    assert.deepEqual(p.posted('/api/conversation/choice'), [])
    assert.equal(p.$('choice-model').value, 'opus')
  } finally { p.close() }
})

test('Other… offers every other model the agent listed, and one picked goes the same way', async () => {
  const p = await choicePage({ model: 'opus', session: true })
  try {
    await choose(p, 'choice-model', '__other__')
    assert.ok(p.$('choice-other').open)
    const listed = Array.from(p.$('choice-other-list').querySelectorAll('input[type=radio]')).map((r) => r.value)
    assert.deepEqual(listed, ['best', 'opus[1m]'])
    p.$('choice-other-list').querySelector('input[value="opus[1m]"]').checked = true
    p.$('choice-other').close('go')
    await p.settle()
    assert.ok(p.$('choice-warn').open, 'a model under way still asks')
    assert.deepEqual(p.posted('/api/conversation/choice'), [])
    p.$('choice-warn').close('go')
    await p.settle()
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus[1m]', effort: '' }])
    assert.equal(p.$('choice-model').value, 'opus[1m]')
  } finally { p.close() }
})

test('Other… takes a full model name, typed', async () => {
  const p = await choicePage({ model: '', session: false })
  try {
    await choose(p, 'choice-model', '__other__')
    await p.type(p.$('choice-other-name'), 'claude-opus-4-1')
    p.$('choice-other').close('go')
    await p.settle()
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'claude-opus-4-1', effort: '' }])
  } finally { p.close() }
})

test('cancelling Other… puts the drop-down back', async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    await choose(p, 'choice-model', '__other__')
    p.$('choice-other').close('cancel')
    await p.settle()
    assert.equal(p.$('choice-model').value, 'opus')
    assert.deepEqual(p.posted('/api/conversation/choice'), [])
  } finally { p.close() }
})

test('Other… with nothing picked or typed changes nothing', async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    await choose(p, 'choice-model', '__other__')
    p.$('choice-other').close('go')
    await p.settle()
    assert.equal(p.$('choice-model').value, 'opus')
    assert.deepEqual(p.posted('/api/conversation/choice'), [])
  } finally { p.close() }
})

test("a choice vibe refuses is said in vibe's words, and the drop-down goes back", async () => {
  const p = await choicePage({ model: 'opus', refuse: '"x y" isn\'t a model name: it can have letters, digits and . _ : [ ] -' })
  try {
    await choose(p, 'choice-model', '__other__')
    await p.type(p.$('choice-other-name'), 'x y')
    p.$('choice-other').close('go')
    await p.settle()
    assert.match(p.$('choice-note').textContent, /isn't a model name/)
    assert.equal(p.$('choice-model').value, 'opus')
  } finally { p.close() }
})

test('while the agent is answering a change can still be made, and is said to apply to the next prompt', async () => {
  const p = await choicePage({ model: 'opus', busy: true })
  try {
    await p.every('pollConversation')
    await choose(p, 'choice-effort', 'low')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus', effort: 'low' }])
    assert.match(p.$('choice-note').textContent, /applies to your next prompt/)
  } finally { p.close() }
})

test('what the agent said it ran is shown beside the drop-downs', async () => {
  const p = await choicePage({ model: 'opus', running: 'claude-opus-5-5' })
  try {
    assert.match(p.$('choice-ran').textContent, /claude-opus-5-5/)
    assert.ok(!p.$('choice-ran').classList.contains('warn-text'))
  } finally { p.close() }
})

test('when the agent says it ran something else than was asked for, the page says so', async () => {
  const p = await choicePage({ model: 'opus', running: 'claude-sonnet-5-5' })
  try {
    assert.match(p.$('choice-ran').textContent, /asked for opus.*claude-sonnet-5-5/)
    assert.ok(p.$('choice-ran').classList.contains('warn-text'))
  } finally { p.close() }
})

test('a name that is not a model (best, opusplan) is not held against what ran', async () => {
  const p = await choicePage({ model: 'best', running: 'claude-opus-5-5' })
  try {
    assert.ok(!p.$('choice-ran').classList.contains('warn-text'))
  } finally { p.close() }
})

test("a turn's end asks again what the agent ran", async () => {
  const p = await choicePage({ model: 'haiku', running: '', busy: true })
  try {
    await p.every('pollConversation')
    p.at.running = 'claude-haiku-5-5'
    p.at.session = true
    p.at.busy = false
    await p.every('pollConversation')
    assert.match(p.$('choice-ran').textContent, /claude-haiku-5-5/)
    assert.ok(!/applies to your next prompt/.test(p.$('choice-note').textContent))
  } finally { p.close() }
})
