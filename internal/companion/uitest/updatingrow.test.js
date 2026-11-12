// A host whose rule was just changed says "Updating…" in the Network log until a read that began after the change has come back:
// no claim about the result, which only sbx knows (the row would be a guess otherwise: a deny rule beats an allow, a wildcard
// may still cover the host). In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const row = (decision, over = {}) => ({ host: 'www.kernel.org:443', decision, hits: 1, lastSeen: '2026-11-12T12:39:25Z', rule: '', ...over })
const other = { host: 'cdn.kernel.org:443', decision: 'blocked', hits: 2, lastSeen: '2026-11-12T12:39:25Z', rule: '' }

async function logPage({ entries, routes = {} }) {
  const at = { entries, held: null }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': () => at.held || { entries: at.entries },
      '/api/allow': { command: 'sbx policy allow ...' },
      '/api/deny': { command: 'sbx policy deny ...' },
      '/api/remove': { command: 'sbx policy rm ...' },
      '/api/rules': { rules: [] },
      ...routes,
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  await p.poll()
  return Object.assign(p, { at })
}

const rowOf = (p, host) => [...p.$('log-table').tBodies[0].querySelectorAll('tr.entry')].find((r) => r.textContent.includes(host))
const words = (p, host) => rowOf(p, host).textContent.replace(/\s+/g, ' ').trim()
const turns = async (n) => { for (let i = 0; i < n; i++) await new Promise((resolve) => setImmediate(resolve)) }
const hold = (p) => { let release; p.at.held = new Promise((resolve) => { release = (entries) => { p.at.held = null; p.at.entries = entries; resolve({ entries }) } }); return (entries) => release(entries) }

test('the row of a host whose rule was just changed says it is updating, and says what it is when the read is back', async () => {
  const p = await logPage({ entries: [row('blocked'), other] })
  try {
    await p.click(rowOf(p, 'www.kernel.org').querySelector('button'))
    const release = hold(p)
    p.$('confirm').close('run')
    await turns(30)
    assert.match(words(p, 'www.kernel.org'), /Updating…/)
    assert.doesNotMatch(words(p, 'www.kernel.org'), /Blocked|Unblocked|Allowed/, 'the row says an outcome nobody knows yet')
    assert.equal(rowOf(p, 'www.kernel.org').querySelector('button').disabled, true, 'a second change can be started on a row still changing')
    assert.doesNotMatch(words(p, 'cdn.kernel.org'), /Updating/, 'another host is marked')
    release([row('unblocked'), other])
    await p.settle()
    assert.match(words(p, 'www.kernel.org'), /Unblocked/)
    assert.doesNotMatch(words(p, 'www.kernel.org'), /Updating/)
    assert.equal(rowOf(p, 'www.kernel.org').querySelector('button').disabled, false)
  } finally { p.close() }
})

test('a read that began before the change, arriving after it, does not take the marker off', async () => {
  const p = await logPage({ entries: [row('blocked'), other] })
  try {
    const releaseStale = hold(p)
    await p.kick('tick') // a read is on its way, and began before the change
    rowOf(p, 'www.kernel.org').querySelector('button').click()
    await turns(10)
    p.$('confirm').close('run')
    await turns(40)
    assert.match(words(p, 'www.kernel.org'), /Updating…/)
    releaseStale([row('blocked'), other]) // the old answer
    await turns(40)
    assert.match(words(p, 'www.kernel.org'), /Updating…/, 'the old answer took the marker off, and the row said Blocked as though nothing had been done')
    p.at.entries = [row('unblocked'), other] // what sbx says now, for the next read, which begins after the change
    await p.poll()
    assert.doesNotMatch(words(p, 'www.kernel.org'), /Updating/)
    assert.match(words(p, 'www.kernel.org'), /Unblocked/)
  } finally { p.close() }
})

test('a change that failed also says updating until the truth is read, then shows what is so', async () => {
  const p = await logPage({ entries: [row('blocked')], routes: { '/api/allow': { status: 500, body: { error: 'sbx said no' } } } })
  try {
    await p.click(rowOf(p, 'www.kernel.org').querySelector('button'))
    const release = hold(p)
    p.$('confirm').close('run')
    await turns(30)
    assert.match(p.$('toast').textContent, /sbx said no/)
    assert.match(words(p, 'www.kernel.org'), /Updating…/, 'part of a change may have been made: nobody knows until it is read')
    release([row('blocked')])
    await p.settle()
    assert.match(words(p, 'www.kernel.org'), /Blocked/)
    assert.doesNotMatch(words(p, 'www.kernel.org'), /Updating/)
  } finally { p.close() }
})

test('blocking an unblocked host marks it too', async () => {
  const rule = { id: 'r1', name: '', resources: ['www.kernel.org:443'], source: 'local', scope: 'sandbox:box', removable: true, wider: false }
  const p = await logPage({ entries: [row('unblocked', { allowedBy: [rule] })] })
  try {
    await p.click(rowOf(p, 'www.kernel.org').querySelector('button'))
    const release = hold(p)
    p.$('confirm').close('run')
    await turns(30)
    assert.match(words(p, 'www.kernel.org'), /Updating…/)
    release([row('blocked')])
    await p.settle()
    assert.match(words(p, 'www.kernel.org'), /Blocked/)
  } finally { p.close() }
})

test('a rule added from the Network rules tab marks no row: the log is not being read there', async () => {
  const p = await logPage({ entries: [row('blocked')] })
  try {
    await p.click('tab-rules')
    p.$('rule-host').value = 'www.kernel.org:443'
    p.$('rule-form').dispatchEvent(new p.window.Event('submit', { bubbles: true, cancelable: true }))
    await p.settle()
    p.$('confirm').close('run')
    await p.settle()
    await p.click('tab-log')
    assert.doesNotMatch(words(p, 'www.kernel.org'), /Updating/, 'a marker was left on a row that is no longer being waited for')
  } finally { p.close() }
})
