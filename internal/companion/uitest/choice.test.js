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
async function choicePage(over = {}, { choice = true } = {}, storage = {}) {
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
  const p = await openPage({ routes, storage: Object.assign({ 'vibe-companion-tab': 'conversation' }, storage) })
  return Object.assign(p, { at })
}

const texts = (select) => Array.from(select.options).map((o) => o.textContent)
const values = (select) => Array.from(select.options).map((o) => o.value)
const shown = (p, id) => { const n = p.$(id); return !!n && !n.hidden && !n.closest('[hidden]') }

// open says the bubble beside the drop-downs is showing.
const open = (p) => !p.$('choice-bubble').hidden

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

test('when the agent cannot be asked for its lists, the bubble says why without blocking, and Retry wakes the sandbox to ask', async () => {
  const p = await choicePage({ optionsError: 'the agent could not say what it has: sandbox is stopped', model: 'opus' })
  try {
    assert.ok(!p.$('choice-error').open, 'nothing the user did has failed, so nothing blocks')
    assert.ok(open(p), 'but it is said, where it will be seen')
    assert.match(p.$('choice-bubble-text').textContent, /sandbox is stopped/)
    assert.ok(p.$('choice-info').classList.contains('warn'))
    assert.equal(p.$('choice-model').value, 'opus', 'what is chosen is still shown')
    await p.click(p.button('Retry', p.$('choice-bubble')))
    assert.deepEqual(p.posted('/api/conversation/choice/options'), [{ wake: false }, { wake: true }])
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', 'haiku', '__other__'])
    assert.ok(!open(p), 'it was about the lists, and they came')
    assert.ok(!p.$('choice-info').classList.contains('warn'))
  } finally { p.close() }
})

test('changing the effort is kept at once, with no question', async () => {
  const p = await choicePage({ model: 'opus', session: true })
  try {
    await choose(p, 'choice-effort', 'low')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus', effort: 'low' }])
    assert.ok(!p.$('choice-warn').open)
    assert.equal(p.$('choice-effort').value, 'low')
    assert.ok(open(p))
    assert.match(p.$('choice-bubble-text').textContent, /applies to your next prompt/)
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

test("a choice vibe refuses is said in vibe's words, in a dialog that must be answered, and the drop-down goes back", async () => {
  const p = await choicePage({ model: 'opus', refuse: '"x y" isn\'t a model name: it can have letters, digits and . _ : [ ] -' })
  try {
    await choose(p, 'choice-model', '__other__')
    await p.type(p.$('choice-other-name'), 'x y')
    p.$('choice-other').close('go')
    await p.settle()
    assert.ok(p.$('choice-error').open)
    assert.match(p.$('choice-error-text').textContent, /isn't a model name/)
    assert.ok(!open(p), 'the bubble is for what is good to know, not for this')
    p.$('choice-error').close('ok')
    await p.settle()
    assert.equal(p.$('choice-model').value, 'opus')
  } finally { p.close() }
})

test('while the agent is answering a change can still be made, and is said to apply to the next prompt', async () => {
  const p = await choicePage({ model: 'opus', busy: true })
  try {
    await p.every('pollConversation')
    await choose(p, 'choice-effort', 'low')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus', effort: 'low' }])
    assert.match(p.$('choice-bubble-text').textContent, /applies to your next prompt/)
  } finally { p.close() }
})

test('what the agent said it ran is shown beside the drop-downs', async () => {
  const p = await choicePage({ model: 'opus', running: 'claude-opus-5-5' })
  try {
    assert.match(p.$('choice-ran').textContent, /claude-opus-5-5/)
    assert.ok(!p.$('choice-ran').classList.contains('warn-text'))
  } finally { p.close() }
})

test('when the agent said it ran something else than was asked for, the page shows it, without interrupting a page just opened', async () => {
  const p = await choicePage({ model: 'opus', running: 'claude-sonnet-5-5' })
  try {
    assert.match(p.$('choice-ran').textContent, /claude-sonnet-5-5/)
    assert.ok(p.$('choice-ran').classList.contains('warn-text'))
    assert.ok(p.$('choice-info').classList.contains('warn'))
    assert.ok(!open(p))
    await p.click('choice-info')
    assert.match(p.$('choice-bubble-text').textContent, /asked for opus, but the agent says it ran claude-sonnet-5-5/)
  } finally { p.close() }
})

test('a name that is not a model (best, opusplan) is not held against what ran', async () => {
  const p = await choicePage({ model: 'best', running: 'claude-opus-5-5' })
  try {
    assert.ok(!p.$('choice-ran').classList.contains('warn-text'))
    assert.ok(!p.$('choice-info').classList.contains('warn'))
  } finally { p.close() }
})

// endTurn has the agent finish a turn that ran `ran`.
async function endTurn(p, ran) {
  p.at.busy = true
  await p.every('pollConversation')
  p.at.running = ran
  p.at.session = true
  p.at.busy = false
  await p.every('pollConversation')
}

test("a turn's end asks again what the agent ran, and no longer says a change is waiting", async () => {
  const p = await choicePage({ model: 'haiku' })
  try {
    await choose(p, 'choice-effort', 'low')
    assert.ok(open(p) && p.$('choice-info').classList.contains('news'))
    await endTurn(p, 'claude-haiku-5-5')
    assert.match(p.$('choice-ran').textContent, /claude-haiku-5-5/)
    assert.ok(!open(p), 'what it said is out of date now')
    assert.ok(!p.$('choice-info').classList.contains('news'))
  } finally { p.close() }
})

test('a turn that ran another model than was asked for always says so, and cannot be told to stop saying it', async () => {
  const p = await choicePage({ model: 'opus', effort: '' }, {}, { 'vibe-companion-choice-quiet': 'yes' })
  try {
    await endTurn(p, 'claude-sonnet-5-5')
    assert.ok(open(p))
    assert.match(p.$('choice-bubble-text').textContent, /asked for opus, but the agent says it ran claude-sonnet-5-5/)
    assert.ok(!shown(p, 'choice-quiet-row'), 'no tick to hide it with')
    assert.ok(p.$('choice-info').classList.contains('warn'))
    await p.click('choice-bubble-close')
    assert.ok(!open(p))
    await endTurn(p, 'claude-sonnet-5-5')
    assert.ok(open(p), 'again, for the next turn')
  } finally { p.close() }
})

test('the bubble is closed by its button, by Escape inside it, and by a click elsewhere, but not by Escape in the prompt box', async () => {
  const p = await choicePage({ model: 'opus' })
  const w = p.window
  try {
    await choose(p, 'choice-effort', 'low')
    assert.ok(open(p))
    await p.click('choice-bubble-close')
    assert.ok(!open(p))
    await p.click('choice-info')
    assert.ok(open(p))
    p.$('choice-bubble').dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.ok(!open(p), 'Escape inside it')
    await p.click('choice-info')
    p.$('convo-input').dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.ok(open(p), "Escape in the prompt box is the agent's Stop, not this")
    w.document.body.dispatchEvent(new w.MouseEvent('click', { bubbles: true }))
    assert.ok(!open(p), 'a click elsewhere')
    await p.click('choice-info')
    assert.ok(open(p), 'the button opens it again')
    await p.click('choice-info')
    assert.ok(!open(p), 'and closes it')
  } finally { p.close() }
})

test('the button says whether the bubble is open', async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    assert.equal(p.$('choice-info').getAttribute('aria-expanded'), 'false')
    assert.equal(p.$('choice-info').getAttribute('aria-controls'), 'choice-bubble')
    await p.click('choice-info')
    assert.equal(p.$('choice-info').getAttribute('aria-expanded'), 'true')
  } finally { p.close() }
})

test("Don't show me this again keeps the bubble shut after a change, but the button still says there is something to know", async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    await choose(p, 'choice-effort', 'low')
    assert.ok(open(p))
    p.$('choice-quiet').checked = true
    p.$('choice-quiet').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.click('choice-bubble-close')
    await choose(p, 'choice-effort', 'high')
    assert.ok(!open(p), 'told not to')
    assert.ok(p.$('choice-info').classList.contains('news'), 'but it is still there to be asked')
    await p.click('choice-info')
    assert.ok(open(p))
    assert.ok(p.$('choice-quiet').checked, 'and the tick can be taken back')
    p.$('choice-quiet').checked = false
    p.$('choice-quiet').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.click('choice-bubble-close')
    await choose(p, 'choice-effort', 'medium')
    assert.ok(open(p), 'asked again')
  } finally { p.close() }
})

test("what was said not to show again is remembered by the browser, for the next time the page is opened", async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    await choose(p, 'choice-effort', 'low')
    p.$('choice-quiet').checked = true
    p.$('choice-quiet').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    assert.equal(p.window.localStorage.getItem('vibe-companion-choice-quiet'), 'yes')
  } finally { p.close() }
  const again = await choicePage({ model: 'opus' }, {}, { 'vibe-companion-choice-quiet': 'yes' })
  try {
    await choose(again, 'choice-effort', 'low')
    assert.ok(!open(again))
    assert.ok(again.$('choice-info').classList.contains('news'))
  } finally { again.close() }
})
