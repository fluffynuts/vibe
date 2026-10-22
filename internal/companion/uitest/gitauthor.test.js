// Tests of the question of giving the agent the user's git author, on the page, in jsdom (see page.js), run with
// page.test.js: a dialog over the whole page that has to be answered, and a reminder, in the conversation, once
// the answer is "later".
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const ASKING = { phase: 'ask', shown: 'Davyd McColl <d@x.com>', push: "It has no GitHub token, so it can't push: what it commits stays on this machine until you push it." }

// authorPage is a page whose host is asking about the git author as at.git says, and answers as at.reply says: by
// default as the host does (give and leave end the question, later makes it a reminder, fix asks again, dismiss ends it).
async function authorPage(git, { tab = 'review' } = {}) {
  const at = { git, reply: null, hold: null, kept: '' }
  const next = { give: () => ({}), leave: () => ({}), later: () => ({ ...ASKING, phase: 'reminder' }), fix: () => ({ ...ASKING }), dismiss: () => ({}) }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, conversation: true, review: true, attention: {}, ...(at.git === undefined ? {} : { gitAuthor: at.git }) }),
    '/api/conversation': { events: [], last: 0, running: false },
    '/api/review': { review: { threads: [] }, diff: [] },
    '/api/log': { entries: [] },
    '/api/git-author': async (call) => {
      if (at.hold) await at.hold
      if (at.reply) return at.reply
      at.git = next[call.body.answer]()
      const kept = at.kept && (call.body.answer === 'give' || call.body.answer === 'leave') ? at.kept : ''
      return { gitAuthor: at.git, kept }
    },
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': tab } })
  await p.poll()
  return Object.assign(p, { at })
}

const dialog = (p) => p.$('git-author')
const note = (p) => p.$('convo-git-author')
const answers = (p) => p.posted('/api/git-author').map((b) => b.answer)
const choose = (p, answer) => p.click('git-author-' + answer)

test('a missing git author is asked about in a dialog over the page, whatever tab it is on, saying what it would give and whether the agent can push', async () => {
  const p = await authorPage(ASKING, { tab: 'review' })
  try {
    assert.equal(dialog(p).open, true)
    const text = dialog(p).textContent
    assert.match(text, /can't commit/)
    assert.match(text, /Davyd McColl <d@x\.com>/)
    assert.match(text, /can't push/)
  } finally { p.close() }
})

test('nothing is asked when the host asks nothing, or has no such question', async () => {
  for (const git of [undefined, {}, { phase: '' }]) {
    const p = await authorPage(git)
    try {
      assert.equal(dialog(p).open, false)
      assert.equal(note(p).hidden, true)
    } finally { p.close() }
  }
})

test('the dialog cannot be put away without a choice: Escape does not close it', async () => {
  const p = await authorPage(ASKING)
  try {
    const cancel = new p.window.Event('cancel', { cancelable: true })
    dialog(p).dispatchEvent(cancel)
    assert.equal(cancel.defaultPrevented, true)
    assert.equal(dialog(p).open, true)
  } finally { p.close() }
})

test('asking again on every poll neither reopens nor duplicates the dialog', async () => {
  const p = await authorPage(ASKING)
  try {
    await p.poll()
    await p.poll()
    assert.equal(dialog(p).open, true)
    assert.equal(p.window.document.querySelectorAll('#git-author').length, 1)
  } finally { p.close() }
})

test('giving the author sends that, and the dialog goes once the host says it is done', async () => {
  const p = await authorPage(ASKING)
  try {
    await choose(p, 'give')
    assert.deepEqual(answers(p), ['give'])
    assert.equal(dialog(p).open, false)
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})

test('leaving it sends that, and nothing is shown after', async () => {
  const p = await authorPage(ASKING)
  try {
    await choose(p, 'leave')
    assert.deepEqual(answers(p), ['leave'])
    assert.equal(dialog(p).open, false)
    assert.equal(note(p).hidden, true)
  } finally { p.close() }
})

test('later puts the dialog away and leaves a reminder in the conversation, saying the agent cannot commit', async () => {
  const p = await authorPage(ASKING)
  try {
    await choose(p, 'later')
    assert.deepEqual(answers(p), ['later'])
    assert.equal(dialog(p).open, false)
    assert.equal(note(p).hidden, false)
    assert.match(note(p).textContent, /agent can't commit in this sandbox/)
    assert.match(note(p).textContent, /\.gitconfig/)
  } finally { p.close() }
})

test('a reminder that is already there when the page opens is shown, not asked as a dialog', async () => {
  const p = await authorPage({ ...ASKING, phase: 'reminder' })
  try {
    assert.equal(dialog(p).open, false)
    assert.equal(note(p).hidden, false)
  } finally { p.close() }
})

test('the reminder reopens the question, or is dismissed', async () => {
  const p = await authorPage({ ...ASKING, phase: 'reminder' })
  try {
    await p.click('convo-git-author-fix')
    assert.deepEqual(answers(p), ['fix'])
    assert.equal(dialog(p).open, true)
    assert.equal(note(p).hidden, true)
    await choose(p, 'later')
    await p.click('convo-git-author-dismiss')
    assert.deepEqual(answers(p), ['fix', 'later', 'dismiss'])
    assert.equal(note(p).hidden, true)
    assert.equal(dialog(p).open, false)
  } finally { p.close() }
})

test('a question answered elsewhere closes the dialog on the next poll', async () => {
  const p = await authorPage(ASKING)
  try {
    p.at.git = {}
    await p.poll()
    assert.equal(dialog(p).open, false)
  } finally { p.close() }
})

test('the buttons wait while an answer is being carried out, so it is sent once', async () => {
  const p = await authorPage(ASKING)
  try {
    let release
    p.at.hold = new Promise((resolve) => { release = resolve })
    const first = choose(p, 'give')
    await new Promise((resolve) => setImmediate(resolve))
    for (const a of ['give', 'leave', 'later']) assert.equal(p.$('git-author-' + a).disabled, true, a + ' is clickable meanwhile')
    p.$('git-author-give').click()
    release()
    await first
    await p.settle()
    assert.deepEqual(answers(p), ['give'])
  } finally { p.close() }
})

test('an answer that fails is said in the dialog, which stays for another try', async () => {
  const p = await authorPage(ASKING)
  try {
    p.at.reply = { status: 500, body: { error: "couldn't give the agent a git author: no room" } }
    await choose(p, 'give')
    assert.equal(dialog(p).open, true)
    assert.match(p.$('git-author-problem').textContent, /no room/)
    assert.equal(p.$('git-author-problem').hidden, false)
    assert.equal(p.$('git-author-give').disabled, false)
    p.at.reply = null
    await choose(p, 'give')
    assert.equal(dialog(p).open, false)
    assert.equal(p.$('git-author-problem').hidden, true)
  } finally { p.close() }
})

test('an answer the host says no longer fits is said, and the page looks again at what the host is asking', async () => {
  const p = await authorPage(ASKING)
  try {
    p.at.reply = { status: 409, body: { error: 'that answer doesn\'t fit now: it was answered already' } }
    p.at.git = {}
    await choose(p, 'give')
    assert.equal(dialog(p).open, false, 'what the host now asks is nothing')
  } finally { p.close() }
})

test('what the host kept is said in a toast, with how to undo it, and nothing is said when nothing was kept', async () => {
  const p = await authorPage(ASKING)
  try {
    p.at.kept = "kept: every sandbox of profile 'p' is given your author; delete git-author to be asked again"
    await choose(p, 'later')
    assert.ok(!/kept:/.test(p.$('toast').textContent), 'a later keeps nothing')
    await p.click('convo-git-author-fix')
    await choose(p, 'give')
    assert.match(p.$('toast').textContent, /kept: every sandbox of profile 'p'.*to be asked again/)
  } finally { p.close() }
})
