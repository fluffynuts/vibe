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
  const server = { down: false, refuse: null, uploads: [] }
  w.fetch = async (url, options = {}) => {
    inFlight++
    try {
      if (server.down) throw new TypeError('Failed to fetch')
      let body = { items: [] }
      if (options.method === 'POST' && String(url) === '/api/items') {
        const name = decodeURIComponent(options.headers['X-Filename'] || '')
        if (server.refuse) return new Response(JSON.stringify({ error: server.refuse }), { status: 413, headers: { 'Content-Type': 'application/json' } })
        server.uploads.push(name)
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
    held: () => Array.from(w.document.querySelectorAll('#held .item')).map((n) => n.textContent),
    clickIn(selector) { w.document.querySelector(selector).click() },
    async poll() { poll(); await settle() },
    settle,
    // pasteSized pastes one file per [name, size], the size as the file says it is (none is that big).
    async pasteSized(...files) {
      const made = files.map(([name, size]) => {
        const f = new w.File(['x'], name, { type: 'image/png' })
        Object.defineProperty(f, 'size', { value: size })
        return f
      })
      const ev = new w.Event('paste', { bubbles: true, cancelable: true })
      ev.clipboardData = { files: made, getData: () => '' }
      w.document.dispatchEvent(ev)
      await settle()
    },
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

async function serverDownFor(p, polls = 3) {
  await p.settle()
  p.server.down = true
  for (let i = 0; i < polls; i++) await p.poll()
}

test('a paste while the server can’t be reached is held, not refused', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.paste('a.png')
    assert.equal(p.held().length, 1)
    assert.match(p.held()[0], /a\.png/)
    assert.match(p.held()[0], /waiting for the server/)
    assert.doesNotMatch(p.status(), /Couldn.t add/)
  } finally { p.close() }
})

test('what is held is added once the server answers again', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.paste('a.png', 'b.png')
    assert.equal(p.held().length, 2)
    await p.poll()
    assert.deepEqual(p.server.uploads, [], 'still down: nothing is sent')
    p.server.down = false
    await p.poll()
    assert.deepEqual(p.server.uploads.sort(), ['a.png', 'b.png'])
    assert.deepEqual(p.held(), [])
    assert.match(p.status(), /Added 2 items/)
  } finally { p.close() }
})

test('a paste the server drops before it was known to be away is held too', async () => {
  const p = openClipboardPage([])
  try {
    await p.settle()
    p.server.down = true
    await p.paste('a.png')
    assert.equal(p.held().length, 1)
    p.server.down = false
    await p.poll()
    assert.deepEqual(p.server.uploads, ['a.png'])
    assert.deepEqual(p.held(), [])
  } finally { p.close() }
})

test('what the server refuses is said, not held: it would be refused again', async () => {
  const p = openClipboardPage([])
  try {
    await p.settle()
    p.server.refuse = 'too big'
    await p.paste('a.png')
    assert.deepEqual(p.held(), [])
    assert.match(p.status(), /Couldn.t add that: too big/)
  } finally { p.close() }
})

test('a held paste can be dropped', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.paste('a.png', 'b.png')
    p.clickIn('#held .item button')
    assert.equal(p.held().length, 1)
    p.server.down = false
    await p.poll()
    assert.equal(p.server.uploads.length, 1, 'only the one kept is sent')
  } finally { p.close() }
})

test('leaving the page while pastes are held asks first, and not otherwise', async () => {
  const p = openClipboardPage([])
  try {
    const leave = () => {
      const ev = new p.window.Event('beforeunload', { cancelable: true })
      p.window.dispatchEvent(ev)
      return ev.defaultPrevented
    }
    assert.equal(leave(), false)
    await serverDownFor(p)
    await p.paste('a.png')
    assert.equal(leave(), true)
    p.server.down = false
    await p.poll()
    assert.equal(leave(), false)
  } finally { p.close() }
})

// A server that has been away through three pastes is a problem to look into, not one to keep adding to: only a few
// are held, and no more than a total size, and what isn't kept is said, so nobody thinks it is.
const MB = 1024 * 1024

test('only three pastes are held while the server is away, and the rest are said not to be kept', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.paste('a.png', 'b.png', 'c.png')
    assert.equal(p.held().length, 3)
    await p.paste('d.png')
    assert.equal(p.held().length, 3)
    assert.match(p.status(), /not kept/i)
    assert.match(p.status(), /d\.png|1 item/)
    p.server.down = false
    await p.poll()
    assert.deepEqual(p.server.uploads.sort(), ['a.png', 'b.png', 'c.png'], 'what was kept goes, and only that')
  } finally { p.close() }
})

test('of five pasted at once while it is away, three are kept and two are said not to be', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.paste('a.png', 'b.png', 'c.png', 'd.png', 'e.png')
    assert.equal(p.held().length, 3)
    assert.match(p.status(), /2 items/)
  } finally { p.close() }
})

test('no more than 150 MB is held, in all', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.pasteSized(['big.png', 100 * MB])
    assert.equal(p.held().length, 1)
    await p.pasteSized(['more.png', 60 * MB])
    assert.equal(p.held().length, 1, '160 MB is over')
    assert.match(p.status(), /not kept/i)
    await p.pasteSized(['fits.png', 50 * MB])
    assert.equal(p.held().length, 2, '150 MB exactly is allowed')
  } finally { p.close() }
})

test('a single paste over the limit is not kept, and is said not to be', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.pasteSized(['huge.png', 151 * MB])
    assert.equal(p.held().length, 0)
    assert.match(p.status(), /not kept/i)
  } finally { p.close() }
})

test('dropping a held paste makes room for another', async () => {
  const p = openClipboardPage([])
  try {
    await serverDownFor(p)
    await p.paste('a.png', 'b.png', 'c.png')
    p.clickIn('#held .item button')
    await p.paste('d.png')
    assert.equal(p.held().length, 3)
    p.server.down = false
    await p.poll()
    assert.deepEqual(p.server.uploads.sort(), ['b.png', 'c.png', 'd.png'])
  } finally { p.close() }
})
