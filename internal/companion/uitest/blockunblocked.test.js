// The Network log's Block on a host that was blocked and is now allowed by a rule: sbx won't add a deny beside an allow, so it removes
// the allow rule(s) that let the host through, saying what else they allow. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const rule = (over) => ({ id: 'r-exact', name: '', resources: ['www.kernel.org:443'], source: 'local', scope: 'sandbox:box', removable: true, wider: false, ...over })
const unblocked = (allowedBy) => ({ host: 'www.kernel.org:443', decision: 'unblocked', hits: 1, lastSeen: '2026-11-12T12:39:25Z', rule: '', ...(allowedBy ? { allowedBy } : {}) })

async function logPage(entries, routes = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': () => ({ entries }),
      '/api/remove': { command: 'sbx policy rm network ...' },
      '/api/deny': { command: 'sbx policy deny network ...' },
      ...routes,
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  await p.poll()
  return p
}

const block = (p) => [...p.$('log-table').querySelectorAll('tr.entry')].find((r) => r.textContent.includes('kernel')).querySelector('button')
const posted = (p, path) => p.calls.filter((c) => c.path === path).map((c) => c.body)
const ask = (p) => ({ title: p.$('confirm-title').textContent, text: p.$('confirm-text').textContent, list: p.$('confirm-list').textContent, command: p.$('confirm-command').textContent })
const run = async (p) => { p.$('confirm').close('run'); await p.settle() }

test('Block on an unblocked host asks to remove the rule that allows it, and runs the removal, not a deny', async () => {
  const p = await logPage([unblocked([rule({ name: 'for kernel work' })])])
  try {
    await p.click(block(p))
    const q = ask(p)
    assert.match(q.title, /Block www\.kernel\.org:443\?/)
    assert.match(q.text, /remov/i)
    assert.match(q.list, /for kernel work/)
    assert.match(q.list, /www\.kernel\.org:443/)
    assert.match(q.command, /sbx policy rm network --sandbox box --id r-exact --force/)
    assert.doesNotMatch(q.text, /also allows/, 'a rule for this host alone is not said to allow more')
    await run(p)
    assert.deepEqual(posted(p, '/api/remove'), [{ id: 'r-exact' }])
    assert.deepEqual(posted(p, '/api/deny'), [], 'a deny was sent beside the allow rule: sbx refuses it')
    assert.match(p.$('toast').textContent, /blocked again/i)
  } finally { p.close() }
})

test('a rule that allows more than the host says so, naming what else it allows', async () => {
  const p = await logPage([unblocked([rule({ id: 'r-wild', resources: ['*.kernel.org', 'git.example.org'], wider: true })])])
  try {
    await p.click(block(p))
    const q = ask(p)
    assert.match(q.text, /also allows/)
    assert.match(q.text, /\*\.kernel\.org/)
    assert.match(q.text, /git\.example\.org/)
    assert.match(q.text, /blocked too/i)
  } finally { p.close() }
})

test('every rule that allows the host is removed, one after another', async () => {
  const p = await logPage([unblocked([rule({ id: 'one' }), rule({ id: 'two', resources: ['*.kernel.org'], wider: true })])])
  try {
    await p.click(block(p))
    assert.match(ask(p).command, /--id one --force[\s\S]*--id two --force/)
    await run(p)
    assert.deepEqual(posted(p, '/api/remove'), [{ id: 'one' }, { id: 'two' }])
  } finally { p.close() }
})

test('when a removal fails the rest are not tried, and the failure is said', async () => {
  let n = 0
  const p = await logPage([unblocked([rule({ id: 'one' }), rule({ id: 'two' })])], {
    '/api/remove': () => (n++ === 0 ? { status: 500, body: { error: 'sbx said no' } } : { command: 'x' }),
  })
  try {
    await p.click(block(p))
    await run(p)
    assert.deepEqual(posted(p, '/api/remove'), [{ id: 'one' }], 'went on after a failure')
    assert.match(p.$('toast').textContent, /sbx said no/)
  } finally { p.close() }
})

test('a rule that cannot be removed from here is explained, and nothing is sent', async () => {
  const p = await logPage([unblocked([rule({ id: 'ok-one' }), rule({ id: 'org-rule', name: 'company policy', removable: false, source: 'organisation' })])])
  try {
    await p.click(block(p))
    assert.equal(p.$('confirm').open, false, 'asked to confirm what cannot be done')
    assert.match(p.$('toast').textContent, /company policy/)
    assert.match(p.$('toast').textContent, /can't be removed here/)
    assert.match(p.$('toast').textContent, /organisation/)
    assert.deepEqual(posted(p, '/api/remove'), [], 'removed the rule that could be, leaving the host half unblocked')
    assert.deepEqual(posted(p, '/api/deny'), [])
  } finally { p.close() }
})

test('an unblocked host the server named no rule for is blocked as it always was', async () => {
  const p = await logPage([unblocked(null)])
  try {
    await p.click(block(p))
    assert.match(ask(p).command, /sbx policy deny network --sandbox box www\.kernel\.org:443/)
    await run(p)
    assert.deepEqual(posted(p, '/api/deny'), [{ host: 'www.kernel.org:443' }])
    assert.deepEqual(posted(p, '/api/remove'), [])
  } finally { p.close() }
})

test('Block on an allowed host, one never blocked, is as it always was', async () => {
  const p = await logPage([{ host: 'www.kernel.org:443', decision: 'allowed', hits: 4, lastSeen: '2026-11-12T12:39:25Z', rule: '' }])
  try {
    await p.click(block(p))
    assert.match(ask(p).command, /sbx policy deny network/)
    await run(p)
    assert.deepEqual(posted(p, '/api/deny'), [{ host: 'www.kernel.org:443' }])
    assert.deepEqual(posted(p, '/api/remove'), [])
  } finally { p.close() }
})
