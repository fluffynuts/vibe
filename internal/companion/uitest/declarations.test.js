// The page's scripts declare each of their functions once: a second declaration of a name in the same scope silently replaces the
// first, everywhere it is called (the Activity tab's needs-attention balloon once took over the tabs' attention dots so).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')

const ui = path.join(__dirname, '..', 'ui')

for (const file of fs.readdirSync(ui).filter((f) => f.endsWith('.js'))) {
  test(file + ' declares each function of its outermost scope once', () => {
    const text = fs.readFileSync(path.join(ui, file), 'utf8').replace(/\r\n/g, '\n')
    const seen = new Map()
    const twice = []
    for (const [i, line] of text.split('\n').entries()) {
      const m = /^( {0,2})(?:async )?function\*? ?([A-Za-z0-9_$]+)\s*\(/.exec(line)
      if (!m) continue
      const key = m[1] + m[2]
      if (seen.has(key)) twice.push(m[2] + ' (lines ' + seen.get(key) + ' and ' + (i + 1) + ')')
      else seen.set(key, i + 1)
    }
    assert.deepEqual(twice, [], 'declared twice')
  })
}
