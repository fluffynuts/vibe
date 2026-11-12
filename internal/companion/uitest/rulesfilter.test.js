// The Network rules and Filesystem rules tabs: a filter like the Network log's, and the sign that the rules are being read. In jsdom
// (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const rules = [
  { id: 'big', name: 'big', decision: 'allow', resources: ['api.anthropic.com:443', 'registry.npmjs.org', 'github.com'], source: 'scoped', scope: 'sandbox:box', protocol: 'tcp', removable: false },
  { id: 'wild', name: 'wild', decision: 'allow', resources: ['*.kernel.org'], source: 'scoped', scope: 'sandbox:box', protocol: 'tcp', removable: true },
  { id: 'www', name: 'www', decision: 'allow', resources: ['www.kernel.org'], source: 'scoped', scope: 'sandbox:box', protocol: 'tcp', removable: true },
  { id: 'glob', name: 'glob', decision: 'allow', resources: ['mirrors.example.net'], source: 'local', scope: 'global', protocol: 'tcp', removable: true },
]

async function rulesPage(tab, { held = null } = {}) {
  const at = { rules, held }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
      '/api/rules': () => at.held || { rules: at.rules },
      '/api/fs-rules': () => at.held || { rules: at.rules.map((r) => ({ ...r, status: 'active' })) },
      '/api/remove': { command: 'sbx policy rm ...' },
    },
    storage: { 'vibe-companion-tab': tab },
  })
  await p.poll()
  return Object.assign(p, { at })
}

const TABLES = { rules: ['rules-table', 'rules-search', 'rules-refreshing'], fs: ['fs-table', 'fs-search', 'fs-refreshing'] }
const rows = (p, tab) => [...p.$(TABLES[tab][0]).tBodies[0].querySelectorAll('tr.entry')].map((r) => r.textContent.replace(/\s+/g, ' ').trim())
const filter = async (p, tab, text) => { await p.type(p.$(TABLES[tab][1]), text); await p.settle() }

for (const tab of ['rules', 'fs']) {
  const name = tab === 'rules' ? 'Network rules' : 'Filesystem rules'

  test(`${name}: typing in the filter keeps the rules with something that matches, whole, whatever the case`, async () => {
    const p = await rulesPage(tab)
    try {
      assert.equal(rows(p, tab).length, 4)
      await filter(p, tab, 'KERNEL')
      const shown = rows(p, tab)
      assert.equal(shown.length, 2)
      assert.ok(shown.every((r) => /kernel\.org/.test(r)))
      await filter(p, tab, 'npmjs')
      const big = rows(p, tab)
      assert.equal(big.length, 1)
      assert.match(big[0], /api\.anthropic\.com/, 'a rule is shown with all its resources, not only the one that matched')
      assert.match(big[0], /github\.com/)
    } finally { p.close() }
  })

  test(`${name}: the filter also matches a rule's source and what it applies to`, async () => {
    const p = await rulesPage(tab)
    try {
      await filter(p, tab, 'global')
      const shown = rows(p, tab)
      assert.equal(shown.length, 1)
      assert.match(shown[0], /mirrors\.example\.net/)
    } finally { p.close() }
  })

  test(`${name}: nothing matching says so, and clearing the filter brings every rule back`, async () => {
    const p = await rulesPage(tab)
    try {
      await filter(p, tab, 'no-such-host')
      assert.equal(rows(p, tab).length, 0)
      assert.match(p.$(TABLES[tab][0]).tBodies[0].textContent, /Nothing matches/)
      await filter(p, tab, '')
      assert.equal(rows(p, tab).length, 4)
    } finally { p.close() }
  })

  test(`${name}: polls that bring nothing new change nothing while a filter is typed`, async () => {
    const p = await rulesPage(tab)
    try {
      await filter(p, tab, 'kernel')
      const seen = []
      const observer = new p.window.MutationObserver((records) => seen.push(...records))
      observer.observe(p.$(TABLES[tab][0]), { childList: true, characterData: true, subtree: true })
      await p.poll(); await p.poll()
      assert.equal(seen.length, 0, 'the table was drawn again though nothing changed: a selection in it would be lost')
    } finally { p.close() }
  })

  test(`${name}: a rule left open stays open when the filter changes, as the same rule`, async () => {
    const p = await rulesPage(tab)
    try {
      const open = [...p.$(TABLES[tab][0]).tBodies[0].querySelectorAll('tr.entry')].find((r) => r.textContent.includes('www.kernel.org'))
      await p.click(open)
      assert.ok(p.$(TABLES[tab][0]).tBodies[0].querySelector('.detail, tr:not(.entry)'), 'the rule did not open')
      const detailsBefore = p.$(TABLES[tab][0]).tBodies[0].querySelectorAll('tr:not(.entry)').length
      await filter(p, tab, 'kernel')
      assert.equal(p.$(TABLES[tab][0]).tBodies[0].querySelectorAll('tr:not(.entry)').length, detailsBefore, 'the open rule closed when the list was filtered')
    } finally { p.close() }
  })

  test(`${name}: says it is reading the rules while it is, and stops when it has`, async () => {
    let release
    const p = await rulesPage(tab)
    try {
      p.at.held = new Promise((resolve) => { release = () => resolve({ rules }) })
      await p.kick('tick')
      const sign = p.$(TABLES[tab][2])
      assert.ok(sign.classList.contains('on'), 'no sign that the rules are being read')
      assert.equal(sign.classList.contains('at-once'), false, 'an ordinary poll shows it at once')
      release()
      await p.settle()
      assert.equal(sign.classList.contains('on'), false)
    } finally { p.close() }
  })
}

test('Network rules: after a rule is changed the sign shows at once', async () => {
  let release
  const p = await rulesPage('rules')
  try {
    const remove = [...p.$('rules-table').tBodies[0].querySelectorAll('tr.entry')].find((r) => r.textContent.includes('*.kernel.org')).querySelector('button')
    await p.click(remove)
    p.at.held = new Promise((resolve) => { release = () => resolve({ rules: rules.filter((r) => r.id !== 'wild') }) })
    p.$('confirm').close('run')
    for (let i = 0; i < 30; i++) await new Promise((resolve) => setImmediate(resolve))
    const sign = p.$('rules-refreshing')
    assert.ok(sign.classList.contains('on') && sign.classList.contains('at-once'), 'no sign at once after the rule was removed')
    release()
    await p.settle()
    assert.equal(sign.classList.contains('on'), false)
    assert.equal(sign.classList.contains('at-once'), false)
  } finally { p.close() }
})

test('typing Enter in the rules filter does not submit the form that adds a rule', async () => {
  const p = await rulesPage('rules')
  try {
    const before = p.calls.length
    const input = p.$('rules-search')
    input.value = 'kernel'
    input.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await p.settle()
    assert.equal(input.closest('form'), null, 'the filter is inside the form that adds a rule: Enter would try to allow a host')
    assert.equal(p.$('confirm').open, false)
    assert.ok(p.calls.length >= before)
  } finally { p.close() }
})
