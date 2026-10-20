// Tests of the clipboard bridge's page (library/base/clipboard-bridge), in
// jsdom against a fake clipboard server, run by `node --test` (see
// review_ui_test.go). Needs `npm ci` in this folder.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { JSDOM } = require('jsdom')

const PAGE = path.join(__dirname, '..', '..', '..', 'library', 'base', 'clipboard-bridge', 'agent-files', '.local', 'share', 'vibe', 'clipboard-page.html')

// openClipboardPage loads the page with a fake server that says a paste is
// already waiting when its file name is in waiting.
function openClipboardPage(waiting) {
  const html = fs.readFileSync(PAGE, 'utf8')
  const script = html.match(/<script>([\s\S]*)<\/script>/)[1]
  const dom = new JSDOM(html.replace(/<script>[\s\S]*<\/script>/, ''), { url: 'http://localhost:5390/', runScripts: 'outside-only', pretendToBeVisual: true })
  const w = dom.window
  let inFlight = 0
  let poll = () => {}
  const server = { down: false }
  w.fetch = async (url, options = {}) => {
    inFlight++
    try {
      if (server.down) throw new TypeError('Failed to fetch')
      let body = { items: [] }
      if (options.method === 'POST' && String(url) === '/api/items') {
        const name = decodeURIComponent(options.headers['X-Filename'] || '')
        body = waiting.includes(name) ? { duplicate: true, id: 1 } : { id: 2 }
      }
      return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
    } finally {
      inFlight--
    }
  }
  w.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
  w.setInterval = (fn) => { poll = fn; return 0 }
  w.eval(script)
  const settle = async () => {
    let quiet = 0
    for (let turns = 0; turns < 1000; turns++) {
      await new Promise((resolve) => setImmediate(resolve))
      quiet = inFlight === 0 ? quiet + 1 : 0
      if (quiet >= 5) return
    }
    throw new Error('the page never settled')
  }
  return {
    window: w,
    status: () => w.document.getElementById('status').textContent,
    link: () => { const el = w.document.getElementById('link'); return el.hidden ? '' : el.textContent },
    server,
    async poll() { poll(); await settle() },
    settle,
    async paste(...names) {
      const files = names.map((n) => new w.File(['content of ' + n], n, { type: 'image/png' }))
      const ev = new w.Event('paste', { bubbles: true, cancelable: true })
      ev.clipboardData = { files, getData: () => '' }
      w.document.dispatchEvent(ev)
      await settle()
    },
    close() { w.close() },
  }
}

test('a paste is added, to go with the next message', async () => {
  const p = openClipboardPage([])
  try {
    await p.paste('a.png')
    assert.equal(p.status(), "Added — it'll reach the agent with your next message.")
  } finally { p.close() }
})

test('a paste already waiting to go says so, and isn’t added again', async () => {
  const p = openClipboardPage(['a.png'])
  try {
    await p.paste('a.png')
    assert.equal(p.status(), "Already pasted: it's waiting to go with your next message.")
  } finally { p.close() }
})

test('of several pasted at once, those already waiting are counted apart', async () => {
  const p = openClipboardPage(['a.png'])
  try {
    await p.paste('a.png', 'b.png', 'c.png')
    assert.equal(p.status(), 'Added 2 items. (1 already pasted.)')
  } finally { p.close() }
  const all = openClipboardPage(['a.png', 'b.png'])
  try {
    await all.paste('a.png', 'b.png')
    assert.equal(all.status(), 'Already pasted, all 2.')
  } finally { all.close() }
})

const NOT_REACHED = "Can't reach the clipboard server — is the sandbox running?"

test('one failed look at the server says nothing: it may be waking up', async () => {
  const p = openClipboardPage([])
  try {
    await p.settle()
    p.server.down = true
    await p.poll()
    assert.equal(p.link(), '')
  } finally { p.close() }
})

test('three failed looks in a row say the server can’t be reached, apart from the status line', async () => {
  const p = openClipboardPage([])
  try {
    await p.settle()
    p.server.down = true
    await p.poll()
    await p.poll()
    assert.equal(p.link(), '')
    await p.poll()
    assert.equal(p.link(), NOT_REACHED)
    assert.equal(p.status(), '', 'the status line, which would shift the page, is left alone')
  } finally { p.close() }
})

test('a server that comes back clears the message', async () => {
  const p = openClipboardPage([])
  try {
    await p.settle()
    p.server.down = true
    for (let i = 0; i < 3; i++) await p.poll()
    assert.equal(p.link(), NOT_REACHED)
    p.server.down = false
    await p.poll()
    assert.equal(p.link(), '')
  } finally { p.close() }
})

test('failures that aren’t in a row don’t add up', async () => {
  const p = openClipboardPage([])
  try {
    await p.settle()
    for (let i = 0; i < 3; i++) {
      p.server.down = true
      await p.poll()
      await p.poll()
      p.server.down = false
      await p.poll()
    }
    assert.equal(p.link(), '')
  } finally { p.close() }
})
