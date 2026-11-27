// :names: in what the page shows (the agent's messages, the user's, review comments) are drawn as their emoji, as GitHub draws
// them; never in code. In jsdom (see page.js).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const agentSaid = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text }] } } })

test("the agent's :names: are drawn as emoji, but not in code", async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [agentSaid(1, ':bug: fixed the pie, and `:tada:` is the code for 🎉\n\n```\n:rocket:\n```')], last: 1, running: false },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  try {
    await p.every('pollConversation')
    const said = p.$('convo-log').textContent
    assert.match(said, /🐛 fixed the pie/)
    assert.match(said, /:tada: is the code/, 'drawn in inline code')
    assert.match(said, /:rocket:/, 'drawn in a code block')
  } finally { p.close() }
})

test("a review comment's :names: are drawn as emoji", async () => {
  const thread = { id: 1, file: '', side: 'new', line: 0, endLine: 0, comments: [{ author: 'agent', body: ':sparkles: looks good', at: '2026-11-16T10:00:00Z' }], status: 'open' }
  const review = { threads: [thread], lastId: 1, started: '2026-11-16T09:00:00Z', ref: 'master', state: 'done' }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': (call) => (call.query.includes('diff=1') ? { review, diff: [], diffKey: 'k' } : { review }),
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  try {
    await p.settle()
    assert.match(p.$('panel-review').textContent, /✨ looks good/)
  } finally { p.close() }
})
