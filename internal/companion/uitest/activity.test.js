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
  date: '2026-11-26', hours: 9,
  branches: [
    branch('phoenix', 'fix/some-bug', 5, [':bug: fix the root cause of the issue', ':nosuchemoji: kept as written']),
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
      '/api/activity/ticket': { ok: true },
    },
    storage: { 'vibe-companion-tab': 'activity' },
  })
  p.kept = kept
  return p
}

const rows = (p) => [...p.$('activity-list').querySelectorAll('.activity-toggle')].map((s) => [...s.children].map((c) => c.textContent.trim()).filter(Boolean).join(' | '))
const groups = (p) => [...p.$('activity-list').querySelectorAll('.activity-group-head')].map((s) => [...s.children].map((c) => c.textContent.trim()).filter(Boolean).join(' | '))
const entry = (p, i) => p.$('activity-list').querySelectorAll('.activity-entry')[i]
const open = (p, i) => p.click(entry(p, i).querySelector('.activity-toggle'))
const panes = (p, i) => [...entry(p, i).querySelectorAll('.activity-pane')]
const lines = (pane) => [...pane.querySelectorAll('li')].map((li) => li.textContent)

test('the rows start closed: each branch with its rounded hours and share of the day', async () => {
  const p = await pageWith()
  try {
    assert.equal(p.$('tab-activity').hidden, false)
    assert.equal(p.$('panel-activity').hidden, false)
    assert.equal(p.$('activity-total').textContent, 'total 9.0')
    assert.deepEqual(rows(p), [
      'fix/some-bug | 5.0 | 56%',
      'feat/some-new-feature | 3.0 | 33%',
      'feat/some-other-feature | 1.0 | 11% | ~estimated',
    ])
    assert.equal(p.$('activity-list').querySelectorAll('.activity-pane').length, 0, 'no summary or commits until a row is opened')
    for (const t of p.$('activity-list').querySelectorAll('.activity-toggle')) assert.equal(t.getAttribute('aria-expanded'), 'false')
    assert.equal(p.$('activity-copy'), null, 'the day-wide Copy is gone: time is logged per branch')
  } finally { p.close() }
})

test('opening a row shows its summary and commits; opening another closes it', async () => {
  const p = await pageWith({ summaries: () => [ready('k1', 'fixed the root cause'), ready('k2', 'new search button'), ready('k3', 'kept up')] })
  try {
    await open(p, 0)
    assert.equal(entry(p, 0).querySelector('.activity-toggle').getAttribute('aria-expanded'), 'true')
    assert.deepEqual(lines(panes(p, 0)[0]), ['fixed the root cause'])
    await open(p, 1)
    assert.equal(panes(p, 0).length, 0, 'the first row closed')
    assert.deepEqual(lines(panes(p, 1)[0]), ['new search button'])
    await open(p, 1)
    assert.equal(p.$('activity-list').querySelectorAll('.activity-pane').length, 0, 'clicking the open row closes it')
  } finally { p.close() }
})

test("each branch's summary and commits are side by side, the commits' :emoji: codes drawn as emoji", async () => {
  const p = await pageWith({ summaries: () => [ready('k1', 'fixed the root cause'), ready('k2', 'new search button', 'prepare PR for review'), ready('k3', 'kept up with master')] })
  try {
    await open(p, 0)
    const [summary, commits] = panes(p, 0)
    assert.equal(summary.querySelector('h3').textContent, 'Summary')
    assert.deepEqual(lines(summary), ['fixed the root cause'])
    assert.equal(commits.querySelector('h3').textContent, 'Commits')
    assert.deepEqual(lines(commits), ['🐛 fix the root cause of the issue', ':nosuchemoji: kept as written'])
  } finally { p.close() }
})

test('each side copies its own lines as bullets, with emoji as shown', async () => {
  let copied = null
  const p = await pageWith({ summaries: () => [ready('k1', 'fixed the root cause'), ready('k2', 'x'), ready('k3', 'y')] })
  try {
    Object.defineProperty(p.window.navigator, 'clipboard', { value: { writeText: async (t) => { copied = t } }, configurable: true })
    await open(p, 0)
    const [summary, commits] = panes(p, 0)
    await p.click(summary.querySelector('button.copy-code'))
    assert.equal(copied, '- fixed the root cause\n')
    await p.click(commits.querySelector('button.copy-code'))
    assert.equal(copied, '- 🐛 fix the root cause of the issue\n- :nosuchemoji: kept as written\n')
  } finally { p.close() }
})

test("a row's (i) shows when and where it was worked on, without opening the row", async () => {
  const p = await pageWith()
  try {
    const info = entry(p, 2).querySelector('.activity-info')
    await p.click(info)
    const balloon = p.$('activity-row-balloon')
    assert.equal(balloon.hidden, false)
    assert.equal(info.getAttribute('aria-expanded'), 'true')
    assert.match(balloon.textContent, /yumbi-web · feat\/some-other-feature/)
    assert.match(balloon.textContent, /When\d\d:\d\d( [AP]M)?–\d\d:\d\d/)
    assert.match(balloon.textContent, /Before rounding2\.5 measured, ~0\.67 estimated/)
    assert.match(balloon.textContent, /In~\/code\/yumbi-web/)
    assert.equal(panes(p, 2).length, 0, 'the row stays closed')
    await p.click(info)
    assert.equal(balloon.hidden, true, 'its (i) again closes it')
  } finally { p.close() }
})

test('the help is in a balloon from the (i) in the bar, closed by the (i), Escape or a click elsewhere', async () => {
  const p = await pageWith()
  try {
    const help = p.$('activity-help-balloon')
    assert.equal(help.hidden, true)
    await p.click('activity-help')
    assert.equal(help.hidden, false)
    assert.match(help.textContent, /rounded to the nearest step/)
    await p.click('activity-help')
    assert.equal(help.hidden, true)
    await p.click('activity-help')
    p.$('activity-total').dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    assert.equal(help.hidden, true, 'a click elsewhere closes it')
    await p.click('activity-help')
    help.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.equal(help.hidden, true, 'Escape closes it')
  } finally { p.close() }
})

test("a row with no time once rounded and no commits is hidden, behind a line that shows it", async () => {
  const day = { date: '2026-11-26', hours: 1, branches: [
    branch('vibe', 'master', 0), branch('vibe', 'feat/x', 1, ['work']), branch('vibe', 'fix/quick', 0, ['a quick fix']),
  ] }
  const p = await pageWith({ days: { '': day } })
  try {
    assert.deepEqual(rows(p).map((r) => r.split(' | ')[0]), ['feat/x', 'fix/quick'], 'a short branch with commits still shows')
    assert.equal(p.$('activity-hidden').hidden, false)
    assert.match(p.$('activity-hidden').textContent, /1 hidden: no time once rounded, and no commits\. show/)
    await p.click('activity-hidden-toggle')
    assert.deepEqual(rows(p).map((r) => r.split(' | ')[0]), ['feat/x', 'fix/quick', 'master'])
    assert.equal(p.$('activity-hidden-toggle').textContent, 'hide')
  } finally { p.close() }
})

test('the date shows in the bar; Return to today shows only on another day; the date opens a list of the days kept', async () => {
  const yesterday = { date: '2026-11-25', hours: 0, branches: [] }
  const days = { '': today }
  for (let d = 19; d <= 25; d++) days['2026-11-' + d] = { date: '2026-11-' + d, hours: 0, branches: [] }
  days['2026-11-25'] = yesterday
  const p = await pageWith({ days })
  try {
    assert.match(p.$('activity-day').textContent, /Thursday/)
    assert.match(p.$('activity-day').textContent, /26/)
    assert.equal(p.$('activity-today').hidden, true, 'no Return to today on today')
    assert.equal(p.$('activity-next').disabled, true)
    await p.click('activity-prev')
    assert.match(p.$('activity-day').textContent, /Wednesday/)
    assert.equal(p.$('activity-today').hidden, false)
    assert.equal(p.$('activity-none').hidden, false)
    await p.click('activity-today')
    assert.match(p.$('activity-day').textContent, /Thursday/)
    assert.equal(p.$('activity-today').hidden, true)
    await p.click('activity-day')
    const list = p.$('activity-days-list')
    assert.equal(list.hidden, false)
    const items = [...list.querySelectorAll('button')]
    assert.equal(items.length, 8, 'today and the week before it')
    assert.equal(items[0].textContent, 'Today')
    assert.equal(items[1].textContent, 'Yesterday')
    assert.equal(items[0].getAttribute('aria-current'), 'date')
    await p.click(items[7])
    assert.equal(list.hidden, true)
    assert.ok(p.calls.some((c) => c.path === '/api/activity' && c.query === '?day=2026-11-19'), 'asked for the day chosen')
    assert.equal(p.$('activity-prev').disabled, true, 'no going past the week kept')
  } finally { p.close() }
})

test('needs attention shows on today only, saying when GitHub is being asked', async () => {
  const attention = [
    { folder: '~/code/yumbi', branch: 'feat/x', what: 'uncommitted changes: please commit and push' },
    { folder: '~/code/phoenix', branch: 'fix/y', what: '3 commits not pushed', checking: true },
  ]
  const p = await pageWith({ attention, problems: ["couldn't read git in ~/code/broken: it is damaged"], days: { '': today, '2026-11-25': { date: '2026-11-25', hours: 0, branches: [] } } })
  try {
    assert.equal(p.$('activity-attention').hidden, false)
    assert.equal(p.$('activity-attention-h').textContent, 'Needs attention')
    const items = [...p.$('activity-attention-list').querySelectorAll('li')].map((li) => li.textContent)
    assert.deepEqual(items, ['~/code/yumbi (feat/x): uncommitted changes: please commit and push', '~/code/phoenix (fix/y): 3 commits not pushed (checking with GitHub…)'])
    assert.match(p.$('activity-problems').textContent, /it is damaged/)
    await p.click('activity-prev')
    assert.equal(p.$('activity-attention').hidden, true, 'not on another day')
  } finally { p.close() }
})

test("a closed row shows a spinner while its summary is being made, or a warning when it failed, and its repository's heading shows it too", async () => {
  const p = await pageWith({ summaries: () => [{ key: 'k1', state: 'failed', why: 'signed out' }, { key: 'k2', state: 'pending' }, { key: 'k3', state: 'none' }] })
  try {
    const state = (n) => n.querySelector('.activity-state')
    // phoenix (5.0) first: its one branch's summary failed; yumbi-web: one being made.
    const [phoenix, yumbi] = [...p.$('activity-list').querySelectorAll('.activity-group')]
    assert.equal(state(entry(p, 0)).textContent, '⚠')
    assert.match(state(entry(p, 0)).title, /Couldn't summarise: signed out/)
    assert.ok(state(entry(p, 1)).querySelector('.spinner'), 'a spinner while it is made')
    assert.equal(state(entry(p, 2)).childElementCount + state(entry(p, 2)).textContent.length, 0, 'nothing with no commits to summarise')
    assert.ok(state(yumbi.querySelector('.activity-group-head')).querySelector('.spinner'), "the branch's spinner shows on its repository")
    assert.equal(state(phoenix.querySelector('.activity-group-head')).textContent, '⚠', "and a failure, when none is under way")
  } finally { p.close() }
})

test('the summary says while it is being made, here or on another vibe, and the tab asks again soon', async () => {
  let state = [{ key: 'k1', state: 'pending' }, { key: 'k2', state: 'elsewhere', holder: 'yumbi' }, { key: 'k3', state: 'none' }]
  const p = await pageWith({ summaries: () => state })
  try {
    await open(p, 1)
    assert.match(panes(p, 1)[0].textContent, /Activity is being summarised on the yumbi vibe, and will appear here shortly\./)
    await open(p, 2)
    assert.match(panes(p, 2)[0].textContent, /No commits to summarise/)
    await open(p, 0)
    assert.equal(panes(p, 0)[0].textContent.includes('Summarising…'), true)
    assert.equal(panes(p, 0)[0].querySelector('button.copy-code'), null, 'nothing to copy yet')
    state = [ready('k1', 'done now'), ready('k2', 'and here'), { key: 'k3', state: 'none' }]
    const realNow = p.window.Date.now
    p.window.Date.now = () => realNow() + 3500
    await p.poll()
    assert.deepEqual(lines(panes(p, 0)[0]), ['done now'], 'not asked again within a few seconds')
    assert.equal(entry(p, 0).querySelector('.activity-toggle').getAttribute('aria-expanded'), 'true', 'the open row stays open across a poll')
  } finally { p.close() }
})

test('a poll that brings the same day leaves the rows as they are, the open one open', async () => {
  const p = await pageWith()
  try {
    await open(p, 1)
    const row = entry(p, 1)
    const realNow = p.window.Date.now
    p.window.Date.now = () => realNow() + 60000
    await p.poll()
    assert.equal(entry(p, 1), row, 'the rows were drawn again')
    assert.equal(row.querySelector('.activity-toggle').getAttribute('aria-expanded'), 'true')
  } finally { p.close() }
})

test('a summary that failed says why, and Retry asks for it again', async () => {
  const p = await pageWith({ summaries: () => [{ key: 'k1', state: 'failed', why: "the agent isn't signed in" }, { key: 'k2', state: 'unavailable', why: 'Commit summaries are only shown for Claude agents.' }, { key: 'k3', state: 'none' }] })
  try {
    await open(p, 1)
    assert.match(panes(p, 1)[0].textContent, /only shown for Claude agents/)
    await open(p, 0)
    assert.match(panes(p, 0)[0].textContent, /Couldn't summarise: the agent isn't signed in/)
    const before = p.calls.filter((c) => c.path === '/api/activity').length
    await p.click(p.button('Retry', panes(p, 0)[0]))
    assert.equal(p.kept.retried, 'k1')
    assert.equal(p.calls.filter((c) => c.path === '/api/activity').length, before + 1)
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

test('branches are grouped by repository, under a heading with its total and share of the day, the longest first', async () => {
  const day = { date: '2026-11-26', hours: 6, branches: [
    branch('phoenix', 'fix/y', 1.5),
    branch('vibe', 'feat/a', 1), branch('vibe', 'feat/b', 2), branch('vibe', 'feat/c', 0.5, [], { estimatedMinutes: 20 }),
    branch('yumbi-web', 'fix/z', 1),
  ] }
  const p = await pageWith({ days: { '': day } })
  try {
    assert.deepEqual(groups(p), ['vibe | 3.5 | 58% | ~estimated', 'phoenix | 1.5 | 25%', 'yumbi-web | 1.0 | 17%'])
    const inGroup = [...p.$('activity-list').querySelectorAll('.activity-group')].map((g) =>
      [...g.querySelectorAll('.activity-toggle')].map((t) => t.querySelector('.activity-branch-name').textContent))
    assert.deepEqual(inGroup, [['feat/b', 'feat/a', 'feat/c'], ['fix/y'], ['fix/z']], 'branches under their repository, the longest first')
    assert.equal(p.$('activity-list').querySelector('.activity-toggle .activity-repo'), null, "a branch's row doesn't repeat its repository")
  } finally { p.close() }
})

test('equal times are ordered by name, so the order holds still', async () => {
  const day = { date: '2026-11-26', hours: 2, branches: [branch('b-repo', 'x', 1), branch('a-repo', 'y', 0.5), branch('a-repo', 'w', 0.5)] }
  const p = await pageWith({ days: { '': day } })
  try {
    assert.deepEqual(groups(p).map((g) => g.split(' | ')[0]), ['a-repo', 'b-repo'])
    assert.deepEqual(rows(p).map((r) => r.split(' | ')[0]), ['w', 'y', 'x'])
  } finally { p.close() }
})

test('a repository whose branches are all hidden is hidden with them', async () => {
  const day = { date: '2026-11-26', hours: 1, branches: [branch('vibe', 'master', 0), branch('phoenix', 'fix/y', 1, ['work'])] }
  const p = await pageWith({ days: { '': day } })
  try {
    assert.deepEqual(groups(p).map((g) => g.split(' | ')[0]), ['phoenix'])
    await p.click('activity-hidden-toggle')
    assert.deepEqual(groups(p).map((g) => g.split(' | ')[0]), ['phoenix', 'vibe'])
  } finally { p.close() }
})

test("a branch's row copies its summary, with its ticket link first, once the summary is ready", async () => {
  let copied = null
  const day = { date: '2026-11-26', hours: 3, branches: [
    branch('vibe', 'feat/a', 2, ['x'], { ticket: 'https://trello.com/c/abc' }), branch('vibe', 'feat/b', 1, ['y']),
  ] }
  const p = await pageWith({ days: { '': day }, summaries: () => [ready('k1', 'did a', 'and more'), { key: 'k2', state: 'pending' }] })
  try {
    Object.defineProperty(p.window.navigator, 'clipboard', { value: { writeText: async (t) => { copied = t } }, configurable: true })
    const copy = (i) => entry(p, i).querySelector('.activity-copy')
    assert.equal(copy(1).disabled, true, 'nothing to copy while the summary is made')
    await p.click(copy(0))
    assert.equal(copied, 'https://trello.com/c/abc\n- did a\n- and more\n')
    const link = entry(p, 0).querySelector('a.activity-ticket')
    assert.equal(link.getAttribute('href'), 'https://trello.com/c/abc')
    assert.equal(link.getAttribute('target'), '_blank')
    assert.match(link.getAttribute('rel'), /noopener/)
    assert.equal(entry(p, 1).querySelector('a.activity-ticket'), null, 'no link where none is set')
    await open(p, 0)
    await p.click(panes(p, 0)[0].querySelector('button.copy-code'))
    assert.equal(copied, 'https://trello.com/c/abc\n- did a\n- and more\n', "the open row's summary copies the same")
  } finally { p.close() }
})

test("a branch's ticket link is set in its (i) balloon", async () => {
  const sent = []
  const day = { date: '2026-11-26', hours: 1, branches: [branch('vibe', 'feat/a', 1, ['x'])] }
  const p = await pageWith({ days: { '': day } })
  try {
    await p.click(entry(p, 0).querySelector('.activity-info'))
    const balloon = p.$('activity-row-balloon')
    const input = balloon.querySelector('input[type=url]')
    assert.ok(input, 'a field for the link')
    input.value = 'https://trello.com/c/abc'
    await p.click(p.button('Save', balloon))
    assert.deepEqual(p.sent('/api/activity/ticket'), [{ repo: 'vibe', branch: 'feat/a', url: 'https://trello.com/c/abc' }])
    assert.equal(balloon.hidden, true, 'it closes once kept')
    assert.equal(p.button('Clear', balloon), undefined, 'nothing to clear where no link was set')
  } finally { p.close() }
})

test("a branch's ticket link can be cleared from its (i) balloon", async () => {
  const day = { date: '2026-11-26', hours: 1, branches: [branch('vibe', 'feat/a', 1, ['x'], { ticket: 'https://trello.com/c/abc' })] }
  const p = await pageWith({ days: { '': day } })
  try {
    await p.click(entry(p, 0).querySelector('.activity-info'))
    const balloon = p.$('activity-row-balloon')
    assert.equal(balloon.querySelector('input[type=url]').value, 'https://trello.com/c/abc')
    await p.click(p.button('Clear', balloon))
    assert.deepEqual(p.sent('/api/activity/ticket'), [{ repo: 'vibe', branch: 'feat/a', url: '' }])
  } finally { p.close() }
})
