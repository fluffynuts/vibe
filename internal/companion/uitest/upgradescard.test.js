// Tests of the Admin tab's Upgrades card, in jsdom (see page.js): it always says which vibe and Docker SBX are in use,
// and what `vibe --upgrade -f` will do.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

function adminPage(upgrades) {
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, attention: {} },
      '/api/admin': { status: { exists: true, running: true }, upgrades },
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
}

const versions = (p) => p.$('admin-versions').textContent

test("this vibe's version is said at once; Docker SBX's while it is being found out", async () => {
  const p = await adminPage({ vibe: '0.1.236', checking: true })
  try {
    assert.equal(versions(p), 'vibe 0.1.236 · Docker SBX: checking…')
  } finally { p.close() }
})

test('once checked, both versions are said, with nothing newer', async () => {
  const p = await adminPage({ vibe: '0.1.236', checked: true, found: [], sbx: 'v0.54.0', vibeInstalled: '0.1.236' })
  try {
    assert.equal(versions(p), 'vibe 0.1.236 · Docker SBX v0.54.0')
  } finally { p.close() }
})

test('a vibe installed that is not the one running says a restart picks it up', async () => {
  const p = await adminPage({ vibe: '0.1.235', checked: true, found: [], sbx: 'v0.54.0', vibeInstalled: '0.1.236' })
  try {
    assert.equal(versions(p), 'vibe 0.1.235 (0.1.236 installed: restart this sandbox\'s vibe to use it) · Docker SBX v0.54.0')
  } finally { p.close() }
})

test('before any check, Docker SBX is said to be not checked yet', async () => {
  const p = await adminPage({ vibe: '0.1.236' })
  try {
    assert.equal(versions(p), 'vibe 0.1.236 · Docker SBX: not checked yet')
  } finally { p.close() }
})

test("a Docker SBX that couldn't be told is said so", async () => {
  const p = await adminPage({ vibe: '0.1.236', checked: true, found: [], sbx: '' })
  try {
    assert.equal(versions(p), "vibe 0.1.236 · Docker SBX: couldn't tell")
  } finally { p.close() }
})

test('with a Docker SBX upgrade waiting, upgrading everything unasked says it stops the running sandboxes, this one too', async () => {
  const p = await adminPage({ vibe: '0.1.236', checked: true, found: ['Docker SBX v0.54.0 → v0.55.0'], sbx: 'v0.54.0', sbxUpgrade: true })
  try {
    const command = p.$('admin-upgrade-command').textContent
    assert.match(command, /vibe --upgrade -f/)
    assert.match(command, /stops every running sandbox first, this one too/)
  } finally { p.close() }
})

test('with only a vibe upgrade waiting, upgrading everything unasked stops nothing', async () => {
  const p = await adminPage({ vibe: '0.1.235', checked: true, found: ['vibe 0.1.235 → 0.1.236'], sbx: 'v0.54.0' })
  try {
    const command = p.$('admin-upgrade-command').textContent
    assert.match(command, /vibe --upgrade -f/)
    assert.doesNotMatch(command, /stops/)
  } finally { p.close() }
})
