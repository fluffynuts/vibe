// A harness for tests of the companion page itself: index.html and its
// scripts, in jsdom, against a fake vibe. The fake answers each call with
// what the test's routes say, and keeps the calls made; the page's polls run
// only when the test says, so nothing changes under a test while it looks.
'use strict'
const fs = require('node:fs')
const path = require('node:path')
const { JSDOM } = require('jsdom')

const UI = path.join(__dirname, '..', 'ui')
const SCRIPTS = ['markdown.js', 'manage.js', 'review.js', 'app.js']
const TOKEN = 'a'.repeat(64)

// openPage loads the page with routes: { '/api/state': body or (call) =>
// body, ... }, matched on the path without its query; anything else is
// answered {}. storage seeds localStorage. With settled false, it comes back
// as soon as the page's scripts have run, for a test of what the page shows
// while answers are still on their way.
async function openPage({ routes = {}, storage = {}, settled = true } = {}) {
  const html = fs.readFileSync(path.join(UI, 'index.html'), 'utf8').replace(/<script src="\/[^"]+"><\/script>/g, '')
  const dom = new JSDOM(html, { url: 'http://localhost:1/#' + TOKEN, runScripts: 'outside-only', pretendToBeVisual: true })
  const w = dom.window
  const calls = []
  let inFlight = 0
  w.fetch = async (url, options = {}) => {
    const u = new URL(String(url), 'http://localhost:1')
    // A JSON body is kept as what it says; anything else (a pasted file)
    // as it was given.
    const body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body || null
    const call = { path: u.pathname, query: u.search, method: options.method || 'GET', headers: options.headers || {}, body }
    calls.push(call)
    inFlight++
    try {
      const route = routes[u.pathname]
      // A route may answer later: a promise, for a call still on its way.
      const body = typeof route === 'function' ? await route(call) : route || {}
      // jsdom has no Response of its own: node's is the one fetch gives.
      // A route answers with an error status as { status, body }.
      const status = body && body.status && body.body ? body.status : 200
      const sent = status === 200 ? body : body.body
      return new Response(JSON.stringify(sent), { status, headers: { 'Content-Type': 'application/json' } })
    } finally {
      inFlight--
    }
  }
  // jsdom has no scrolling: a test that cares what's scrolled to says so.
  w.Element.prototype.scrollIntoView = function () {}
  w.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
  // jsdom has dialogs, but not showing them: closing one is what the page
  // waits on, as in a browser.
  w.HTMLDialogElement.prototype.showModal = function () { this.open = true }
  w.HTMLDialogElement.prototype.close = function (value) {
    if (value !== undefined) this.returnValue = value
    this.open = false
    this.dispatchEvent(new w.Event('close'))
  }
  const intervals = []
  w.setInterval = (fn, ms) => { intervals.push({ fn, ms }); return intervals.length }
  w.clearInterval = () => {}
  for (const [k, v] of Object.entries(storage)) w.localStorage.setItem(k, v)
  for (const f of SCRIPTS) w.eval(fs.readFileSync(path.join(UI, f), 'utf8'))

  const $ = (id) => w.document.getElementById(id)
  // settle waits until the page has had all its answers and done what they
  // lead to: no call left unanswered over a few turns of the event loop.
  const settle = async () => {
    let quiet = 0
    for (let turns = 0; turns < 1000; turns++) {
      await new Promise((resolve) => setImmediate(resolve))
      quiet = inFlight === 0 ? quiet + 1 : 0
      if (quiet >= 5) return
    }
    throw new Error('the page never settled: a call to vibe is still waiting')
  }
  if (settled) await settle()
  return {
    window: w,
    $,
    calls,
    settle,
    // poll runs the page's regular poll (its tick), as its timer would.
    async poll() { await this.every('tick') },
    // every runs the page's timer that calls the function named name, as
    // the timer would.
    async every(name) {
      const timer = intervals.find((i) => i.fn.name === name)
      if (!timer) throw new Error('the page set no timer going for ' + name)
      timer.fn()
      await settle()
    },
    async click(target) {
      const node = typeof target === 'string' ? $(target) : target
      if (!node) throw new Error('nothing to click: ' + target)
      node.click()
      await settle()
    },
    async type(node, text) {
      node.value = text
      node.dispatchEvent(new w.Event('input', { bubbles: true }))
      await settle()
    },
    // confirm answers the confirmation the page is showing.
    async confirm(yes) {
      const dialog = $('confirm')
      if (!dialog.open) throw new Error('nothing is asking to be confirmed')
      dialog.close(yes ? 'run' : 'cancel')
      await settle()
    },
    button(text, within) {
      return Array.from((within || w.document).querySelectorAll('button')).find((b) => b.textContent === text)
    },
    posted(p) { return calls.filter((c) => c.method === 'POST' && c.path === p).map((c) => c.body) },
    close() { w.close() },
  }
}

module.exports = { openPage }
