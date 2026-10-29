// How the companion page frames the clipboard page beside the conversation.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

test('the clipboard beside the conversation is framed with a note that it is framed, and "open on its own" is not', async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, clipboardUrl: 'http://localhost:5390/', clipboardReady: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
  })
  try {
    await p.poll()
    assert.equal(p.$('convo-clipboard-frame').getAttribute('src'), 'http://localhost:5390/?framed=1')
    assert.equal(p.$('convo-clipboard-open').getAttribute('href'), 'http://localhost:5390/')
  } finally {
    p.close()
  }
})
