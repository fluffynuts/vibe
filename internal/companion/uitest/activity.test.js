// The Activity tab: how a day's time split between repositories and branches, for a timesheet.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const branch = (repo, name, hours, bullets = [], more = {}) => Object.assign({
  repo, branch: name, hours, bullets, folders: ['~/code/' + repo],
  stretches: [{ start: '2026-11-26T09:10:00+02:00', end: '2026-11-26T11:40:00+02:00', measured: true }],
  measuredMinutes: 150, estimatedMinutes: 0,
}, more)

const today = {
  date: '2026-11-26', hours: 6.5,
  branches: [
    branch('phoenix', 'fix/some-bug', 2.5, ['fix the root cause of the issue']),
    branch('yumbi-web', 'feat/some-new-feature', 3, ['implemented new search button', 'fixed bug in the vicinity']),
    branch('yumbi-web', 'feat/some-other-feature', 1, ['merged master'], { estimatedMinutes: 40 }),
  ],
}

async function pageWith({ days = { '': today }, attention = [], problems = [], sandbox = 'box', rounding = '30m' } = {}) {
  const kept = { rounding }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox, sandboxTabs: !!sandbox, activity: true, attention: {} },
      '/api/log': { entries: [] },
      '/api/activity': (call) => {
        const day = new URLSearchParams(call.query).get('day') || ''
        if (!days[day]) return { status: 400, body: { error: 'no such day' } }
        return { report: { day: days[day], today: '2026-11-26', earliest: '2026-11-19', problems: [] }, attention, problems, rounding: kept.rounding }
      },
      '/api/activity/rounding': (call) => { kept.rounding = call.body.rounding; return { rounding: kept.rounding } },
      '/api/activity/seen': { ok: true },
    },
    storage: { 'vibe-companion-tab': 'activity' },
  })
  return p
}

const rows = (p) => [...p.$('activity-list').querySelectorAll('summary')].map((s) => [...s.children].map((c) => c.textContent).filter(Boolean).join(' | '))

test('the tab shows each branch with its rounded hours, share of the day and what was done', async () => {
  const p = await pageWith()
  try {
    assert.equal(p.$('tab-activity').hidden, false)
    assert.equal(p.$('panel-activity').hidden, false)
    assert.equal(p.$('activity-day').textContent, 'Today')
    assert.equal(p.$('activity-total').textContent, 'total 6.5')
    assert.deepEqual(rows(p), [
      'phoenix | fix/some-bug | 2.5 | 38%',
      'yumbi-web | feat/some-new-feature | 3.0 | 46%',
      'yumbi-web | feat/some-other-feature | 1.0 | 15% | ~estimated',
    ])
    const bullets = [...p.$('activity-list').querySelectorAll('.activity-bullets li')].map((li) => li.textContent)
    assert.deepEqual(bullets, ['fix the root cause of the issue', 'implemented new search button', 'fixed bug in the vicinity', 'merged master'])
    assert.equal(p.$('activity-none').hidden, true)
  } finally { p.close() }
})

test('a branch opens to show when it was worked on, measured and estimated, and in which folders', async () => {
  const p = await pageWith()
  try {
    const more = p.$('activity-list').querySelectorAll('details')[2].querySelector('.activity-more').textContent
    assert.match(more, /When: \d\d:\d\d( [AP]M)?–\d\d:\d\d( [AP]M)?Before/)
    assert.match(more, /Before rounding: 2\.5 measured, ~0\.67 estimated/)
    assert.match(more, /In: ~\/code\/yumbi-web/)
  } finally { p.close() }
})

test('Copy gives the day as text for a timesheet: each repository, its branches with their hours, and the bullets', async () => {
  let copied = null
  const p = await pageWith()
  try {
    Object.defineProperty(p.window.navigator, 'clipboard', { value: { writeText: async (t) => { copied = t } }, configurable: true })
    await p.click('activity-copy')
    assert.equal(copied, [
      '[phoenix]',
      '  [fix/some-bug] 2.5',
      '  - fix the root cause of the issue',
      '[yumbi-web]',
      '  [feat/some-new-feature] 3.0',
      '  - implemented new search button',
      '  - fixed bug in the vicinity',
      '  [feat/some-other-feature] 1.0',
      '  - merged master',
      '',
    ].join('\n'))
  } finally { p.close() }
})

test('the arrows step through the days kept, and the day returns to today', async () => {
  const yesterday = { date: '2026-11-25', hours: 0, branches: [] }
  const p = await pageWith({ days: { '': today, '2026-11-25': yesterday } })
  try {
    assert.equal(p.$('activity-next').disabled, true, "there's no going past today")
    await p.click('activity-prev')
    assert.equal(p.$('activity-day').textContent, 'Yesterday')
    assert.equal(p.$('activity-none').hidden, false)
    assert.equal(p.$('activity-copy').disabled, true)
    assert.equal(p.$('activity-next').disabled, false)
    await p.click('activity-day')
    assert.equal(p.$('activity-day').textContent, 'Today')
    assert.equal(rows(p).length, 3)
    const asked = p.calls.filter((c) => c.path === '/api/activity').map((c) => c.query)
    assert.ok(asked.includes('?day=2026-11-25'), 'asked for ' + JSON.stringify(asked))
  } finally { p.close() }
})

test('the rounding chosen is shown, and choosing another keeps it and reads the day again', async () => {
  const p = await pageWith()
  try {
    const pressed = () => [...p.$('activity-rounding').querySelectorAll('[aria-pressed=true]')].map((b) => b.dataset.rounding)
    assert.deepEqual(pressed(), ['30m'])
    const before = p.calls.filter((c) => c.path === '/api/activity').length
    await p.click(p.$('activity-rounding').querySelector('[data-rounding="15m"]'))
    assert.deepEqual(p.sent('/api/activity/rounding'), [{ rounding: '15m' }])
    assert.deepEqual(pressed(), ['15m'])
    assert.equal(p.calls.filter((c) => c.path === '/api/activity').length, before + 1)
  } finally { p.close() }
})

test('needs attention lists what wants doing now, saying when GitHub is being asked', async () => {
  const attention = [
    { folder: '~/code/yumbi', branch: 'feat/x', what: 'uncommitted changes: please commit and push' },
    { folder: '~/code/phoenix', branch: 'fix/y', what: '3 commits not pushed', checking: true },
  ]
  const p = await pageWith({ attention, problems: ["couldn't read git in ~/code/broken: it is damaged"] })
  try {
    assert.equal(p.$('activity-attention').hidden, false)
    const items = [...p.$('activity-attention-list').querySelectorAll('li')].map((li) => li.textContent)
    assert.deepEqual(items, ['~/code/yumbi (feat/x): uncommitted changes: please commit and push', '~/code/phoenix (fix/y): 3 commits not pushed (checking with GitHub…)'])
    assert.equal(p.$('activity-problems').hidden, false)
    assert.match(p.$('activity-problems').textContent, /it is damaged/)
  } finally { p.close() }
})

test('a poll that brings the same day leaves the rows as they are, an opened one open', async () => {
  const p = await pageWith()
  try {
    const first = p.$('activity-list').querySelector('details')
    first.open = true
    first.dispatchEvent(new p.window.Event('toggle'))
    p.window.Date.now = ((now) => () => now() + 60000)(p.window.Date.now)
    await p.poll()
    assert.equal(p.$('activity-list').querySelector('details'), first, 'the rows were drawn again')
    assert.equal(first.open, true)
  } finally { p.close() }
})

test('typing or clicking on a sandbox page says the user is busy, at most once a minute', async () => {
  const p = await pageWith()
  try {
    p.window.document.body.dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    p.window.document.body.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Shift', bubbles: true }))
    await p.settle()
    assert.equal(p.sent('/api/activity/seen').length, 1)
  } finally { p.close() }
})

test('the page for no sandbox has the tab, and says nobody is busy on a sandbox', async () => {
  const p = await pageWith({ sandbox: '' })
  try {
    assert.equal(p.$('tab-activity').hidden, false)
    p.window.document.body.dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    await p.settle()
    assert.equal(p.sent('/api/activity/seen').length, 0)
  } finally { p.close() }
})
