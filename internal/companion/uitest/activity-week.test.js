// The Activity tab's week: a pie of the week by repository beside a column per day, each repository in the fill it keeps; picking one
// shows its branches. In jsdom (see page.js); the layout is checked in Firefox (firefox.test.js).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const days = ['2026-11-23', '2026-11-24', '2026-11-25', '2026-11-26', '2026-11-27', '2026-11-28', '2026-11-29']
const repo = (name, d, branches) => ({ repo: name, days: d, hours: d.reduce((a, b) => a + b, 0), branches })
const thisWeek = {
  start: '2026-11-23', days, unread: [], today: '2026-11-26', earliest: '2026-11-13', problems: [], hours: 10,
  repos: [
    repo('vibe', [3, 2, 0, 1, 0, 0, 0], [
      { branch: 'feat/activity', days: [3, 1.5, 0, 1, 0, 0, 0], hours: 5.5 },
      { branch: 'master', days: [0, 0.5, 0, 0, 0, 0, 0], hours: 0.5 }]),
    repo('yumbi-web', [1, 1, 0, 0, 0, 0, 0], [{ branch: 'feat/dotnet-10', days: [1, 1, 0, 0, 0, 0, 0], hours: 2 }]),
    repo('NExpect', [0, 0, 1, 0.5, 0, 0, 0], [{ branch: 'master', days: [0, 0, 1, 0.5, 0, 0, 0], hours: 1.5 }]),
    repo('dotfiles', [0, 0, 0, 0.5, 0, 0, 0], [{ branch: 'master', days: [0, 0, 0, 0.5, 0, 0, 0], hours: 0.5 }]),
  ],
}
const lastWeek = Object.assign({}, thisWeek, {
  start: '2026-11-16', days: ['2026-11-16', '2026-11-17', '2026-11-18', '2026-11-19', '2026-11-20', '2026-11-21', '2026-11-22'],
  earliest: '2026-11-20', unread: ['2026-11-16', '2026-11-17', '2026-11-18', '2026-11-19'], repos: [repo('vibe', [0, 0, 0, 0, 2, 0, 0], [{ branch: 'x', days: [0, 0, 0, 0, 2, 0, 0], hours: 2 }])], hours: 2,
})
const day = { date: '2026-11-26', hours: 0, branches: [] }

async function weekPage({ mode = 'week', fills = { vibe: 0, 'yumbi-web': 1, NExpect: 2, dotfiles: 9 }, colourBlind = false } = {}) {
  const kept = { colourBlind }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, activity: true, attention: {} },
      '/api/log': { entries: [] },
      '/api/activity': { report: { day, today: '2026-11-26', earliest: '2026-11-13', problems: [] }, attention: [], problems: [], rounding: '30m', summaries: [] },
      '/api/activity/week': (call) => {
        const asked = new URLSearchParams(call.query).get('day') || ''
        const week = asked && asked < '2026-11-23' ? lastWeek : thisWeek
        return { week, fills, colourBlind: kept.colourBlind, rounding: '30m', attention: [] }
      },
      '/api/activity/colour-blind': (call) => { kept.colourBlind = call.body.on; return { on: call.body.on } },
      '/api/activity/seen': { ok: true },
    },
    storage: { 'vibe-companion-tab': 'activity', 'vibe-activity-mode': mode },
  })
  return p
}

const legend = (p) => [...p.$('activity-week-legend').querySelectorAll('button')].map((b) => [...b.children].map((c) => c.textContent.trim()).filter(Boolean).join(' | '))
const slices = (p) => [...p.$('activity-week-pie').querySelectorAll('path')]
const weekCalls = (p) => p.calls.filter((c) => c.path === '/api/activity/week')
const patternOf = (p, slot) => p.$('activity-fill-' + slot)

test('Week shows the pie, its list (hours and share, longest first) and a column per day, and is kept for the page', async () => {
  const p = await weekPage({ mode: 'day' })
  try {
    assert.equal(p.$('activity-week').hidden, true)
    await p.click(p.$('activity-mode').querySelector('[data-mode="week"]'))
    assert.equal(p.$('activity-week').hidden, false)
    assert.equal(p.$('activity-list').hidden, true)
    assert.equal(p.window.localStorage.getItem('vibe-activity-mode'), 'week')
    assert.deepEqual(legend(p), ['vibe | 6.0h | 60%', 'yumbi-web | 2.0h | 20%', 'NExpect | 1.5h | 15%', 'dotfiles | 0.5h | 5%'])
    assert.equal(slices(p).length, 4)
    assert.match(slices(p)[0].getAttribute('aria-label'), /^vibe, 6\.0h, 60% of the week$/)
    assert.equal(p.$('activity-day').textContent, 'Week of ' + new Date('2026-11-23T00:00:00').toLocaleDateString([], { weekday: 'short', day: 'numeric', month: 'short' }))
    assert.equal(p.$('activity-total').textContent, 'total 10.0h')
    const columns = p.$('activity-week-columns')
    assert.deepEqual([...columns.querySelectorAll('.activity-day-name')].map((t) => t.textContent), ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'])
    assert.equal(columns.querySelectorAll('path').length, 8, 'a segment for each repository on each day it had time')
  } finally { p.close() }
})

test("each repository is drawn in the fill it keeps: the tenth fill is the first colour again, with a pattern", async () => {
  const p = await weekPage()
  try {
    assert.equal(slices(p)[0].getAttribute('fill'), 'url(#activity-fill-0)')
    assert.equal(slices(p)[3].getAttribute('fill'), 'url(#activity-fill-9)')
    const first = patternOf(p, 0), tenth = patternOf(p, 9)
    assert.ok(first && tenth, 'the fills used have no patterns written')
    assert.equal(first.querySelector('rect').getAttribute('fill'), tenth.querySelector('rect').getAttribute('fill'), 'the tenth is the first colour again')
    assert.equal(first.querySelectorAll('rect, circle').length, 1, 'the first is solid')
    assert.ok(tenth.querySelectorAll('rect, circle').length > 1, 'the tenth has a pattern over its colour')
    assert.match(p.$('activity-week-legend').querySelector('[data-repo="dotfiles"]').getAttribute('aria-label'), /drawn stripes \//)
  } finally { p.close() }
})

test('picking a repository in the list (or a slice) shows its branches with the week and each day; picking it again hides them', async () => {
  const p = await weekPage()
  try {
    assert.match(p.$('activity-week-branches').textContent, /Pick a repository/)
    await p.click(p.$('activity-week-legend').querySelector('[data-repo="vibe"]'))
    const rows = [...p.$('activity-week-branches').querySelectorAll('.activity-entry')].map((e) => e.textContent.replace(/\s+/g, ' ').trim())
    assert.deepEqual(rows, ['feat/activity5.5h92%Mon 3.0h · Tue 1.5h · Thu 1.0h', 'master0.5h8%Tue 0.5h'])
    assert.equal(p.$('activity-week-pie').classList.contains('has-pick'), true)
    assert.equal(slices(p)[0].getAttribute('aria-pressed'), 'true')
    slices(p)[0].dispatchEvent(new p.window.MouseEvent('click', { bubbles: true }))
    await p.settle()
    assert.match(p.$('activity-week-branches').textContent, /Pick a repository/)
  } finally { p.close() }
})

test('the colour-blind choice is kept, and draws the fills in the three hues and their patterns', async () => {
  const p = await weekPage()
  try {
    const before = patternOf(p, 1).querySelector('rect').getAttribute('fill')
    p.$('activity-colour-blind').checked = true
    p.$('activity-colour-blind').dispatchEvent(new p.window.Event('change'))
    await p.settle()
    assert.deepEqual(p.sent('/api/activity/colour-blind'), [{ on: true }])
    const after = patternOf(p, 1).querySelector('rect').getAttribute('fill')
    assert.notEqual(after, before)
    assert.equal(after, '#f47230', "the second of the colour-blind hues, on the light page")
    assert.ok(patternOf(p, 9).querySelectorAll('rect, circle').length > 1, 'past the three hues, a pattern')
  } finally { p.close() }
})

test('the weeks step back to the first kept, saying which days are before it, and Return to this week comes back', async () => {
  const p = await weekPage()
  try {
    assert.equal(p.$('activity-next').disabled, true)
    assert.equal(p.$('activity-today').hidden, true)
    await p.click('activity-prev')
    assert.equal(new URLSearchParams(weekCalls(p).at(-1).query).get('day'), '2026-11-16')
    assert.equal(p.$('activity-week-unread').hidden, false)
    assert.match(p.$('activity-week-unread').textContent, /Mon, Tue, Wed, Thu are before the days kept/)
    assert.equal(p.$('activity-prev').disabled, true, 'the week before has none of the days kept')
    assert.equal(p.$('activity-today').textContent, 'Return to this week')
    await p.click('activity-today')
    assert.equal(new URLSearchParams(weekCalls(p).at(-1).query).get('day'), null)
    assert.equal(p.$('activity-week-unread').hidden, true)
  } finally { p.close() }
})

test('in Week, the date lists the weeks kept', async () => {
  const p = await weekPage()
  try {
    await p.click('activity-day')
    const items = [...p.$('activity-days-list').querySelectorAll('button')].map((b) => b.textContent)
    assert.equal(items[0], 'This week')
    assert.equal(items.length, 3, 'this week, last week, and the week the first day kept is in')
  } finally { p.close() }
})

test('the slices are reached by Tab, moved between with the arrow keys, and picked with Enter', async () => {
  const p = await weekPage()
  try {
    const key = (node, k) => node.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: k, bubbles: true }))
    assert.equal(slices(p)[0].getAttribute('tabindex'), '0')
    slices(p)[0].focus()
    key(slices(p)[0], 'ArrowRight')
    assert.equal(p.window.document.activeElement.dataset.repo, 'yumbi-web')
    key(p.window.document.activeElement, 'Enter')
    await p.settle()
    assert.match(p.$('activity-week-branches').textContent, /feat\/dotnet-10/)
    assert.equal(p.window.document.activeElement.dataset.repo, 'yumbi-web', 'focus stays on the slice picked')
  } finally { p.close() }
})
