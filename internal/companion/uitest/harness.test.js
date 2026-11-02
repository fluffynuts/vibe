// Tests of the harness the page tests run in (page.js): what it keeps of what jsdom says, so that
// a page script that throws is never quiet.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

test("something jsdom can't do is kept apart from an error in the page, which is kept and not hidden", async () => {
  const p = await openPage({ routes: { '/api/state': { sandbox: 'box', sandboxTabs: true, attention: {} }, '/api/log': { entries: [] } } })
  const printed = []
  const original = console.error
  console.error = (...args) => { printed.push(args.map(String).join(' ')) }
  try {
    p.window.eval("setTimeout(function () { throw new Error('the page broke') }, 0)")
    p.window.eval('location.reload()')
    // Until the page's timer has run and jsdom has said so, not for a count of turns that may be shorter than the timer.
    const deadline = Date.now() + 5000
    while (p.errors.length === 0 && Date.now() < deadline) await new Promise((resolve) => setTimeout(resolve, 5))
  } finally {
    console.error = original
    p.close()
  }
  assert.ok(p.unimplemented.some((m) => /navigation/.test(m)), 'a navigation was not kept as unimplemented: ' + JSON.stringify(p.unimplemented))
  assert.ok(!p.unimplemented.some((m) => /the page broke/.test(m)), 'an error in the page was taken for something unimplemented')
  assert.ok(p.errors.some((m) => /the page broke/.test(m)), 'an error in the page was not kept: ' + JSON.stringify(p.errors))
  assert.ok(printed.some((m) => /the page broke/.test(m)), 'an error in the page was not printed: ' + JSON.stringify(printed))
  assert.ok(!printed.some((m) => /navigation/.test(m)), 'a navigation, which a test may be looking for, was printed')
})
