// Tests of seeing a file of the workspace that the agent names, over the conversation, in jsdom (see page.js),
// run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const said = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text }] } } })

// filePage is a page whose conversation holds what the agent said, and whose host has files: path -> text, or a
// { status, body } refusal.
async function filePage(text, { files = {}, viewFiles = true, alsoListed = [], checkFails = false } = {}) {
  const events = [{ seq: 1, kind: 'user', text: 'hi' }, said(2, text)]
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, viewFiles, attention: {} },
    '/api/conversation': (call) => {
      const after = Number(new URLSearchParams(call.query).get('after') || 0)
      return { events: events.filter((e) => e.seq > after), last: 2, running: false }
    },
    // Which of the paths asked about are files: those the host holds, and any that go on being listed though gone by the time they are opened.
    '/api/workspace-files': (call) => {
      if (checkFails) return { status: 500, body: { error: 'boom' } }
      const asked = new URLSearchParams(call.query).getAll('path')
      return { files: asked.filter((a) => a in files || alsoListed.includes(a)) }
    },
    '/api/workspace-file': (call) => {
      const path = new URLSearchParams(call.query).get('path')
      const file = files[path]
      if (file === undefined) return { status: 404, body: { error: 'there is no file ' + path + ' in the workspace', code: 'missing' } }
      return typeof file === 'string' ? { path, text: file } : file
    },
    '/api/log': { entries: [] },
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': 'conversation' } })
  await p.every('pollConversation')
  await p.settle()
  return p
}

const refs = (p) => Array.from(p.$('convo-log').querySelectorAll('button.file-ref')).map((b) => b.textContent)
const checks = (p) => p.calls.filter((c) => c.path === '/api/workspace-files').map((c) => new URLSearchParams(c.query).getAll('path'))
const asked = (p) => p.calls.filter((c) => c.path === '/api/workspace-file').map((c) => new URLSearchParams(c.query).get('path'))
const clipboardOf = (p) => {
  const written = []
  Object.defineProperty(p.window.navigator, 'clipboard', { configurable: true, value: { writeText: async (t) => { written.push(t) } } })
  return written
}

test('a path in code is a button only if the host says it is a file it can show; other code, and what is not a file, is just code', async () => {
  const text = 'The plan is at `doc/plans/a.md`, see `src/x.go`, `README.md`, `./run.sh`, `safe.directory`, `core.checkStat`, `~/.gitconfig`, `go test ./...`, `foo()`, `1.2.3`, `a@b.com`, `src/`, `x`, `https://example.com/a.md`, `a b/c.md` and `Makefile`.'
  const p = await filePage(text, { files: { 'doc/plans/a.md': 'x', 'src/x.go': 'x', 'README.md': 'x' } })
  try {
    assert.deepEqual(refs(p), ['doc/plans/a.md', 'src/x.go', 'README.md'])
    assert.deepEqual(checks(p), [['doc/plans/a.md', 'src/x.go', 'README.md', './run.sh', 'safe.directory', 'core.checkStat', '~/.gitconfig']], 'one ask, of what reads like a path and nothing else')
  } finally { p.close() }
})

test('a path is asked about once however often it is mentioned', async () => {
  const p = await filePage('`a.md` and `a.md` and `b.md`', { files: { 'a.md': 'x' } })
  try {
    assert.deepEqual(checks(p), [['a.md', 'b.md']])
    assert.deepEqual(refs(p), ['a.md', 'a.md'])
  } finally { p.close() }
})

test('when the host cannot say which are files, none is offered', async () => {
  const p = await filePage('See `doc/a.md`.', { files: { 'doc/a.md': 'x' }, checkFails: true })
  try {
    assert.deepEqual(refs(p), [])
    assert.equal(p.$('convo-log').querySelectorAll('code').length, 1, 'still shown as code')
  } finally { p.close() }
})

test('a path in code inside a link, or in a code block, is just code', async () => {
  const p = await filePage('[the `a.md` plan](http://localhost:3000/x)\n\n```\ndoc/plans/a.md\n```')
  try {
    assert.deepEqual(refs(p), [])
  } finally { p.close() }
})

test('nothing can be opened when the host has no folder to show from', async () => {
  const p = await filePage('See `doc/a.md`.', { viewFiles: false })
  try {
    assert.deepEqual(refs(p), [])
    assert.deepEqual(checks(p), [], 'and the host is not asked')
  } finally { p.close() }
})

test('opening a path asks the host for that file, and shows it over the conversation, as Markdown for Markdown', async () => {
  const p = await filePage('See `doc/a.md`.', { files: { 'doc/a.md': '# The plan\n\n- one\n- two\n\n<script>alert(1)</script>' } })
  try {
    assert.ok(!p.$('file-view').open)
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    assert.deepEqual(asked(p), ['doc/a.md'])
    assert.ok(p.$('file-view').open)
    assert.equal(p.$('file-view-path').value, 'doc/a.md')
    assert.equal(p.$('file-view-body').querySelector('h1').textContent, 'The plan')
    assert.equal(p.$('file-view-body').querySelectorAll('li').length, 2)
    assert.equal(p.$('file-view-body').querySelector('script'), null, 'what the file holds is never markup')
    assert.match(p.$('file-view-body').textContent, /<script>alert\(1\)<\/script>/)
  } finally { p.close() }
})

test('any other file is shown as it is, in a block of its own', async () => {
  const p = await filePage('See `src/x.go`.', { files: { 'src/x.go': 'package x\n\n# not a heading\n' } })
  try {
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    const pre = p.$('file-view-body').querySelector('pre')
    assert.equal(pre.textContent, 'package x\n\n# not a heading\n')
    assert.equal(p.$('file-view-body').querySelector('h1'), null)
  } finally { p.close() }
})

test('the file is shown whole and read-only, and closing it leaves the conversation as it was', async () => {
  const p = await filePage('See `doc/a.md`.', { files: { 'doc/a.md': 'text' } })
  try {
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    assert.equal(p.$('file-view-body').querySelector('textarea, input'), null, 'nothing to edit')
    await p.click('file-view-close')
    assert.ok(!p.$('file-view').open)
    assert.deepEqual(refs(p), ['doc/a.md'])
  } finally { p.close() }
})

test('the file has a button copying its text, just as it is', async () => {
  const p = await filePage('See `doc/a.md`.', { files: { 'doc/a.md': '# Plan\n\n  indented\n' } })
  try {
    const written = clipboardOf(p)
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    await p.click('file-view-copy')
    assert.deepEqual(written, ['# Plan\n\n  indented\n'])
    assert.equal(p.$('toast').textContent, 'Copied')
  } finally { p.close() }
})

test('hidden control characters in a file are shown, and copied without', async () => {
  const p = await filePage('See `doc/a.md`.', { files: { 'doc/a.md': 'a\u001b[201~rm -rf b' } })
  try {
    const written = clipboardOf(p)
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    assert.ok(!p.$('file-view-body').textContent.includes('\u001b'), 'the escape is shown as a symbol')
    await p.click('file-view-copy')
    assert.deepEqual(written, ['a[201~rm -rf b'])
    assert.match(p.$('toast').textContent, /hidden control character/)
  } finally { p.close() }
})

test('a file outside the workspace is said to be, with a warning, and nothing of it is shown', async () => {
  const p = await filePage('Open `/etc/notes.md` and `../x.md`.', {
    alsoListed: ['../x.md'],
    files: { '/etc/notes.md': { status: 403, body: { error: '/etc/notes.md is outside the folder this sandbox works in, so vibe won\'t show it', code: 'outside' } } },
  })
  try {
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    assert.ok(p.$('file-view').open)
    assert.ok(p.$('file-view').classList.contains('outside'), 'styled as a warning')
    assert.match(p.$('file-view-body').textContent, /outside the folder this sandbox works in/)
    assert.equal(p.$('file-view-copy').hidden, true, 'nothing to copy')
  } finally { p.close() }
})

test('a file that is not there, or not text, says so in the same place', async () => {
  const p = await filePage('See `gone.md` and `big.bin`.', {
    alsoListed: ['gone.md'],
    files: { 'big.bin': { status: 415, body: { error: 'big.bin doesn\'t look like a text file', code: 'notText' } } },
  })
  try {
    const [gone, big] = Array.from(p.$('convo-log').querySelectorAll('button.file-ref'))
    await p.click(gone)
    assert.match(p.$('file-view-body').textContent, /there is no file gone\.md/)
    assert.ok(!p.$('file-view').classList.contains('outside'))
    await p.click(big)
    assert.match(p.$('file-view-body').textContent, /doesn't look like a text file/)
    assert.equal(p.$('file-view-path').value, 'big.bin')
  } finally { p.close() }
})

test('opening another file after one replaces what is shown', async () => {
  const p = await filePage('`a.md` and `b.md`', { files: { 'a.md': 'first', 'b.md': 'second' } })
  try {
    const [a, b] = Array.from(p.$('convo-log').querySelectorAll('button.file-ref'))
    await p.click(a)
    await p.click('file-view-close')
    await p.click(b)
    assert.equal(p.$('file-view-body').textContent.trim(), 'second')
  } finally { p.close() }
})

test('a path written ./ or ../ in a file being viewed is from that file, and is shown as written', async () => {
  const p = await filePage('See `docs/plans/a.md`.', { files: {
    'docs/plans/a.md': 'Next: `./b.md`, up: `../c.md`, from the root: `src/x.go`, out: `../../../z.md`.',
    'docs/plans/b.md': 'the sibling', 'docs/c.md': 'one up', 'src/x.go': 'x', 'b.md': 'the one at the root, not what was meant',
  } })
  try {
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    const buttons = () => Array.from(p.$('file-view-body').querySelectorAll('button.file-ref'))
    assert.deepEqual(buttons().map((b) => b.textContent), ['./b.md', '../c.md', 'src/x.go'], 'shown as written')
    assert.deepEqual(checks(p).pop(), ['docs/plans/b.md', 'docs/c.md', 'src/x.go', '../z.md'], 'but asked about from the file')
    await p.click(buttons()[0])
    assert.equal(p.$('file-view-path').value, 'docs/plans/b.md')
    assert.equal(p.$('file-view-body').textContent.trim(), 'the sibling')
  } finally { p.close() }
})

test('a Markdown link to a file is a button showing it, in the conversation and in a viewed file, and is otherwise the words and its address', async () => {
  const p = await filePage('See [the plan](docs/plans/a.md), [site](https://example.com/x.md), [gone](nowhere.md) and [part](docs/c.md#top).', { files: {
    'docs/plans/a.md': 'Next: [the sibling](./b.md), [up](../c.md).',
    'docs/plans/b.md': 'the sibling', 'docs/c.md': 'one up',
  } })
  try {
    assert.deepEqual(refs(p), ['the plan', 'part'], 'the words of the link, not its address')
    assert.deepEqual(checks(p), [['docs/plans/a.md', 'nowhere.md', 'docs/c.md']], 'a web address is not asked about')
    const text = p.$('convo-log').textContent
    assert.match(text, /site \(https:\/\/example\.com\/x\.md\)/, 'a web link stays words and its address')
    assert.match(text, /gone \(nowhere\.md\)/, 'so does a link to what is not there')
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    assert.equal(p.$('file-view-path').value, 'docs/plans/a.md')
    const buttons = () => Array.from(p.$('file-view-body').querySelectorAll('button.file-ref'))
    assert.deepEqual(buttons().map((b) => b.textContent), ['the sibling', 'up'])
    await p.click(buttons()[0])
    assert.equal(p.$('file-view-path').value, 'docs/plans/b.md', 'from the viewed file, not the root')
    assert.equal(p.$('file-view-body').textContent.trim(), 'the sibling')
  } finally { p.close() }
})

// --- moving about in the viewer, as in a browser ---

// viewing is a page with a file docs/a.md that links to b.md and c.md beside it, b.md to c.md, and the viewer open on docs/a.md.
async function viewing(extra = {}) {
  const files = Object.assign({
    'docs/a.md': '# A\n\nTo [bee](./b.md) and [sea](./c.md).',
    'docs/b.md': '# B\n\nOn to [sea](./c.md).',
    'docs/c.md': '# C\n\nThe end.',
  }, extra)
  const p = await filePage('See `docs/a.md`.', { files })
  await p.click(p.$('convo-log').querySelector('button.file-ref'))
  Object.assign(p, { files })
  return p
}
const nav = (p) => ({ back: p.$('file-view-back'), forward: p.$('file-view-forward'), reload: p.$('file-view-reload'), url: p.$('file-view-path') })
const follow = (p, words) => p.click(Array.from(p.$('file-view-body').querySelectorAll('button.file-ref')).find((b) => b.textContent === words))
const heading = (p) => p.$('file-view-body').querySelector('h1').textContent

test('the viewer has back, forward and reload, a path bar that can be read and not changed, and a close button that is an icon', async () => {
  const p = await viewing()
  try {
    const n = nav(p)
    assert.ok(n.back && n.forward && n.reload, 'the three buttons are there')
    assert.equal(n.url.tagName, 'INPUT')
    assert.ok(n.url.readOnly, 'the path can be selected, not edited')
    assert.equal(n.url.value, 'docs/a.md')
    assert.equal(n.back.getAttribute('aria-label'), 'Back')
    assert.equal(n.forward.getAttribute('aria-label'), 'Forward')
    assert.equal(n.reload.getAttribute('aria-label'), 'Reload')
    const close = p.$('file-view-close')
    assert.equal(close.getAttribute('aria-label'), 'Close')
    assert.ok(close.querySelector('svg') && close.textContent.trim() === '', 'an icon, not the word')
    const header = p.$('file-view').querySelector('header')
    const order = Array.from(header.children).map((c) => c.id || c.className)
    assert.deepEqual(order.slice(0, 3), ['file-view-back', 'file-view-forward', 'file-view-reload'], 'the buttons first')
    assert.equal(order[order.length - 1], 'file-view-close', 'and the close button last')
    assert.ok(order.indexOf('file-view-url') > 2 && order.indexOf('file-view-url') < order.length - 1, 'the path bar between them')
    assert.ok(header.querySelector('.file-view-url #file-view-path'))
  } finally { p.close() }
})

test('with nowhere to go back or forward to, those buttons are off', async () => {
  const p = await viewing()
  try {
    assert.ok(nav(p).back.disabled && nav(p).forward.disabled)
  } finally { p.close() }
})

test('following a link in the viewer can be undone with Back, and redone with Forward', async () => {
  const p = await viewing()
  try {
    await follow(p, 'bee')
    assert.equal(nav(p).url.value, 'docs/b.md')
    assert.equal(heading(p), 'B')
    assert.ok(!nav(p).back.disabled && nav(p).forward.disabled)
    await p.click(nav(p).back)
    assert.equal(nav(p).url.value, 'docs/a.md')
    assert.equal(heading(p), 'A')
    assert.ok(nav(p).back.disabled && !nav(p).forward.disabled, 'at the start, with the way on to B still there')
    await p.click(nav(p).forward)
    assert.equal(heading(p), 'B')
    assert.ok(nav(p).forward.disabled)
  } finally { p.close() }
})

test('two links away from a file is two Backs from it', async () => {
  const p = await viewing()
  try {
    await follow(p, 'bee')
    await follow(p, 'sea')
    assert.equal(heading(p), 'C')
    await p.click(nav(p).back)
    assert.equal(heading(p), 'B')
    await p.click(nav(p).back)
    assert.equal(heading(p), 'A')
    assert.ok(nav(p).back.disabled)
  } finally { p.close() }
})

test('following a link after going back takes the way forward away, as a browser does', async () => {
  const p = await viewing()
  try {
    await follow(p, 'bee')
    await p.click(nav(p).back)
    await follow(p, 'sea')
    assert.equal(heading(p), 'C')
    assert.ok(nav(p).forward.disabled, 'B is no longer ahead')
    await p.click(nav(p).back)
    assert.equal(heading(p), 'A', 'back from C is A, which it was followed from')
  } finally { p.close() }
})

test('Reload asks the host for the file again, and shows what it holds now', async () => {
  const p = await viewing()
  try {
    p.files['docs/a.md'] = '# A, changed\n\nNo links now.'
    await p.click(nav(p).reload)
    assert.deepEqual(asked(p), ['docs/a.md', 'docs/a.md'])
    assert.equal(heading(p), 'A, changed')
    assert.ok(nav(p).back.disabled, 'reloading goes nowhere')
  } finally { p.close() }
})

test('going back to a file shows it where it was read to', async () => {
  const p = await viewing()
  try {
    const body = p.$('file-view-body')
    body.scrollTop = 120
    await follow(p, 'bee')
    assert.equal(body.scrollTop, 0, 'a file arrived at is at its top')
    await p.click(nav(p).back)
    assert.equal(body.scrollTop, 120)
  } finally { p.close() }
})

test('reloading keeps the place in the file', async () => {
  const p = await viewing()
  try {
    p.$('file-view-body').scrollTop = 80
    await p.click(nav(p).reload)
    assert.equal(p.$('file-view-body').scrollTop, 80)
  } finally { p.close() }
})

test('closing the viewer ends what was followed: the next file opened is the first of a new run', async () => {
  const p = await viewing()
  try {
    await follow(p, 'bee')
    assert.ok(!nav(p).back.disabled)
    await p.click('file-view-close')
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    assert.equal(nav(p).url.value, 'docs/a.md')
    assert.ok(nav(p).back.disabled && nav(p).forward.disabled)
  } finally { p.close() }
})

test('a file that cannot be shown is a place in the history too, and Back leaves it', async () => {
  const p = await filePage('See `docs/a.md`.', { files: { 'docs/a.md': 'To [gone](./gone.md).' }, alsoListed: ['docs/gone.md'] })
  try {
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    await follow(p, 'gone')
    assert.equal(nav(p).url.value, 'docs/gone.md')
    assert.match(p.$('file-view-body').textContent, /there is no file docs\/gone\.md/)
    await p.click(nav(p).back)
    assert.equal(nav(p).url.value, 'docs/a.md')
    assert.match(p.$('file-view-body').textContent, /To gone/)
  } finally { p.close() }
})

test('a refused file keeps its warning only while it is the one shown', async () => {
  const p = await filePage('See `docs/a.md`.', { files: {
    'docs/a.md': 'To [out](./out.md).',
    'docs/out.md': { status: 403, body: { error: 'that is outside the folder this sandbox works in', code: 'outside' } },
  }, alsoListed: ['docs/out.md'] })
  try {
    await p.click(p.$('convo-log').querySelector('button.file-ref'))
    await follow(p, 'out')
    assert.ok(p.$('file-view').classList.contains('outside'))
    await p.click(nav(p).back)
    assert.ok(!p.$('file-view').classList.contains('outside'), 'back at a file that is fine')
  } finally { p.close() }
})
