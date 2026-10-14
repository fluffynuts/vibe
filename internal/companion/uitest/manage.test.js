// Tests for ui/manage.js, run by `node --test` (see manage_test.go).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { likelyTrash, coveredBy, picked, summary } = require('../ui/manage.js')

const inv = {
  profiles: [
    { id: 'profile:keep', trash: false, sandboxes: [
      { id: 'sandbox:live', trash: false },
      { id: 'sandbox:ghost', trash: true },
    ] },
    { id: 'profile:trash', trash: true, sandboxes: [{ id: 'sandbox:old', trash: true }] },
    { id: 'profile:unused', trash: true, sandboxes: [] },
    { id: 'profile:golden', trash: false, kept: 'it ships with vibe', sandboxes: [{ id: 'sandbox:g', trash: true }] },
    { id: 'profile:busy', trash: true, kept: "its sandbox 'b' can't be deleted", sandboxes: [
      { id: 'sandbox:b', trash: true, kept: 'a vibe is attached to it' },
      { id: 'sandbox:c', trash: true },
    ] },
  ],
  unknown: [{ id: 'unknown:handmade' }],
}

test('likely trash: whole profiles, and trash sandboxes of profiles kept', () => {
  assert.deepEqual([...likelyTrash(inv)].sort(), [
    'profile:trash', 'profile:unused', 'sandbox:c', 'sandbox:g', 'sandbox:ghost',
  ])
})

test("a ticked profile's sandboxes aren't sent as well", () => {
  const ticked = new Set(['profile:trash', 'sandbox:old', 'sandbox:live'])
  assert.deepEqual(picked(inv, ticked), ['sandbox:live', 'profile:trash'])
  assert.equal(coveredBy(inv, ticked, 'sandbox:old').id, 'profile:trash')
  assert.equal(coveredBy(inv, ticked, 'sandbox:live'), null)
})

test("what vibe keeps isn't sent, even if ticked", () => {
  const ticked = new Set(['profile:golden', 'sandbox:b', 'unknown:handmade'])
  assert.deepEqual(picked(inv, ticked), ['unknown:handmade'])
})

test('the summary counts what goes', () => {
  assert.equal(summary(inv, new Set()), 'nothing selected')
  assert.equal(summary(inv, new Set(['profile:keep', 'unknown:handmade'])), '1 profile and 3 sandboxes selected')
  assert.equal(summary(inv, new Set(['sandbox:old'])), '1 sandbox selected')
})
