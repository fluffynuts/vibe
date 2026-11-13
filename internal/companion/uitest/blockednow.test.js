// A host allowed when last seen and blocked by a rule since says "Blocked now" and offers Allow, not the Allowed and Block it was
// drawn with: the log only records requests, and a Block from the page leaves the Allowed entry as the host's latest.
// In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const deny = { id: 'd-1', name: '', resources: ['git.kernel.org:443'], source: 'local', scope: 'sandbox:box', removable: true, wider: false, identical: true }
const entries = () => [
  { host: 'git.kernel.org:443', decision: 'blocked-now', hits: 4, lastSeen: '2026-11-13T14:56:55Z', rule: '', blockedBy: [deny] },
  { host: 'a.org:443', decision: 'blocked', hits: 1, lastSeen: '2026-11-13T14:56:00Z', rule: '' },
  { host: 'b.org:443', decision: 'allowed', hits: 1, lastSeen: '2026-11-13T14:56:00Z', rule: '' },
]

async function logPage() {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': () => ({ entries: entries() }),
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  await p.poll()
  return p
}

const row = (p, host) => [...p.$('log-table').tBodies[0].querySelectorAll('tr.entry')].find((r) => r.textContent.includes(host))

test('the row says Blocked now, with the colour of a block and a title that says why', async () => {
  const p = await logPage()
  try {
    const status = row(p, 'git.kernel.org').querySelector('.status')
    assert.equal(status.textContent, 'Blocked now')
    assert.ok(status.classList.contains('blocked-now'))
    assert.match(status.title, /allowed when last seen/i)
  } finally { p.close() }
})

test('it offers Allow, which names the rule that blocks it', async () => {
  const p = await logPage()
  try {
    const button = row(p, 'git.kernel.org').querySelector('button')
    assert.equal(button.textContent, 'Allow')
    button.click()
    await new Promise((resolve) => setImmediate(resolve))
    assert.match(p.$('confirm-command').textContent, /sbx policy rm network --sandbox box --id d-1 --force/)
  } finally { p.close() }
})

test('it counts as blocked, and shows under the Blocked filter', async () => {
  const p = await logPage()
  try {
    assert.match(p.$('log-summary').textContent.replace(/\s+/g, ' '), /2 blocked/)
    await p.click(p.$('log-filter').querySelector('[data-filter=blocked]'))
    assert.ok(row(p, 'git.kernel.org'), 'it vanished under the Blocked filter')
    assert.equal(row(p, 'b.org'), undefined)
  } finally { p.close() }
})
