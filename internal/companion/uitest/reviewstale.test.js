// The Review tab's diff is a snapshot, and the agent, answering a round of review, can change the code after it: comments it then
// makes are placed for the new code, on a diff of the old (Refresh puts it right). So when the agent's turn ends, the page asks
// whether the code has changed since the diff it holds: if it has, with the Review tab open it says so, beside Refresh (a refresh
// there would undo what is being read: the gaps opened, the place in the diff); with another tab open, the diff is worked out
// afresh when the tab is next shown. A turn that changed nothing is none of the tab's business: no note, no refetch.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const running = { threads: [], lastId: 0, started: '2026-10-16T10:00:00Z', state: 'done' }
const file = { path: 'a.go', status: 'modified', lines: 3, fingerprint: 'f1', hunks: [{ oldStart: 1, newStart: 1, lines: [{ kind: 'add', new: 1, text: 'x' }] }] }

// stalePage opens the page on tab, with a review on and its diff keyed diffKey. world.changed is what vibe will say of the code,
// world.running whether the agent's turn is on, and world.failing makes the question fail.
async function stalePage({ tab = 'review', review = running, world = {} } = {}) {
  const w = Object.assign({ changed: false, running: false, failing: false, diffKey: 'k1' }, world)
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': (call) => (call.query.includes('diff=1') ? { review, diff: [file], diffKey: w.diffKey } : { review }),
      '/api/review/changed': async () => {
        if (w.hold) await w.hold
        if (w.failing) throw new Error('vibe says no')
        return { changed: w.changed, key: 'k2' }
      },
      '/api/conversation': () => ({ events: w.events || [], last: w.events && w.events.length ? w.events[w.events.length - 1].seq : 0, running: w.running }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': tab },
  })
  await p.every('pollConversation')
  return Object.assign(p, { world: w })
}

const diffLoads = (p) => p.calls.filter((c) => c.path === '/api/review' && c.query.includes('diff=1')).length
const asked = (p) => p.calls.filter((c) => c.path === '/api/review/changed')
const note = (p) => p.$('review-stale')

// agentsTurn runs one turn of the agent's, from its start to its end, with the page polling it.
async function agentsTurn(p, { duringChange = false } = {}) {
  p.world.running = true
  await p.every('pollConversation')
  if (duringChange) p.world.changed = true
  p.world.running = false
  await p.every('pollConversation')
}

test('a turn that changed the code, with the Review tab open, says so beside Refresh and does not refresh', async () => {
  const p = await stalePage()
  try {
    assert.equal(note(p).hidden, true, 'nothing to say yet')
    const loads = diffLoads(p)
    p.world.changed = true
    await agentsTurn(p)
    assert.equal(note(p).hidden, false)
    assert.match(note(p).textContent, /code has changed/i)
    assert.equal(diffLoads(p), loads, 'the diff was not refetched under whoever is reading it')
    assert.deepEqual(asked(p).map((c) => c.query), ['?key=k1'], 'asked once, with the key of the diff it holds')
  } finally { p.close() }
})

test('a turn that changed nothing says nothing', async () => {
  const p = await stalePage()
  try {
    await agentsTurn(p)
    assert.equal(asked(p).length, 1, 'it did ask')
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})

test('nothing is asked while the agent works, only when its turn is over', async () => {
  const p = await stalePage()
  try {
    p.world.running = true
    await p.every('pollConversation')
    await p.every('pollConversation')
    assert.equal(asked(p).length, 0)
    p.world.running = false
    await p.every('pollConversation')
    assert.equal(asked(p).length, 1)
  } finally { p.close() }
})

test('the note stays through the polls, until Refresh, which works the diff out afresh and clears it', async () => {
  const p = await stalePage()
  try {
    p.world.changed = true
    await agentsTurn(p)
    await p.poll()
    await p.poll()
    assert.equal(note(p).hidden, false, 'a poll does not take it away')
    const loads = diffLoads(p)
    p.world.diffKey = 'k3'
    await p.click('review-refresh')
    assert.equal(diffLoads(p), loads + 1)
    assert.equal(note(p).hidden, true)
    // And the next question is about the diff now held.
    p.world.changed = false
    await agentsTurn(p)
    assert.equal(asked(p).pop().query, '?key=k3')
  } finally { p.close() }
})

test('with another tab open, a turn that changed the code has the diff worked out afresh when the Review tab is shown', async () => {
  const p = await stalePage()
  try {
    await p.click('tab-log')
    const loads = diffLoads(p)
    p.world.changed = true
    await agentsTurn(p)
    assert.equal(note(p).hidden, true, 'no note for a tab nobody is looking at')
    assert.equal(diffLoads(p), loads, 'and nothing is fetched until it is looked at')
    await p.click('tab-review')
    assert.equal(diffLoads(p), loads + 1)
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})

test('with another tab open, a turn that changed nothing leaves the diff, and what is open in it, alone', async () => {
  const p = await stalePage()
  try {
    await p.click('tab-log')
    const loads = diffLoads(p)
    await agentsTurn(p)
    assert.equal(asked(p).length, 1)
    await p.click('tab-review')
    assert.equal(diffLoads(p), loads, 'the diff is not worked out again')
  } finally { p.close() }
})

test('the agent is not asked about code with no review on', async () => {
  const p = await stalePage({ review: { threads: [], lastId: 0 } })
  try {
    await agentsTurn(p)
    assert.equal(asked(p).length, 0)
  } finally { p.close() }
})

test('nothing is asked before there is a diff to ask about', async () => {
  const p = await stalePage({ tab: 'log' })
  try {
    await agentsTurn(p)
    assert.equal(asked(p).length, 0, 'the Review tab has not been shown, so no diff was worked out')
  } finally { p.close() }
})

test('a diff without whitespace changes is asked about as that diff', async () => {
  const p = await stalePage()
  try {
    p.$('review-whitespace').click()
    await p.settle()
    await agentsTurn(p)
    assert.equal(asked(p).pop().query, '?key=k1&whitespace=ignore')
  } finally { p.close() }
})

test('a question vibe cannot answer is no reason to say anything', async () => {
  const p = await stalePage({ world: { failing: true } })
  try {
    await agentsTurn(p)
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})

test('an answer about a diff that has been replaced since is not about the one on the page', async () => {
  let release
  const p = await stalePage({ world: { changed: true, hold: new Promise((resolve) => { release = resolve }) } })
  const turns = async () => { for (let i = 0; i < 10; i++) await new Promise((resolve) => setImmediate(resolve)) }
  try {
    p.world.running = true
    await p.every('pollConversation')
    p.world.running = false
    await p.kick('pollConversation') // the turn is over: the question goes, and is held
    assert.equal(asked(p).length, 1, 'asked, and not answered')
    p.world.diffKey = 'k3'
    p.$('review-refresh').click() // the diff held is another now (not p.click: that waits for every call, and one is held)
    await turns()
    release()
    await turns()
    await p.settle()
    assert.equal(note(p).hidden, true, 'the answer was about the diff before the Refresh')
  } finally { p.close() }
})

// --- a comment on a file the diff on the page does not have ----------------------------------------

// The review has a comment (#7) on b.go, and one (#8) on a.go, and one on the whole review (#9); the diff the page holds has
// a.go alone. #7 is drawn under "On files not in this diff", which is the plain truth when the diff is current, and the page
// being behind when the code has changed since it was worked out: then Refresh would put it where it belongs. So going to it
// asks vibe, and says so only when the page is behind. It says it in a toast, with a Refresh of its own, because the comments
// pane is a fly-out over the right of the diff and can cover the note beside Refresh; the note shows as well, for when it is shut.
const said = (by, body) => [{ author: by, body, at: '2026-10-16T11:00:00Z', sent: true }]
const withMissing = {
  threads: [
    { id: 7, file: 'b.go', side: 'new', line: 3, status: 'open', comments: said('agent', 'why this?') },
    { id: 8, file: 'a.go', side: 'new', line: 1, status: 'open', comments: said('agent', 'and this?') },
    { id: 9, status: 'open', comments: said('agent', 'on the whole review') },
  ],
  lastId: 9, started: '2026-10-16T10:00:00Z', state: 'done',
}
const toastShowing = (p) => (p.$('toast').hidden ? null : p.$('toast').textContent)
const mentions = (id) => ({ seq: 1, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text: 'please look at #' + id }] } } })

async function pageWithMissing(world = {}) {
  const p = await stalePage({ review: withMissing, world })
  await p.click('review-comments-open')
  return p
}
const cardFor = (p, id) => p.$('review-comments-list').querySelector('[data-thread="' + id + '"]')
const goTo = async (p, id) => { cardFor(p, id).click(); await p.settle() }

test('going to a comment on a file the diff lacks, when the code has changed since, says so with a Refresh of its own, and the note shows too', async () => {
  const p = await pageWithMissing({ changed: true })
  try {
    assert.ok(p.$('review-thread-7'), 'the premise: #7 is drawn, under the files not in the diff')
    assert.equal(p.$('review-files').querySelector('[data-path="b.go"]'), null, 'and b.go is not in the diff')
    await goTo(p, 7)
    assert.equal(asked(p).length, 1, 'vibe was asked')
    assert.match(toastShowing(p), /isn't in the diff you have/)
    assert.equal(p.$('toast').querySelector('.toast-action').textContent, 'Refresh')
    assert.equal(note(p).hidden, false, 'the note beside Refresh shows, for when the pane is shut')
    assert.ok(p.$('review-refresh').classList.contains('stale'))
  } finally { p.close() }
})

test('going to one when the diff is current says nothing: the file just is not in it', async () => {
  const p = await pageWithMissing({ changed: false })
  try {
    await goTo(p, 7)
    assert.equal(asked(p).length, 1, 'vibe was asked')
    assert.equal(toastShowing(p), null)
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})

test('with the note already showing, going to one says it at once, without asking again', async () => {
  const p = await pageWithMissing({ changed: true })
  try {
    await p.every('pollConversation')
    p.world.running = true
    await p.every('pollConversation')
    p.world.running = false
    await p.every('pollConversation') // a turn ends: the code has changed, so the note shows
    assert.equal(note(p).hidden, false)
    const before = asked(p).length
    await goTo(p, 7)
    assert.match(toastShowing(p), /isn't in the diff you have/)
    assert.equal(asked(p).length, before, 'no second question')
  } finally { p.close() }
})

test('the Refresh in that toast works the diff out afresh, and takes the toast and the note away', async () => {
  const p = await pageWithMissing({ changed: true })
  try {
    await goTo(p, 7)
    const loads = diffLoads(p)
    p.world.diffKey = 'k4'
    p.$('toast').querySelector('.toast-action').click()
    await p.settle()
    assert.equal(diffLoads(p), loads + 1)
    assert.equal(note(p).hidden, true)
    assert.equal(p.$('toast').hidden, true)
  } finally { p.close() }
})

test('a link to such a comment, in the conversation, says the same', async () => {
  const p = await pageWithMissing({ changed: true, events: [mentions(7)] })
  try {
    await p.every('pollConversation')
    p.$('convo-log').querySelector('.thread-ref').click()
    await p.settle()
    assert.match(toastShowing(p) || '', /isn't in the diff you have/)
    assert.equal(note(p).hidden, false)
  } finally { p.close() }
})

test('a link to a comment the review does not have says there is none, and asks nothing about the code', async () => {
  const p = await pageWithMissing({ changed: true, events: [mentions(99)] })
  try {
    await p.every('pollConversation')
    p.$('convo-log').querySelector('.thread-ref').click()
    await p.settle()
    assert.match(toastShowing(p) || '', /no comment #99/)
    assert.equal(asked(p).length, 0)
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})

test('a comment the diff does have, and one on the whole review, are gone to without a word about the diff or a question', async () => {
  const p = await pageWithMissing({ changed: true })
  try {
    assert.ok(p.$('review-thread-8'), 'the premise: the page has a card for #8 in a file')
    await goTo(p, 8)
    await goTo(p, 9)
    assert.equal(toastShowing(p), null)
    assert.equal(asked(p).length, 0)
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})
