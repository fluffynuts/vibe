// Tests of the CPU and memory bars in the tab bar, in jsdom (see page.js), run
// with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const GiB = 1024 * 1024 * 1024

const ok = (extra) => Object.assign({
  state: 'ok', cpu: 25, cores: 12, coresSetting: 12, memUsed: 2 * GiB, memTotal: 8 * GiB, memSetting: '8g', ageMs: 1000,
}, extra)

// usagePage is any tab of a sandbox that can be read: answer is what
// /api/usage says now, which a test can change.
function usagePage(answer, { usage = true, tab = 'log' } = {}) {
  const at = { answer }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, usage, attention: {} }),
    '/api/usage': () => at.answer,
    '/api/log': { entries: [] },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': tab } }).then((p) => Object.assign(p, { at }))
}

const fill = (p, which) => p.$('usage-' + which + '-fill').style.width
const label = (p, which) => p.$('usage-' + which + '-label').textContent

test('a page that can\'t read the sandbox shows no bars', async () => {
  const p = await usagePage(ok(), { usage: false })
  try {
    assert.equal(p.$('usage').hidden, true)
    assert.equal(p.calls.filter((c) => c.path === '/api/usage').length, 0, 'nothing was asked for')
  } finally { p.close() }
})

test('the bars are in the tab bar, past the tabs, and not among the tabs themselves', async () => {
  const p = await usagePage(ok())
  try {
    assert.equal(p.$('usage').hidden, false)
    assert.ok(p.$('tabbar').contains(p.$('usage')), 'in the tab bar')
    assert.ok(!p.$('tabs').contains(p.$('usage')), 'a widget among the tabs would be reached by arrowing along them')
    assert.equal(p.$('tabs').getAttribute('role'), 'tablist')
  } finally { p.close() }
})

test('each bar says what the sandbox has, and how full it is', async () => {
  const p = await usagePage(ok({ cpu: 87.5, memUsed: 6 * GiB }))
  try {
    assert.equal(label(p, 'cpu'), 'CPU (12 cores)')
    assert.equal(label(p, 'mem'), 'MEM (8g)')
    assert.equal(fill(p, 'cpu'), '87.5%')
    assert.equal(fill(p, 'mem'), '75%')
  } finally { p.close() }
})

test('with no recorded settings, the labels say what the sandbox reports', async () => {
  const p = await usagePage(ok({ coresSetting: 0, cores: 6, memSetting: '', memTotal: 7.8 * GiB, memUsed: 1 * GiB }))
  try {
    assert.equal(label(p, 'cpu'), 'CPU (6 cores)')
    assert.equal(label(p, 'mem'), 'MEM (7.8 GiB)')
  } finally { p.close() }
})

test('one core is "1 core"', async () => {
  const p = await usagePage(ok({ coresSetting: 1, cores: 1 }))
  try { assert.equal(label(p, 'cpu'), 'CPU (1 core)') } finally { p.close() }
})

test('a bar warms at 70% and runs hot at 90%, and says the figure too, not by colour alone', async () => {
  for (const [cpu, want] of [[10, 'ok'], [69.9, 'ok'], [70, 'warm'], [89.9, 'warm'], [90, 'hot'], [100, 'hot']]) {
    const p = await usagePage(ok({ cpu }))
    try {
      const meter = p.$('usage-cpu')
      assert.ok(meter.classList.contains(want), cpu + '% should be ' + want + ': ' + meter.className)
      assert.equal(meter.getAttribute('aria-valuenow'), String(Math.round(cpu)))
      assert.match(meter.getAttribute('aria-valuetext'), new RegExp(Math.round(cpu) + '%'))
    } finally { p.close() }
  }
})

test('memory runs the same thresholds on how much of it is used', async () => {
  const p = await usagePage(ok({ memUsed: 7.5 * GiB }))
  try {
    assert.ok(p.$('usage-mem').classList.contains('hot'), p.$('usage-mem').className)
    assert.equal(fill(p, 'mem'), '93.75%')
  } finally { p.close() }
})

test('the tooltip gives the whole picture and how old it is', async () => {
  const p = await usagePage(ok({ cpu: 87.5, memUsed: 5.2 * GiB, ageMs: 2000 }))
  try {
    const tip = p.$('usage').title
    assert.match(tip, /CPU 88% of 12 cores/)
    assert.match(tip, /5\.2 of 8\.0 GiB \(65%\)/)
    assert.match(tip, /2s ago/)
  } finally { p.close() }
})

test('no CPU figure yet leaves that bar empty, saying so, and the memory one shown', async () => {
  const p = await usagePage(ok({ cpu: null }))
  try {
    assert.equal(fill(p, 'cpu'), '0%')
    assert.match(p.$('usage').title, /CPU: measuring/)
    assert.equal(fill(p, 'mem'), '25%')
  } finally { p.close() }
})

test('a reading too old to trust greys the bars and says how old it is', async () => {
  const p = await usagePage(ok({ ageMs: 20000 }))
  try {
    assert.ok(p.$('usage').classList.contains('stale'))
    assert.match(p.$('usage').title, /20s ago/)
    assert.match(p.$('usage').title, /not answering|can't|couldn't/i)
  } finally { p.close() }
})

test('a failed read keeps the last figures, greyed, with why', async () => {
  const p = await usagePage(ok({ ageMs: 4000, error: 'exec: timed out' }))
  try {
    assert.ok(p.$('usage').classList.contains('stale'))
    assert.equal(fill(p, 'cpu'), '25%')
    assert.match(p.$('usage').title, /timed out/)
  } finally { p.close() }
})

test('a stopped sandbox shows empty bars that say so', async () => {
  const p = await usagePage({ state: 'stopped' })
  try {
    assert.ok(p.$('usage').classList.contains('off'))
    assert.equal(fill(p, 'cpu'), '0%')
    assert.equal(fill(p, 'mem'), '0%')
    assert.match(p.$('usage').title, /stopped/)
  } finally { p.close() }
})

test('before the first reading, and when it can\'t be read at all, the bars say that', async () => {
  let p = await usagePage({ state: 'unknown' })
  try {
    assert.ok(p.$('usage').classList.contains('off'))
    assert.match(p.$('usage').title, /reading/i)
  } finally { p.close() }
  p = await usagePage({ state: 'unreachable', error: 'sbx isn\'t answering' })
  try {
    assert.ok(p.$('usage').classList.contains('stale'))
    assert.match(p.$('usage').title, /sbx isn't answering/)
  } finally { p.close() }
})

test('the bars follow the sandbox as the page polls, whichever tab it\'s on', async () => {
  const p = await usagePage(ok({ cpu: 20 }), { tab: 'rules' })
  try {
    assert.equal(fill(p, 'cpu'), '20%')
    p.at.answer = ok({ cpu: 95 })
    await p.poll()
    assert.equal(fill(p, 'cpu'), '95%')
    assert.ok(p.$('usage-cpu').classList.contains('hot'))
  } finally { p.close() }
})
