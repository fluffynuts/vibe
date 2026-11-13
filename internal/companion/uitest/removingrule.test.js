// A rule removed from the Network rules tab says "Removing…" in its row, from the moment the user confirms until a read of the rules that
// began after sbx finished the command has come back: as the Network log's rows do (see updatingrow.test.js). In jsdom (see page.js).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const rule = (id, host, over = {}) => ({ id, name: id, resources: [host], decision: 'allowed', source: 'local', scope: 'sandbox:box', protocol: 'tcp', status: 'active', removable: true, ...over })
const turns = async (n) => { for (let i = 0; i < n; i++) await new Promise((resolve) => setImmediate(resolve)) }
const held = () => { let release; const promise = new Promise((resolve) => { release = (value) => resolve(value) }); return { promise, release } }

async function rulesPage({ rules, routes = {} }) {
  const at = { rules, held: null }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
      '/api/remove': { command: 'sbx policy rm ...' },
      '/api/rules': () => at.held || { rules: at.rules },
      ...routes,
    },
    storage: { 'vibe-companion-tab': 'rules' },
  })
  await p.poll()
  return Object.assign(p, { at })
}

const rowOf = (p, host) => [...p.$('rules-table').tBodies[0].querySelectorAll('tr.entry')].find((r) => r.textContent.includes(host))
const words = (p, host) => rowOf(p, host).textContent.replace(/\s+/g, ' ').trim()
const hold = (p) => { let release; p.at.held = new Promise((resolve) => { release = (rules) => { p.at.held = null; p.at.rules = rules; resolve({ rules }) } }); return (rules) => release(rules) }
const rulesReads = (p) => p.calls.filter((c) => c.path === '/api/rules').length
const a = rule('a1', 'a.example.com')
const b = rule('b1', 'b.example.com')

test('a rule being removed says so in its row from the moment the removal is confirmed, before sbx has run the command', async () => {
  const command = held()
  const p = await rulesPage({ rules: [a, b], routes: { '/api/remove': () => command.promise } })
  try {
    const sign = p.$('rules-refreshing')
    const before = rulesReads(p)
    rowOf(p, 'a.example.com').querySelector('button').click()
    await turns(10)
    p.$('confirm').close('run')
    await turns(40)
    assert.match(words(p, 'a.example.com'), /Removing…/, 'nothing says the removal was asked for, while sbx runs it')
    assert.equal(rowOf(p, 'a.example.com').querySelector('button'), null, 'a second removal can be started on a rule being removed')
    assert.ok(rowOf(p, 'b.example.com').querySelector('button'), 'another rule lost its button')
    assert.doesNotMatch(words(p, 'b.example.com'), /Removing/)
    assert.ok(sign.classList.contains('on') && sign.classList.contains('at-once'), 'no sign while sbx runs the command')
    assert.equal(rulesReads(p), before, 'the rules were read before the command had finished')
    const release = hold(p)
    command.release({ command: 'sbx policy rm ...' })
    await turns(40)
    assert.match(words(p, 'a.example.com'), /Removing…/, 'the marker went when the command finished, before the read that shows the result')
    release([b])
    await p.settle()
    assert.equal(rowOf(p, 'a.example.com'), undefined, 'the rule is still listed')
    assert.doesNotMatch(words(p, 'b.example.com'), /Removing/)
  } finally { p.close() }
})

test('a read that began before the removal, arriving after it, does not take the marker off', async () => {
  const p = await rulesPage({ rules: [a, b] })
  try {
    const stale = hold(p)
    await p.kick('tick')
    rowOf(p, 'a.example.com').querySelector('button').click()
    await turns(10)
    p.$('confirm').close('run')
    await turns(40)
    assert.match(words(p, 'a.example.com'), /Removing…/)
    stale([a, b]) // the old answer
    await turns(40)
    assert.match(words(p, 'a.example.com'), /Removing…/, 'the old answer took the marker off and the rule looked as though nothing was done')
    p.at.rules = [b]
    await p.poll()
    assert.equal(rowOf(p, 'a.example.com'), undefined)
  } finally { p.close() }
})

test('a removal that failed says removing until the truth is read, then shows the rule as it is, removable again', async () => {
  const p = await rulesPage({ rules: [a], routes: { '/api/remove': { status: 500, body: { error: 'sbx said no' } } } })
  try {
    rowOf(p, 'a.example.com').querySelector('button').click()
    await turns(10)
    const release = hold(p)
    p.$('confirm').close('run')
    await turns(40)
    assert.match(p.$('toast').textContent, /sbx said no/)
    assert.match(words(p, 'a.example.com'), /Removing…/, 'part of a removal may have been made: nobody knows until it is read')
    release([a])
    await p.settle()
    assert.doesNotMatch(words(p, 'a.example.com'), /Removing/)
    assert.ok(rowOf(p, 'a.example.com').querySelector('button'), 'the rule cannot be removed again')
  } finally { p.close() }
})

test('cancelling the confirmation marks nothing', async () => {
  const p = await rulesPage({ rules: [a] })
  try {
    rowOf(p, 'a.example.com').querySelector('button').click()
    await turns(10)
    p.$('confirm').close('cancel')
    await turns(20)
    assert.doesNotMatch(words(p, 'a.example.com'), /Removing/)
    assert.ok(rowOf(p, 'a.example.com').querySelector('button'))
  } finally { p.close() }
})
