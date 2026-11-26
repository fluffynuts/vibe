// The Activity tab: how a day's time split between repositories and branches, for a timesheet.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const branch = (repo, name, hours, commits = [], more = {}) => Object.assign({
  repo, branch: name, hours, commits, folders: ['~/code/' + repo],
  stretches: [{ start: '2026-11-26T09:10:00+02:00', end: '2026-11-26T11:40:00+02:00', measured: true }],
  measuredMinutes: 150, estimatedMinutes: 0,
}, more)

const today = {
  date: '2026-11-26', hours: 6.5,
  branches: [
    branch('phoenix', 'fix/some-bug', 2.5, [':bug: fix the root cause of the issue', ':nosuchemoji: kept as written']),
    branch('yumbi-web', 'feat/some-new-feature', 3, ['implemented new search button', 'fixed bug in the vicinity']),
    branch('yumbi-web', 'feat/some-other-feature', 1, ['merged master'], { estimatedMinutes: 40 }),
  ],
}

const ready = (key, ...bullets) => ({ key, state: 'ready', bullets })

async function pageWith({ days = { '': today }, attention = [], problems = [], sandbox = 'box', rounding = '30m', summaries = () => [] } = {}) {
  const kept = { rounding }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox, sandboxTabs: !!sandbox, activity: true, attention: {} },
      '/api/log': { entries: [] },
      '/api/activity': (call) => {
        const day = new URLSearchParams(call.query).get('day') || ''
        if (!days[day]) return { status: 400, body: { error: 'no such day' } }
        return { report: { day: days[day], today: '2026-11-26', earliest: '2026-11-19', problems: [] }, attention, problems, rounding: kept.rounding, summaries: summaries(day) }
      },
      '/api/activity/rounding': (call) => { kept.rounding = call.body.rounding; return { rounding: kept.rounding } },
      '/api/activity/seen': { ok: true },
      '/api/activity/summary/retry': (call) => { kept.retried = call.body.key; return { ok: true } },
    },
    storage: { 'vibe-companion-tab': 'activity' },
  })
  p.kept = kept
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
    assert.equal(p.$('activity-copy'), null, 'the day-wide Copy is gone: time is logged per branch')
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

const panes = (p, i) => [...p.$('activity-list').querySelectorAll('.activity-entry')[i].querySelectorAll('.activity-pane')]
const lines = (pane) => [...pane.querySelectorAll('li')].map((li) => li.textContent)

test("each branch's summary and commits are side by side, the commits' :emoji: codes drawn as emoji", async () => {
  const p = await pageWith({ summaries: () => [ready('k1', 'fixed the root cause'), ready('k2', 'new search button', 'prepare PR for review'), ready('k3', 'kept up with master')] })
  try {
    const [summary, commits] = panes(p, 0)
    assert.equal(summary.querySelector('h3').textContent, 'Summary')
    assert.deepEqual(lines(summary), ['fixed the root cause'])
    assert.equal(commits.querySelector('h3').textContent, 'Commits')
    assert.deepEqual(lines(commits), ['🐛 fix the root cause of the issue', ':nosuchemoji: kept as written'])
    assert.deepEqual(lines(panes(p, 1)[0]), ['new search button', 'prepare PR for review'])
  } finally { p.close() }
})

test('each side copies its own lines as bullets, with emoji as shown', async () => {
  let copied = null
  const p = await pageWith({ summaries: () => [ready('k1', 'fixed the root cause'), ready('k2', 'x'), ready('k3', 'y')] })
  try {
    Object.defineProperty(p.window.navigator, 'clipboard', { value: { writeText: async (t) => { copied = t } }, configurable: true })
    const [summary, commits] = panes(p, 0)
    await p.click(summary.querySelector('button.copy-code'))
    assert.equal(copied, '- fixed the root cause\n')
    await p.click(commits.querySelector('button.copy-code'))
    assert.equal(copied, '- 🐛 fix the root cause of the issue\n- :nosuchemoji: kept as written\n')
  } finally { p.close() }
})

test('the summary says while it is being made, here or on another vibe, and the tab asks again soon', async () => {
  let state = [{ key: 'k1', state: 'pending' }, { key: 'k2', state: 'elsewhere', holder: 'yumbi' }, { key: 'k3', state: 'none' }]
  const p = await pageWith({ summaries: () => state })
  try {
    assert.equal(panes(p, 0)[0].textContent.includes('Summarising…'), true)
    assert.match(panes(p, 1)[0].textContent, /Activity is being summarised on the yumbi vibe, and will appear here shortly\./)
    assert.match(panes(p, 2)[0].textContent, /No commits to summarise/)
    assert.equal(panes(p, 0)[0].querySelector('button.copy-code'), null, 'nothing to copy yet')
    state = [ready('k1', 'done now'), ready('k2', 'and here'), { key: 'k3', state: 'none' }]
    const realNow = p.window.Date.now
    p.window.Date.now = () => realNow() + 3500
    await p.poll()
    assert.deepEqual(lines(panes(p, 0)[0]), ['done now'], 'not asked again within a few seconds')
  } finally { p.close() }
})

test('a summary that failed says why, and Retry asks for it again', async () => {
  const p = await pageWith({ summaries: () => [{ key: 'k1', state: 'failed', why: "the agent isn't signed in" }, { key: 'k2', state: 'unavailable', why: 'Commit summaries are only shown for Claude agents.' }, { key: 'k3', state: 'none' }] })
  try {
    assert.match(panes(p, 0)[0].textContent, /Couldn't summarise: the agent isn't signed in/)
    assert.match(panes(p, 1)[0].textContent, /only shown for Claude agents/)
    const before = p.calls.filter((c) => c.path === '/api/activity').length
    await p.click(p.button('Retry', panes(p, 0)[0]))
    assert.equal(p.kept.retried, 'k1')
    assert.equal(p.calls.filter((c) => c.path === '/api/activity').length, before + 1)
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
