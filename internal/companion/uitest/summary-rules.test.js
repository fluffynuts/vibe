// The Activity tab's summary rules, edited on the Admin tab: the day's and the week's, saved unless changed since loaded, put back
// to the default on asking; the Activity tab's (i) links to them. In jsdom (see page.js).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const files = () => [
  { which: 'day', path: '~/.vibe/activity/summary-rules.md', text: 'Day rules.\n', default: 'Day rules.\n' },
  { which: 'week', path: '~/.vibe/activity/week-summary-rules.md', text: 'My week rules.\n', default: 'Week rules.\n' },
]

async function adminPage({ rules = { files: files() }, save = null, tab = 'admin' } = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, activity: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': { status: { exists: true, running: true, profile: 'p' }, job: null, upgrades: {} },
      '/api/activity': { report: { day: { date: '2026-11-26', hours: 0, branches: [] }, today: '2026-11-26', earliest: '2026-11-13', problems: [] }, attention: [], problems: [], rounding: '30m', summaries: [] },
      '/api/activity/rules': (call) => (call.method === 'POST' ? (save ? save(call) : { which: call.body.which, text: call.body.text }) : rules),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': tab },
  })
  await p.settle()
  return p
}

const editor = (p, which) => p.$('admin-rules').querySelector('textarea[data-rules="' + which + '"]').closest('.instr')
const box = (p, which) => editor(p, which).querySelector('textarea')
const button = (p, which, text) => [...editor(p, which).querySelectorAll('button')].find((b) => b.textContent === text)
const typeIn = (p, node, text) => { node.value = text; node.dispatchEvent(new p.window.Event('input', { bubbles: true })) }

test("the Admin tab shows the day's and the week's rules, each with its file, and whether it is the default", async () => {
  const p = await adminPage()
  try {
    assert.equal(p.$('admin-rules-card').hidden, false)
    assert.equal(box(p, 'day').value, 'Day rules.\n')
    assert.match(editor(p, 'week').textContent, /week-summary-rules\.md/)
    assert.match(editor(p, 'day').querySelector('.meta').textContent, /The default/)
    assert.match(editor(p, 'week').querySelector('.meta').textContent, /Your own/)
    assert.equal(button(p, 'day', 'Save').disabled, true, 'nothing to save')
  } finally { p.close() }
})

test('a change is saved with the text it was loaded as, and the next save goes from what was saved', async () => {
  const p = await adminPage()
  try {
    typeIn(p, box(p, 'week'), 'Shorter.\n')
    assert.match(editor(p, 'week').querySelector('.meta').textContent, /Unsaved/)
    await p.click(button(p, 'week', 'Save'))
    typeIn(p, box(p, 'week'), 'Shorter still.\n')
    await p.click(button(p, 'week', 'Save'))
    assert.deepEqual(p.sent('/api/activity/rules'), [
      { which: 'week', text: 'Shorter.\n', base: 'My week rules.\n' },
      { which: 'week', text: 'Shorter still.\n', base: 'Shorter.\n' },
    ])
    assert.match(editor(p, 'week').querySelector('.status').textContent, /Saved/)
  } finally { p.close() }
})

test('a save refused because the file changed says so, and Reload reads it again', async () => {
  const p = await adminPage({ save: () => ({ status: 409, body: { error: 'the rules were changed since they were loaded here' } }) })
  try {
    typeIn(p, box(p, 'day'), 'Mine.\n')
    await p.click(button(p, 'day', 'Save'))
    assert.match(editor(p, 'day').querySelector('.status').textContent, /changed since they were loaded/)
    const reload = button(p, 'day', 'Reload')
    assert.equal(reload.hidden, false)
    const before = p.calls.filter((c) => c.path === '/api/activity/rules' && c.method !== 'POST').length
    await p.click(reload)
    assert.ok(p.calls.filter((c) => c.path === '/api/activity/rules' && c.method !== 'POST').length > before, 'not read again')
  } finally { p.close() }
})

test('"Back to the default" puts the default in the box, unsaved', async () => {
  const p = await adminPage()
  try {
    await p.click(button(p, 'week', 'Back to the default'))
    assert.equal(box(p, 'week').value, 'Week rules.\n')
    assert.equal(button(p, 'week', 'Save').disabled, false)
    assert.deepEqual(p.sent('/api/activity/rules'), [], 'nothing saved yet')
  } finally { p.close() }
})

test('a page that makes no summaries has no rules card', async () => {
  const p = await adminPage({ rules: { status: 404, body: { error: 'this page makes no summaries' } } })
  try {
    assert.equal(p.$('admin-rules-card').hidden, true)
  } finally { p.close() }
})

test("the Activity tab's (i) links to the rules, on the Admin tab", async () => {
  const p = await adminPage({ tab: 'activity' })
  try {
    await p.click('activity-help')
    assert.equal(p.$('activity-rules-link').hidden, false)
    await p.click('activity-rules-link')
    assert.equal(p.$('panel-admin').hidden, false)
    await p.settle()
    assert.equal(p.window.document.activeElement.dataset.rules, 'day')
  } finally { p.close() }
})
