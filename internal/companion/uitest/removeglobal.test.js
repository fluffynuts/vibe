// Remove on the Network rules tab. sbx refuses to remove a rule of the global policy for one sandbox, so a global rule is explained,
// with the command that removes it for every sandbox, not offered. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const GLOBAL_ID = '44bca27d-0115-4557-a140-64c69019caf3'
const mine = { id: 'mine', name: 'mine', decision: 'allowed', resources: ['kernel.org'], source: 'scoped', scope: 'sandbox:box', protocol: 'tcp', removable: true }
const global = { id: GLOBAL_ID, name: GLOBAL_ID, decision: 'allowed', resources: ['mirrors.kernel.org'], source: 'local', scope: 'global', protocol: 'tcp', removable: true }
const bigGlobal = { ...global, id: 'big-global', name: 'big-global', resources: Array.from({ length: 25 }, (_, i) => 'host' + i + '.example.org:443') }

async function rulesPage(rules) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
      '/api/rules': { rules },
      '/api/remove': { command: 'sbx policy rm network ...' },
    },
    storage: { 'vibe-companion-tab': 'rules' },
  })
  await p.poll()
  return p
}

const removeButton = (p, text) => [...p.$('rules-table').tBodies[0].querySelectorAll('tr.entry')].find((r) => r.textContent.includes(text)).querySelector('button')
const posted = (p) => p.calls.filter((c) => c.path === '/api/remove').map((c) => c.body)

test("Remove on a rule of this sandbox's own says what it is in words that read, and runs the sandbox's removal", async () => {
  const p = await rulesPage([mine, global])
  try {
    await p.click(removeButton(p, 'kernel.org'))
    assert.equal(p.$('confirm').open, true)
    assert.equal(p.$('confirm-text').textContent, 'kernel.org — an allow rule for this sandbox.')
    assert.match(p.$('confirm-command').textContent, /sbx policy rm network --sandbox box --id mine --force/)
    p.$('confirm').close('run')
    await p.settle()
    assert.deepEqual(posted(p), [{ id: 'mine' }])
  } finally { p.close() }
})

test('Remove on a global rule explains it, with the command for every sandbox, and sends nothing', async () => {
  const p = await rulesPage([mine, global])
  try {
    await p.click(removeButton(p, 'mirrors.kernel.org'))
    assert.equal(p.$('confirm').open, false, 'asked to confirm what sbx will refuse')
    const said = p.$('toast').textContent
    assert.match(said, /mirrors\.kernel\.org is a global rule/)
    assert.match(said, /every sandbox/)
    assert.ok(said.includes('sbx policy rm network --id ' + GLOBAL_ID), 'no command that removes it: ' + said)
    assert.doesNotMatch(said, /--sandbox/)
    assert.deepEqual(posted(p), [])
  } finally { p.close() }
})

test('a global rule with many resources is named by a few of them, not all', async () => {
  const p = await rulesPage([bigGlobal])
  try {
    await p.click(removeButton(p, 'host0.example.org'))
    const said = p.$('toast').textContent
    assert.match(said, /host0\.example\.org:443, host1\.example\.org:443, host2\.example\.org:443 and 22 more is a global rule/)
    assert.doesNotMatch(said, /host24/)
  } finally { p.close() }
})
