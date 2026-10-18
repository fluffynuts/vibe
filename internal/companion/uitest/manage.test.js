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

const { filtered } = require('../ui/manage.js')

const many = {
  profiles: [
    { id: 'profile:yumbi', name: 'yumbi', features: ['dotnet'], flags: [], sandboxes: [
      { id: 'sandbox:api', name: 'yumbi-api', folder: '~/code/yumbi/api', flags: ['running'] },
      { id: 'sandbox:web', name: 'yumbi-web', folder: '~/code/yumbi/web', flags: ['stopped', 'folder gone'], trash: true },
    ] },
    { id: 'profile:go', name: 'go', features: ['go'], flags: [], sandboxes: [
      { id: 'sandbox:vibe', name: 'vibe', folder: '~/code/opensource/vibe', flags: ['running'] },
    ] },
    { id: 'profile:test', name: 'test', features: [], flags: ['no sandboxes'], trash: true, sandboxes: [] },
  ],
  unknown: [{ id: 'unknown:handmade', name: 'handmade', running: false }],
}
const shownIds = (f) => [
  ...f.profiles.flatMap((p) => [p.id, ...p.sandboxes.map((s) => s.id)]),
  ...f.unknown.map((u) => u.id),
]

test('no filter shows everything', () => {
  const f = filtered(many, '  ')
  assert.deepEqual(shownIds(f), shownIds(many))
  assert.equal(f.shown, 4)
  assert.equal(f.total, 4)
})

test('a sandbox matches on its name or folder, ignoring case', () => {
  assert.deepEqual(shownIds(filtered(many, 'OPENSOURCE')), ['profile:go', 'sandbox:vibe'])
  assert.deepEqual(shownIds(filtered(many, 'yumbi-web')), ['profile:yumbi', 'sandbox:web'])
})

test("a profile's name match shows all its sandboxes", () => {
  assert.deepEqual(shownIds(filtered(many, 'yumbi')), ['profile:yumbi', 'sandbox:api', 'sandbox:web'])
  assert.deepEqual(shownIds(filtered(many, 'dotnet')), ['profile:yumbi', 'sandbox:api', 'sandbox:web'])
})

test('a profile with no sandboxes shows when it matches', () => {
  assert.deepEqual(shownIds(filtered(many, 'test')), ['profile:test'])
})

test('every word has to match, across the profile and the sandbox', () => {
  assert.deepEqual(shownIds(filtered(many, 'yumbi running')), ['profile:yumbi', 'sandbox:api'])
  assert.deepEqual(shownIds(filtered(many, 'folder gone')), ['profile:yumbi', 'sandbox:web'])
  assert.deepEqual(shownIds(filtered(many, 'vibe stopped')), [])
})

test('likely trash, and sandboxes vibe has no record of, can be found too', () => {
  assert.deepEqual(shownIds(filtered(many, 'trash')), ['profile:yumbi', 'sandbox:web', 'profile:test'])
  assert.deepEqual(shownIds(filtered(many, 'handmade')), ['unknown:handmade'])
  assert.deepEqual(shownIds(filtered(many, 'stopped')), ['profile:yumbi', 'sandbox:web', 'unknown:handmade'])
})

test('the count is of sandboxes shown, of all of them', () => {
  const f = filtered(many, 'yumbi')
  assert.equal(f.shown, 2)
  assert.equal(f.total, 4)
})

test('the state filter shows only running, or only stopped, sandboxes', () => {
  assert.deepEqual(shownIds(filtered(many, '', 'running')), ['profile:yumbi', 'sandbox:api', 'profile:go', 'sandbox:vibe'])
  assert.deepEqual(shownIds(filtered(many, '', 'stopped')), ['profile:yumbi', 'sandbox:web', 'unknown:handmade'])
  assert.deepEqual(shownIds(filtered(many, '', 'all')), shownIds(many))
})

test("the state filter narrows a profile's own match to sandboxes in that state", () => {
  assert.deepEqual(shownIds(filtered(many, 'yumbi', 'stopped')), ['profile:yumbi', 'sandbox:web'])
  assert.deepEqual(shownIds(filtered(many, 'test', 'running')), [])
})

test('the state filter counts what it hides', () => {
  const f = filtered(many, '', 'running')
  assert.equal(f.shown, 2)
  assert.equal(f.total, 4)
})
