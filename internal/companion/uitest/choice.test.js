// Tests of the model and effort drop-downs in the Conversation tab, in jsdom
// (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const OPTIONS = {
  models: ['sonnet', 'opus', 'haiku'], otherModels: ['best', 'opus[1m]'],
  efforts: ['low', 'medium', 'high'], current: 'Opus 5.5 (default)',
  names: { sonnet: 'Sonnet 5.5', opus: 'Opus 5.5', haiku: 'Haiku 5.5', best: 'Fable 5.1', 'opus[1m]': 'Opus 5.5 (1M context)' },
}

// choicePage is a page for a sandbox whose agent can be asked for a model
// and effort. at is what vibe holds, which a test can change: the choice, the
// model the agent said it ran, whether there is a session, the lists (or an
// error for them), and what the agent is doing.
async function choicePage(over = {}, { choice = true } = {}, storage = {}) {
  const at = Object.assign({ model: '', effort: '', running: '', session: false, options: OPTIONS, optionsError: null, busy: false, refuse: null }, over)
  // The lists the page is given: the agent's, once asked in this run, else
  // what an earlier run kept (at.cached, from at.cachedAt), if anything.
  const listed = () => at.haveOptions ? { options: at.options, optionsAt: 1792000000000, optionsFresh: true }
    : at.cached ? { options: at.cached, optionsAt: at.cachedAt || 1791000000000, optionsFresh: false } : { options: null, optionsFresh: false }
  const body = () => Object.assign({ model: at.model, effort: at.effort, running: at.running, session: at.session }, listed())
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, choice, attention: {} },
    '/api/conversation': () => ({ events: [], last: 0, running: at.busy }),
    '/api/conversation/choice': async (call) => {
      if (call.method === 'GET') { if (at.holdRead) await at.holdRead; return body() }
      if (at.refuse) return { status: 400, body: { error: at.refuse } }
      // What vibe does: a changed model puts the effort back to Default.
      at.effort = call.body.model !== at.model ? '' : call.body.effort
      at.model = call.body.model
      return body()
    },
    '/api/conversation/choice/options': async (call) => {
      if (at.hold) await at.hold
      if (at.optionsError && !(call.body && call.body.wake)) return { status: 502, body: { error: at.optionsError } }
      at.haveOptions = true
      return listed()
    },
    '/api/log': { entries: [] },
  }
  const p = await openPage({ routes, storage: Object.assign({ 'vibe-companion-tab': 'conversation' }, storage), settled: !(at.hold || at.holdRead), before: at.before })
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

test('when the agent ran what was asked for, nothing is said beside the drop-downs: the bubble has it if wanted', async () => {
  const p = await choicePage({ model: 'opus', running: 'claude-opus-5-5' })
  try {
    assert.equal(p.$('choice-ran').textContent, '')
    assert.ok(!p.$('choice-ran').classList.contains('warn-text'))
    await p.click('choice-info')
    assert.match(p.$('choice-bubble-text').textContent, /The last turn ran on claude-opus-5-5/)
  } finally { p.close() }
})

test('with the default chosen, whatever it ran is what was asked for', async () => {
  const p = await choicePage({ model: '', running: 'claude-opus-5-5' })
  try {
    assert.equal(p.$('choice-ran').textContent, '')
    assert.ok(!p.$('choice-info').classList.contains('warn'))
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
    await p.click('choice-info')
    assert.match(p.$('choice-bubble-text').textContent, /The last turn ran on claude-haiku-5-5/)
    await p.click('choice-info')
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

// turns lets the page run a few turns of the event loop, for a test with a
// call held back.
const turns = async () => { for (let i = 0; i < 30; i++) await new Promise((resolve) => setImmediate(resolve)) }

const KEPT = { models: ['sonnet', 'opus'], otherModels: [], efforts: ['low', 'high'], current: 'Sonnet 4.5 (default)' }

test('lists kept from an earlier run fill the drop-downs before the agent has answered, and the fresh ones replace them', async () => {
  let release
  const p = await choicePage({ model: 'opus', cached: KEPT, hold: new Promise((resolve) => { release = resolve }) })
  try {
    await turns()
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', '__other__'], 'the kept lists, at once')
    assert.equal(texts(p.$('choice-model'))[0], 'Default (Sonnet 4.5)')
    assert.deepEqual(values(p.$('choice-effort')), ['', 'low', 'high'])
    assert.ok(!open(p), 'nothing to say about them')
    release()
    await p.settle()
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', 'haiku', '__other__'], 'the agent has now said')
    assert.equal(texts(p.$('choice-model'))[0], 'Default (Opus 5.5)')
    assert.ok(!p.$('choice-info').classList.contains('news'))
  } finally { p.close() }
})

test('when the agent cannot be asked again, the kept lists stay, quietly: the bubble, when asked, says how old they are and why', async () => {
  const p = await choicePage({ model: 'opus', cached: KEPT, optionsError: 'sandbox is stopped' })
  try {
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', '__other__'])
    assert.ok(!p.$('choice-error').open)
    assert.ok(!open(p), 'not opened by itself: there is something to use')
    assert.ok(!p.$('choice-info').classList.contains('warn'))
    assert.ok(p.$('choice-info').classList.contains('news'), 'but the button says there is something to know')
    await p.click('choice-info')
    assert.match(p.$('choice-bubble-stale').textContent, /from .*2026.*couldn't be refreshed.*sandbox is stopped/s)
    assert.ok(!p.$('choice-bubble-stale').hidden)
    await p.click('choice-bubble-retry')
    assert.deepEqual(p.posted('/api/conversation/choice/options'), [{ wake: false }, { wake: true }])
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', 'haiku', '__other__'])
    assert.ok(!open(p), 'it came, so there is nothing more to say')
    assert.ok(!p.$('choice-info').classList.contains('news'))
  } finally { p.close() }
})

test('the old lists are not what a change is held to: what was kept stays usable while stale', async () => {
  const p = await choicePage({ model: 'opus', cached: KEPT, optionsError: 'sandbox is stopped' })
  try {
    await choose(p, 'choice-effort', 'low')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus', effort: 'low' }])
  } finally { p.close() }
})

const dotted = (p) => p.$('choice-info').classList.contains('news')

test('the dot on the button goes when the bubble is dismissed, by its X or by the button, and comes back with the next change', async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    await choose(p, 'choice-effort', 'low')
    assert.ok(open(p) && dotted(p))
    await p.click('choice-bubble-close')
    assert.ok(!dotted(p), 'closed with the X')
    await choose(p, 'choice-effort', 'high')
    assert.ok(open(p) && dotted(p), 'a new change is news again')
    await p.click('choice-info')
    assert.ok(!open(p))
    assert.ok(!dotted(p), 'closed by the button')
  } finally { p.close() }
})

test('the dot goes too when the bubble is dismissed by Escape or a click elsewhere', async () => {
  const p = await choicePage({ model: 'opus' })
  const w = p.window
  try {
    await choose(p, 'choice-effort', 'low')
    p.$('choice-bubble').dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.ok(!dotted(p), 'Escape')
    await choose(p, 'choice-effort', 'high')
    w.document.body.dispatchEvent(new w.MouseEvent('click', { bubbles: true }))
    assert.ok(!dotted(p), 'a click elsewhere')
  } finally { p.close() }
})

test('when told not to show the bubble, the dot is there until the bubble has been looked at and closed', async () => {
  const p = await choicePage({ model: 'opus' }, {}, { 'vibe-companion-choice-quiet': 'yes' })
  try {
    await choose(p, 'choice-effort', 'low')
    assert.ok(!open(p) && dotted(p))
    await p.click('choice-info')
    await p.click('choice-info')
    assert.ok(!dotted(p))
  } finally { p.close() }
})

test('a dismissed notice about old lists stays on the button to be read again, without the dot', async () => {
  const p = await choicePage({ model: 'opus', cached: KEPT, optionsError: 'sandbox is stopped' })
  try {
    assert.ok(dotted(p))
    await p.click('choice-info')
    await p.click('choice-bubble-close')
    assert.ok(!dotted(p))
    await p.click('choice-info')
    assert.match(p.$('choice-bubble-stale').textContent, /sandbox is stopped/)
  } finally { p.close() }
})

test('a dismissed warning that another model ran loses its dot but not its amber, while it is still so', async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    await endTurn(p, 'claude-sonnet-5-5')
    assert.ok(open(p) && dotted(p))
    await p.click('choice-bubble-close')
    assert.ok(!dotted(p))
    assert.ok(p.$('choice-info').classList.contains('warn'), 'it is still so')
    assert.ok(p.$('choice-ran').classList.contains('warn-text'))
    await endTurn(p, 'claude-sonnet-5-5')
    assert.ok(open(p) && dotted(p), 'and the next turn says it again')
  } finally { p.close() }
})

test('a change made while the bubble is open and told not to show still leaves the dot', async () => {
  const p = await choicePage({ model: 'opus' }, {}, { 'vibe-companion-choice-quiet': 'yes' })
  try {
    await p.click('choice-info')
    assert.ok(open(p))
    await choose(p, 'choice-effort', 'low')
    assert.ok(!open(p))
    assert.ok(dotted(p))
  } finally { p.close() }
})

// What the widget shows while the agent's lists are on their way.
const spinning = (p) => !p.$('choice-spinner').hidden
const locked = (p) => p.$('choice-model').disabled && p.$('choice-effort').disabled

test('with nothing known of the lists, the whole widget is disabled with a spinner, and says so to a screen reader', async () => {
  let release
  const p = await choicePage({ model: 'opus', effort: 'high', hold: new Promise((resolve) => { release = resolve }) })
  try {
    await turns()
    assert.ok(locked(p), 'the drop-downs are disabled')
    assert.ok(p.$('convo-choice').classList.contains('loading'), 'and look it')
    assert.equal(p.$('convo-choice').getAttribute('aria-busy'), 'true')
    assert.ok(spinning(p))
    assert.match(p.$('choice-ran').textContent, /Loading models/)
    assert.match(p.$('choice-busy').textContent, /Loading/)
    assert.equal(p.$('choice-model').value, 'opus', 'what is chosen is still shown')
    assert.equal(p.$('choice-effort').value, 'high')
    release()
    await p.settle()
    assert.ok(!p.$('choice-model').disabled && !p.$('choice-effort').disabled)
    assert.ok(!p.$('convo-choice').classList.contains('loading'))
    assert.equal(p.$('convo-choice').getAttribute('aria-busy'), 'false')
    assert.ok(!spinning(p))
    assert.equal(p.$('choice-busy').textContent, '')
    assert.equal(p.$('choice-ran').textContent, '')
  } finally { p.close() }
})

test('before vibe has even said what is chosen, the widget is disabled with a spinner', async () => {
  let release
  const p = await choicePage({ model: 'opus', holdRead: new Promise((resolve) => { release = resolve }) })
  try {
    await turns()
    assert.ok(locked(p) && spinning(p))
    release()
    await p.settle()
    assert.ok(!locked(p) && !spinning(p))
    assert.equal(p.$('choice-model').value, 'opus')
  } finally { p.close() }
})

test('with lists kept, the drop-downs stay usable while they are asked for again: only the spinner shows', async () => {
  let release
  const p = await choicePage({ model: 'opus', cached: KEPT, hold: new Promise((resolve) => { release = resolve }) })
  try {
    await turns()
    assert.ok(!p.$('choice-model').disabled && !p.$('choice-effort').disabled)
    assert.ok(!p.$('convo-choice').classList.contains('loading'))
    assert.equal(p.$('convo-choice').getAttribute('aria-busy'), 'false')
    assert.ok(spinning(p))
    assert.match(p.$('choice-busy').textContent, /Checking/)
    assert.equal(p.$('choice-ran').textContent, '', 'no words beside it')
    release()
    await p.settle()
    assert.ok(!spinning(p))
    assert.equal(p.$('choice-busy').textContent, '')
  } finally { p.close() }
})

test('the spinner stops when asking fails, and the drop-downs are usable', async () => {
  const p = await choicePage({ model: 'opus', optionsError: 'sandbox is stopped' })
  try {
    assert.ok(!spinning(p) && !locked(p))
    assert.equal(p.$('convo-choice').getAttribute('aria-busy'), 'false')
    assert.match(p.$('choice-bubble-text').textContent, /sandbox is stopped/)
  } finally { p.close() }
})

test('a Retry locks the widget again until the agent answers, when it has no lists', async () => {
  const p = await choicePage({ model: 'opus', optionsError: 'sandbox is stopped' })
  try {
    let release
    p.at.hold = new Promise((resolve) => { release = resolve })
    p.$('choice-bubble-retry').click()
    await turns()
    assert.ok(locked(p) && spinning(p))
    release()
    await p.settle()
    assert.ok(!locked(p) && !spinning(p))
    assert.deepEqual(p.posted('/api/conversation/choice/options').at(-1), { wake: true })
  } finally { p.close() }
})

// A page whose timers the test holds: the one that is set for ms is run when fire() is called.
function heldTimers(ms) {
  const held = []
  return {
    before(w) {
      const real = w.setTimeout.bind(w)
      w.setTimeout = (fn, delay, ...rest) => {
        if (delay !== ms) return real(fn, delay, ...rest)
        held.push(fn)
        return held.length
      }
      w.clearTimeout = ((clear) => (id) => { if (typeof id === 'number' && id <= held.length && id > 0) held[id - 1] = null; else clear(id) })(w.clearTimeout.bind(w))
    },
    fire() { for (const fn of held.splice(0)) if (fn) fn() },
  }
}

test('the lock has a limit: after it the drop-downs are usable, and the bubble says it is still asking', async () => {
  const timers = heldTimers(30000)
  let release
  const p = await choicePage({ model: 'opus', hold: new Promise((resolve) => { release = resolve }), before: timers.before })
  try {
    await turns()
    assert.ok(locked(p))
    timers.fire()
    await turns()
    assert.ok(!locked(p), 'usable again')
    assert.equal(p.$('convo-choice').getAttribute('aria-busy'), 'false')
    assert.ok(spinning(p), 'but still asking, and says so')
    assert.ok(open(p))
    assert.match(p.$('choice-bubble-text').textContent, /still asking the agent/i)
    assert.ok(dotted(p))
    release()
    await p.settle()
    assert.ok(!spinning(p))
    assert.ok(!open(p), 'it came, so there is nothing more to say')
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', 'haiku', '__other__'])
  } finally { p.close() }
})

test('the limit does nothing once the agent has answered', async () => {
  const timers = heldTimers(30000)
  const p = await choicePage({ model: 'opus', before: timers.before })
  try {
    timers.fire()
    await turns()
    assert.ok(!open(p))
  } finally { p.close() }
})

test('each model in the drop-down is shown with the name, and so the version, the agent gives it', async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    assert.deepEqual(texts(p.$('choice-model')), ['Default (Opus 5.5)', 'Sonnet 5.5', 'Opus 5.5', 'Haiku 5.5', 'Other…'])
    assert.deepEqual(values(p.$('choice-model')), ['', 'sonnet', 'opus', 'haiku', '__other__'], 'what is kept is still the alias')
  } finally { p.close() }
})

test('a model the agent gave no name for is shown as its alias, in title case', async () => {
  const p = await choicePage({ model: 'opus', options: Object.assign({}, OPTIONS, { names: { opus: 'Opus 5.5' } }) })
  try {
    assert.deepEqual(texts(p.$('choice-model')).slice(1, 4), ['Sonnet', 'Opus 5.5', 'Haiku'])
  } finally { p.close() }
})

test('a model chosen from beyond the drop-down is shown by its name, with the alias it is kept under after it', async () => {
  const p = await choicePage({ model: 'opus[1m]' })
  try {
    assert.equal(p.$('choice-model').value, 'opus[1m]')
    assert.ok(texts(p.$('choice-model')).includes('Opus 5.5 (1M context) · opus[1m]'))
  } finally { p.close() }
})

test('Other… lists each model by what the agent calls it, with the alias it is kept under after', async () => {
  const p = await choicePage({ model: 'opus' })
  try {
    await choose(p, 'choice-model', '__other__')
    const labels = Array.from(p.$('choice-other-list').querySelectorAll('label')).map((l) => l.textContent.trim())
    assert.deepEqual(labels, ['Fable 5.1 · best', 'Opus 5.5 (1M context) · opus[1m]'])
    p.$('choice-other').close('cancel')
    await p.settle()
  } finally { p.close() }
})

test('the question about changing the model names the one it is changing to as the drop-down does', async () => {
  const p = await choicePage({ model: 'opus', session: true })
  try {
    await choose(p, 'choice-model', 'haiku')
    assert.match(p.$('choice-warn-title').textContent, /Change the model to Haiku 5\.5\?/)
    p.$('choice-warn').close('cancel')
    await p.settle()
  } finally { p.close() }
})

test('with no names kept (lists from before they were asked for), the aliases show until the agent is asked again', async () => {
  let release
  const p = await choicePage({ model: 'opus', cached: KEPT, hold: new Promise((resolve) => { release = resolve }) })
  try {
    await turns()
    assert.deepEqual(texts(p.$('choice-model')).slice(1, 3), ['Sonnet', 'Opus'])
    release()
    await p.settle()
    assert.deepEqual(texts(p.$('choice-model')).slice(1, 4), ['Sonnet 5.5', 'Opus 5.5', 'Haiku 5.5'])
  } finally { p.close() }
})

test('the effort entries are in title case, and what is kept is still what the agent takes', async () => {
  const p = await choicePage({ model: 'opus', effort: 'high' })
  try {
    assert.deepEqual(texts(p.$('choice-effort')), ['Default', 'Low', 'Medium', 'High'])
    assert.deepEqual(values(p.$('choice-effort')), ['', 'low', 'medium', 'high'])
    await choose(p, 'choice-effort', 'low')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus', effort: 'low' }])
  } finally { p.close() }
})

test('an effort chosen that the lists lack is shown in title case too, xhigh as Extra High', async () => {
  const p = await choicePage({ model: 'opus', effort: 'xhigh' })
  try {
    assert.equal(p.$('choice-effort').value, 'xhigh')
    assert.ok(texts(p.$('choice-effort')).includes('Extra High'))
  } finally { p.close() }
})

test('aliases with no name from the agent are in title case, wherever they show', async () => {
  const p = await choicePage({ model: 'opus[1m]', session: true, options: Object.assign({}, OPTIONS, { names: {} }) })
  try {
    assert.ok(texts(p.$('choice-model')).includes('Opus[1M]'), 'the one chosen')
    assert.deepEqual(texts(p.$('choice-model')).slice(1, 4), ['Sonnet', 'Opus', 'Haiku'])
    await choose(p, 'choice-model', '__other__')
    const labels = Array.from(p.$('choice-other-list').querySelectorAll('label')).map((l) => l.textContent.trim())
    assert.deepEqual(labels, ['Best', 'Opus[1M]'])
    p.$('choice-other').close('cancel')
    await p.settle()
    await choose(p, 'choice-model', 'haiku')
    assert.match(p.$('choice-warn-title').textContent, /Change the model to Haiku\?/)
    p.$('choice-warn').close('cancel')
    await p.settle()
  } finally { p.close() }
})

test('a full model id typed under Other… is shown exactly as typed, not title-cased', async () => {
  const p = await choicePage({ model: 'claude-opus-4-1' })
  try {
    assert.ok(texts(p.$('choice-model')).includes('claude-opus-4-1'))
  } finally { p.close() }
})

test('xhigh is called Extra High in the list, and is still kept and sent as xhigh', async () => {
  const p = await choicePage({ model: 'opus', options: Object.assign({}, OPTIONS, { efforts: ['low', 'high', 'xhigh', 'max'] }) })
  try {
    assert.deepEqual(texts(p.$('choice-effort')), ['Default', 'Low', 'High', 'Extra High', 'Max'])
    await choose(p, 'choice-effort', 'xhigh')
    assert.deepEqual(p.posted('/api/conversation/choice'), [{ model: 'opus', effort: 'xhigh' }])
  } finally { p.close() }
})
