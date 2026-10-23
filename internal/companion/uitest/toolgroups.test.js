// Tests of the tools the agent used being gathered into one collapsed group between what it says, in jsdom (see
// page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const said = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text }] } } })
const used = (seq, name, input = { command: 'ls' }) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'tool_use', name, input }] } } })
const gave = (seq, text, isError) => ({ seq, kind: 'agent', agent: { type: 'user', message: { content: [{ type: 'tool_result', content: text, is_error: !!isError }] } } })
const turnEnd = (seq) => ({ seq, kind: 'agent', agent: { type: 'result' } })
const prompt = (seq, text) => ({ seq, kind: 'user', text })

async function groupPage(events, running = false) {
  const at = { events, running }
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
    '/api/conversation': (call) => {
      const after = Number(new URLSearchParams(call.query).get('after')) || 0
      return { events: at.events.filter((e) => e.seq > after), last: at.events.reduce((m, e) => Math.max(m, e.seq), 0), running: at.running }
    },
    '/api/log': { entries: [] },
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': 'conversation' } })
  const w = p.window
  w.CSS = { highlights: new Map() }
  w.Highlight = class { constructor(...ranges) { this.ranges = ranges } }
  await p.every('pollConversation')
  return Object.assign(p, { at })
}

const groups = (p) => Array.from(p.$('convo-log').querySelectorAll('details.tool-group'))
const summary = (g) => g.querySelector(':scope > summary').textContent
const log = (p) => Array.from(p.$('convo-log').children).filter((c) => !c.classList.contains('typing')).map((c) => c.classList.contains('tool-group') ? '[group]' : c.textContent.trim())

test('the tools used between what the agent says are one group, in order with what it says', async () => {
  const p = await groupPage([
    prompt(1, 'go'), used(2, 'Bash'), gave(3, 'a'), used(4, 'Read', { file_path: '/x' }), gave(5, 'b'),
    said(6, 'first words'), used(7, 'Bash'), gave(8, 'c'), said(9, 'last words'),
  ])
  try {
    assert.deepEqual(log(p).map((l) => l.replace(/[^\w\s[\]]+$/, '')), ['go', '[group]', 'first words', '[group]', 'last words'])
    assert.equal(groups(p).length, 2)
    assert.equal(groups(p)[0].querySelectorAll('.msg.tool').length, 2, 'both tool lines are inside it')
    assert.equal(groups(p)[0].querySelectorAll('details.tool-result').length, 2, 'and both results')
  } finally { p.close() }
})

test('a group says how many tools were used, and which', async () => {
  const p = await groupPage([
    used(1, 'Bash'), gave(2, 'a'), used(3, 'Bash'), gave(4, 'b'), used(5, 'Read'), used(6, 'Bash'), said(7, 'done'),
    used(8, 'Edit'), said(9, 'ok'),
  ])
  try {
    assert.equal(summary(groups(p)[0]), '⚙ 4 tool uses · Bash ×3, Read ×1')
    assert.equal(summary(groups(p)[1]), '⚙ 1 tool use · Edit ×1')
  } finally { p.close() }
})

test('a group with many different tools names the most used, and says there are more', async () => {
  const names = ['A', 'B', 'C', 'D', 'E', 'F', 'G']
  const p = await groupPage([...names.map((n, i) => used(i + 1, n)), said(9, 'done')])
  try {
    assert.match(summary(groups(p)[0]), /^⚙ 7 tool uses · A ×1, B ×1, C ×1, D ×1, E ×1, and 2 more$/)
  } finally { p.close() }
})

test('a group in which a tool failed says so, and is marked', async () => {
  const p = await groupPage([used(1, 'Bash'), gave(2, 'boom', true), used(3, 'Bash'), gave(4, 'fine'), said(5, 'done')])
  try {
    assert.match(summary(groups(p)[0]), /2 tool uses · Bash ×2 · 1 failed$/)
    assert.ok(groups(p)[0].classList.contains('bad'))
  } finally { p.close() }
})

test('groups are collapsed once the agent has said something after them, or the turn has ended', async () => {
  const p = await groupPage([
    prompt(1, 'go'), used(2, 'Bash'), said(3, 'words'),
    used(4, 'Bash'), turnEnd(5),
    prompt(6, 'again'), used(7, 'Bash'), prompt(8, 'interrupting'),
  ])
  try {
    assert.deepEqual(groups(p).map((g) => g.open), [false, false, false])
  } finally { p.close() }
})

test('while the agent works, the tools it is using are shown as they come, and collapse when it speaks', async () => {
  const p = await groupPage([prompt(1, 'go'), used(2, 'Bash'), gave(3, 'a')], true)
  try {
    assert.equal(groups(p)[0].open, true, 'open while it works')
    p.at.events.push(used(4, 'Read'))
    await p.every('pollConversation')
    assert.equal(groups(p).length, 1, 'the next tool joins the group')
    assert.equal(groups(p)[0].open, true)
    assert.match(summary(groups(p)[0]), /2 tool uses · Bash ×1, Read ×1/)
    p.at.events.push(said(5, 'I read it'))
    await p.every('pollConversation')
    assert.equal(groups(p)[0].open, false, 'collapsed once it speaks')
    p.at.events.push(used(6, 'Edit'))
    await p.every('pollConversation')
    assert.equal(groups(p).length, 2, 'what follows is a new group')
    assert.equal(groups(p)[1].open, true)
    assert.equal(groups(p)[0].open, false)
  } finally { p.close() }
})

test('a group the user opened stays open when the agent goes on', async () => {
  const p = await groupPage([used(1, 'Bash'), said(2, 'words')], true)
  try {
    await p.click(groups(p)[0].querySelector(':scope > summary'))
    assert.equal(groups(p)[0].open, true)
    p.at.events.push(said(3, 'more words'), used(4, 'Bash'), said(5, 'and more'))
    await p.every('pollConversation')
    assert.equal(groups(p)[0].open, true, 'still as the user left it')
    assert.equal(groups(p)[1].open, false)
  } finally { p.close() }
})

test('a group the user closed while it was live stays closed as the agent adds to it', async () => {
  const p = await groupPage([used(1, 'Bash')], true)
  try {
    await p.click(groups(p)[0].querySelector(':scope > summary'))
    assert.equal(groups(p)[0].open, false)
    p.at.events.push(used(2, 'Read'))
    await p.every('pollConversation')
    assert.equal(groups(p)[0].open, false)
    assert.match(summary(groups(p)[0]), /2 tool uses/)
  } finally { p.close() }
})

test('a tool result inside a group is still opened by its own summary', async () => {
  const p = await groupPage([used(1, 'Bash'), gave(2, 'the output'), said(3, 'done')])
  try {
    const result = groups(p)[0].querySelector('details.tool-result')
    assert.equal(result.open, false)
    assert.equal(result.querySelector('pre').textContent, 'the output')
    await p.click(result.querySelector('summary'))
    assert.equal(result.open, true)
  } finally { p.close() }
})

test('no group is made when the agent used no tool', async () => {
  const p = await groupPage([prompt(1, 'hi'), said(2, 'hello')])
  try {
    assert.equal(groups(p).length, 0)
  } finally { p.close() }
})

test('what is in a collapsed group is searched only with tools ticked, and going to a match opens the group', async () => {
  const p = await groupPage([used(1, 'Bash', { command: 'grep needle-in-a-command' }), gave(2, 'needle-in-output'), said(3, 'a needle-in-words')])
  try {
    await p.click('convo-find-open')
    await p.type(p.$('convo-find-input'), 'needle')
    const matches = () => ((p.window.CSS.highlights.get('find-match') || { ranges: [] }).ranges).map((r) => r.toString())
    assert.equal(matches().length, 1, 'only the words')
    assert.equal(groups(p)[0].open, false)
    p.$('convo-find-tools').checked = true
    p.$('convo-find-tools').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.equal(matches().length, 3)
    assert.equal(groups(p)[0].open, true, 'the group holding the match is opened')
  } finally { p.close() }
})

test('a group is collapsed when the agent is not working, even if nothing came after it', async () => {
  const p = await groupPage([prompt(1, 'go'), used(2, 'Bash'), gave(3, 'a')], false)
  try {
    assert.equal(groups(p)[0].open, false)
  } finally { p.close() }
})
