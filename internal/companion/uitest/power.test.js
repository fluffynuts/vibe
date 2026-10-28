// Tests of the power button at the top right of the page, which says whether vibe is reached and opens
// the menu of what can be done about it. See docs/plan-quality-of-life.md, "A power menu on the page".
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { openPage } = require('./page.js')

const reached = { '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} }, '/api/log': { entries: [] } }
const gone = { '/api/state': () => { throw new Error('refused') }, '/api/log': () => { throw new Error('refused') } }

const stateOf = (p) => ['up', 'down', 'connecting'].filter((s) => p.$('link').classList.contains(s))

test('the button says connecting until vibe has answered', async () => {
  const p = await openPage({ routes: reached, settled: false })
  try {
    assert.deepEqual(stateOf(p), ['connecting'])
    assert.match(p.$('link').title, /connecting/)
    await p.settle()
  } finally { p.close() }
})

test('with vibe reached, the button is up, and says so to a tooltip, a screen reader and the menu', async () => {
  const p = await openPage({ routes: reached })
  try {
    assert.deepEqual(stateOf(p), ['up'])
    assert.match(p.$('link').title, /connected/)
    assert.match(p.$('link').getAttribute('aria-label'), /connected/)
    assert.equal(p.$('link-text').textContent, 'connected')
    assert.equal(p.$('link-text').getAttribute('role'), 'status')
    assert.equal(p.$('power-status').textContent, 'Status: connected')
    assert.equal(p.$('power-status').getAttribute('role'), 'presentation')
  } finally { p.close() }
})

test('with vibe gone, the button is down, and says what to do wherever it said "connected"', async () => {
  const p = await openPage({ routes: gone })
  try {
    assert.deepEqual(stateOf(p), ['down'])
    for (const said of [p.$('link').title, p.$('link').getAttribute('aria-label'), p.$('link-text').textContent, p.$('power-status').textContent]) {
      assert.match(said, /vibe is not running/, said)
    }
  } finally { p.close() }
})

test('the button opens the menu and does nothing else; Escape, an outside click and a second click close it', async () => {
  const p = await openPage({ routes: reached })
  try {
    const before = p.calls.length
    assert.equal(p.$('power-menu').hidden, true)
    await p.click('link')
    assert.equal(p.$('power-menu').hidden, false)
    assert.equal(p.$('link').getAttribute('aria-expanded'), 'true')
    assert.equal(p.calls.slice(before).filter((c) => c.method !== 'GET').length, 0, 'opening the menu acted')
    p.$('power-menu').dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.equal(p.$('power-menu').hidden, true)
    assert.equal(p.$('link').getAttribute('aria-expanded'), 'false')
    await p.click('link')
    p.window.document.body.dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    assert.equal(p.$('power-menu').hidden, true)
    await p.click('link')
    await p.click('link')
    assert.equal(p.$('power-menu').hidden, true)
  } finally { p.close() }
})

test('the menu still opens with vibe gone, because the issue links need no vibe', async () => {
  const p = await openPage({ routes: gone })
  try {
    await p.click('link')
    assert.equal(p.$('power-menu').hidden, false)
    assert.equal(p.$('power-menu').querySelectorAll('a').length, 3)
  } finally { p.close() }
})

const templates = fs.readdirSync(path.join(__dirname, '..', '..', '..', '.github', 'ISSUE_TEMPLATE'))

test('the menu links to the three issue forms of the repository, each of which exists, and passes nothing else on', async () => {
  const p = await openPage({ routes: reached })
  try {
    const links = [...p.$('power-menu').querySelectorAll('a')]
    assert.deepEqual(links.map((a) => a.textContent), ['Report a bug', 'Request a feature', 'Something else'])
    for (const a of links) {
      const url = new URL(a.href)
      assert.equal(url.origin + url.pathname, 'https://github.com/fluffynuts/vibe/issues/new', a.href)
      assert.deepEqual([...url.searchParams.keys()], ['template'], 'what the link carries: ' + a.href)
      assert.ok(templates.includes(url.searchParams.get('template')), url.searchParams.get('template') + ' is not in .github/ISSUE_TEMPLATE')
      assert.equal(a.target, '_blank')
      assert.equal(a.rel, 'noopener noreferrer')
    }
  } finally { p.close() }
})

// powerRoutes is a vibe that can be stopped and restarted: check is what /api/power says, log what has been said so far.
function powerRoutes({ check, log = { text: '', next: 0, running: false }, boot = 'b1' } = {}) {
  const ok = { ok: true }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, conversation: true, attention: {}, power: true, boot: routes.boot, folder: '~/work/box' }),
    '/api/log': { entries: [] },
    '/api/power': (call) => call.method === 'POST' ? { status: 202, body: { ok: true } } : (routes.check || { sandbox: 'box', folder: '~/work/box', actions: { stop: ok, restart: ok } }),
    '/api/power/log': () => Object.assign({ boot: routes.boot }, routes.log),
    boot, check, log,
  }
  return routes
}

const items = (p) => ({ stop: p.$('power-stop'), restart: p.$('power-restart') })

test('the menu offers to stop and restart this sandbox when vibe can', async () => {
  const p = await openPage({ routes: powerRoutes() })
  try {
    await p.click('link')
    await p.settle()
    for (const item of Object.values(items(p))) assert.equal(item.disabled, false)
    assert.match(items(p).stop.textContent, /Stop/)
    assert.match(items(p).restart.textContent, /Restart/)
  } finally { p.close() }
})

test('an action vibe says it cant do is disabled with the reason shown beside it', async () => {
  const check = { sandbox: 'box', folder: '~/work/box', actions: { stop: { ok: true }, restart: { ok: false, why: 'a vibe here has a terminal session' } } }
  const p = await openPage({ routes: powerRoutes({ check }) })
  try {
    await p.click('link')
    await p.settle()
    assert.equal(items(p).restart.disabled, true)
    assert.match(items(p).restart.textContent, /a vibe here has a terminal session/)
    assert.equal(items(p).stop.disabled, false)
  } finally { p.close() }
})

test('with vibe gone, stop and restart are disabled and say vibe is not running', async () => {
  const routes = powerRoutes()
  const p = await openPage({ routes })
  try {
    for (const path of ['/api/state', '/api/log', '/api/power']) routes[path] = () => { throw new Error('refused') }
    await p.poll()
    await p.poll()
    await p.click('link')
    await p.settle()
    for (const item of Object.values(items(p))) {
      assert.equal(item.disabled, true)
      assert.match(item.textContent, /vibe is not running/)
    }
  } finally { p.close() }
})

test('a page whose vibe has no power menu has no stop or restart in it', async () => {
  const routes = powerRoutes()
  const state = routes['/api/state']
  routes['/api/state'] = () => Object.assign(state(), { power: false })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    assert.equal(p.$('power-actions').hidden, true)
  } finally { p.close() }
})

test('Stop asks first, naming the sandbox and its folder and what it would interrupt, and nothing is done until it is confirmed', async () => {
  const warn = 'the agent of the headless vibe (PID 7) is part-way through a turn: ending it cuts the turn off'
  const check = { sandbox: 'box', folder: '~/work/box', actions: { stop: { ok: true, warns: [warn] }, restart: { ok: true } } }
  const p = await openPage({ routes: powerRoutes({ check }) })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).stop)
    assert.equal(p.$('confirm').open, true)
    const asked = p.$('confirm-title').textContent + ' ' + p.$('confirm-text').textContent + ' ' + p.$('confirm-list').textContent
    for (const want of ["'box'", '~/work/box', 'part-way through a turn']) assert.ok(asked.includes(want), `the question leaves out ${want}: ${asked}`)
    assert.equal(p.posted('/api/power').length, 0)
    await p.confirm(false)
    assert.equal(p.posted('/api/power').length, 0, 'cancelled, and it was done')
  } finally { p.close() }
})

test('confirming Stop sends it, confirmed, for this sandbox, and shows what vibe says as it goes', async () => {
  const routes = powerRoutes({ log: { text: '=== vibe --stop ===\nstopping sandbox box\n', next: 40, running: true } })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).stop)
    await p.confirm(true)
    assert.deepEqual(p.posted('/api/power'), [{ sandbox: 'box', action: 'stop', confirmed: true, warns: [] }])
    assert.equal(p.$('power-progress').open, true)
    await p.every('pollPower')
    assert.match(p.$('power-log').textContent, /stopping sandbox box/)
    routes.log = { text: 'sandbox stopped\n', next: 56, running: true }
    await p.every('pollPower')
    assert.match(p.$('power-log').textContent, /stopping sandbox box\n[\s\S]*sandbox stopped/)
    const asked = p.calls.filter((c) => c.path === '/api/power/log').map((c) => c.query)
    assert.ok(asked[asked.length - 1].includes('from=40'), 'asked again from where it had got to: ' + asked)
  } finally { p.close() }
})

test('a stop whose log is answered by a vibe that has finished, and is still there, says it did not take effect', async () => {
  const routes = powerRoutes({ log: { text: 'couldn\'t end vibe (PID 7)\n', next: 27, running: false } })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).stop)
    await p.confirm(true)
    await p.every('pollPower')
    assert.match(p.$('power-end').textContent, /stop didn't take effect: vibe is still running/)
    assert.match(p.$('power-log').textContent, /couldn't end vibe/)
  } finally { p.close() }
})

test('a stop ends with vibe gone, and says so', async () => {
  const routes = powerRoutes({ log: { text: 'stopping\n', next: 9, running: true } })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).stop)
    await p.confirm(true)
    routes['/api/power/log'] = () => { throw new Error('refused') }
    routes['/api/state'] = () => { throw new Error('refused') }
    await p.every('pollPower')
    await p.every('pollPower')
    assert.match(p.$('power-end').textContent, /vibe has ended/)
  } finally { p.close() }
})

test('a restart ends when the log is answered under a new boot, which needs no poll of the state first, and says the page is back', async () => {
  const routes = powerRoutes({ log: { text: 'restarting\n', next: 11, running: true } })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).restart)
    await p.confirm(true)
    await p.every('pollPower')
    assert.equal(p.$('power-end').textContent, '')
    // The log answers under the new boot before the page's regular poll has seen it.
    routes.boot = 'b2'
    routes.log = { text: 'it is up\n', next: 20, running: false }
    await p.every('pollPower')
    assert.match(p.$('power-end').textContent, /Restarted/)
  } finally { p.close() }
})

test('an action that ended and changed nothing says so, and to read what it said', async () => {
  const routes = powerRoutes({ log: { text: 'not restarting: something is wrong\n', next: 34, running: false } })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).restart)
    await p.confirm(true)
    await p.every('pollPower')
    await p.every('pollPower')
    assert.match(p.$('power-end').textContent, /didn't take effect/)
    assert.match(p.$('power-log').textContent, /not restarting: something is wrong/)
  } finally { p.close() }
})

test('a refused action says why, and starts no progress', async () => {
  const routes = powerRoutes()
  const post = routes['/api/power']
  routes['/api/power'] = (call) => call.method === 'POST' ? { status: 409, body: { error: 'a rebuild is under way' } } : post(call)
  const p = await openPage({ routes })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).stop)
    await p.confirm(true)
    assert.equal(p.$('power-progress').open, false)
    assert.match(p.$('toast').textContent, /a rebuild is under way/)
  } finally { p.close() }
})

// profilesPage is the Profiles tab with sandboxes whose rows offer actions, and a vibe that can start and restart them.
function profilesPage(actionsOf, routes = powerRoutes()) {
  const row = (name, actions, attached) => ({ id: 'sandbox:' + name, name, folder: '~/work/' + name, flags: [], actions, attached })
  const state = routes['/api/state']
  routes['/api/state'] = () => Object.assign(state(), { manage: true })
  routes['/api/manage'] = {
    inventory: { profiles: [{ id: 'profile:p', name: 'p', features: [], flags: [], actions: [], sandboxes: [row('other', actionsOf, !!routes.attached)] }], unknown: [] },
    job: null,
  }
  routes['/api/power'] = (call) => {
    if (call.method === 'POST') return { status: 202, body: { ok: true } }
    return { sandbox: 'other', folder: '~/work/other', actions: { start: { ok: true }, restart: { ok: true, warns: routes.warns || [] } } }
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': 'manage' } })
}

const rowButton = (p, text) => [...p.window.document.querySelectorAll('#manage-list button')].find((b) => b.textContent === text)

test('a sandbox row offers to start a vibe for it, asking first and naming the sandbox and its folder', async () => {
  const p = await profilesPage(['startvibe'])
  try {
    await p.click(rowButton(p, 'Start vibe…'))
    await p.settle()
    assert.equal(p.$('confirm').open, true)
    const asked = p.$('confirm-title').textContent + ' ' + p.$('confirm-text').textContent
    assert.ok(asked.includes("'other'") && asked.includes('~/work/other'), asked)
    assert.equal(p.posted('/api/power').length, 0)
    await p.confirm(true)
    assert.deepEqual(p.posted('/api/power'), [{ sandbox: 'other', action: 'start', confirmed: true, warns: [] }])
    assert.equal(p.$('power-progress').open, true)
  } finally { p.close() }
})

test("restarting another sandbox's vibe shows what it would interrupt, and posts for that sandbox", async () => {
  const routes = powerRoutes()
  routes.attached = true
  routes.warns = ['the agent of the headless vibe (PID 7) is part-way through a turn: ending it cuts the turn off']
  const p = await profilesPage(['stop', 'restartvibe'], routes)
  try {
    await p.click(rowButton(p, 'Restart vibe…'))
    await p.settle()
    assert.match(p.$('confirm-list').textContent, /part-way through a turn/)
    await p.confirm(true)
    assert.deepEqual(p.posted('/api/power'), [{ sandbox: 'other', action: 'restart', confirmed: true, warns: routes.warns }])
  } finally { p.close() }
})

test("another sandbox's action ends when what runs it has, and says to read what it said: no boot to wait for", async () => {
  const routes = powerRoutes({ log: { text: 'starting vibe\n', next: 14, running: true } })
  const p = await profilesPage(['startvibe'], routes)
  try {
    await p.click(rowButton(p, 'Start vibe…'))
    await p.settle()
    await p.confirm(true)
    await p.every('pollPower')
    assert.equal(p.$('power-end').textContent, '')
    routes.log = { text: 'the vibe is up\n', next: 28, running: false }
    await p.every('pollPower')
    assert.match(p.$('power-end').textContent, /has ended/)
    assert.doesNotMatch(p.$('power-end').textContent, /take effect/)
    assert.ok(p.calls.some((c) => c.path === '/api/power/log' && c.query.includes('sandbox=other')))
  } finally { p.close() }
})

test('a Profiles page with no sandbox of its own has no stop or restart for itself in the menu', async () => {
  const routes = powerRoutes()
  const state = routes['/api/state']
  routes['/api/state'] = () => Object.assign(state(), { sandbox: '', sandboxTabs: false, manage: true })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    assert.equal(p.$('power-actions').hidden, true)
  } finally { p.close() }
})

test('a poll of the log is not started while the last is still out, so nothing is appended twice', async () => {
  const routes = powerRoutes({ log: { text: 'one line\n', next: 9, running: true } })
  const p = await openPage({ routes })
  try {
    await p.click('link')
    await p.settle()
    await p.click(items(p).stop)
    await p.confirm(true)
    let release
    const held = new Promise((resolve) => { release = resolve })
    const answer = routes['/api/power/log']
    routes['/api/power/log'] = () => held.then(() => answer())
    const before = p.calls.filter((c) => c.path === '/api/power/log').length
    await p.kick('pollPower')
    await p.kick('pollPower')
    await p.kick('pollPower')
    assert.equal(p.calls.filter((c) => c.path === '/api/power/log').length - before, 1, 'polls while one was out')
    release()
    await p.settle()
    assert.equal(p.$('power-log').textContent, 'one line\n')
  } finally { p.close() }
})

test('every entry of the power menu has an icon: a square for Stop, circular arrows for Restart, the GitHub mark for each link', async () => {
  const p = await openPage({ routes: powerRoutes() })
  try {
    const icon = (node) => node.querySelector('svg.power-icon')
    for (const [name, node] of [['Stop', items(p).stop], ['Restart', items(p).restart], ...[...p.$('power-menu').querySelectorAll('a')].map((a) => [a.textContent, a])]) {
      assert.ok(icon(node), name + ' has no icon')
      assert.equal(icon(node).getAttribute('aria-hidden'), 'true', name + "'s icon is read out")
    }
    const shape = (node) => icon(node).innerHTML
    assert.notEqual(shape(items(p).stop), shape(items(p).restart))
    const links = [...p.$('power-menu').querySelectorAll('a')].map(shape)
    assert.equal(new Set(links).size, 1, 'the GitHub links have different icons')
    assert.notEqual(links[0], shape(items(p).stop))
  } finally { p.close() }
})
