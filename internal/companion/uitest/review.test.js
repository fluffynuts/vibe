// Tests for ui/review.js, run by `node --test` (see review_ui_test.go).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { placeThreads, inReadingOrder } = require('../ui/review.js')

const files = [
  { path: 'a.go', status: 'modified', hunks: [{ oldStart: 10, newStart: 10, lines: [
    { kind: 'context', old: 10, new: 10, text: 'x' },
    { kind: 'del', old: 11, text: 'old' },
    { kind: 'add', new: 11, text: 'new' },
    { kind: 'add', new: 12, text: 'more' },
  ] }] },
  { path: 'b.go', status: 'added', hunks: [{ oldStart: 0, newStart: 1, lines: [{ kind: 'add', new: 1, text: 'b' }] }] },
]

test('a comment goes after the last line it is on, by side', () => {
  const placed = placeThreads(files, [
    { id: 1, file: 'a.go', side: 'new', line: 11, endLine: 12 },
    { id: 2, file: 'a.go', side: 'old', line: 11, endLine: 11 },
    { id: 3, file: 'b.go', side: 'new', line: 1, endLine: 1 },
  ])
  assert.deepEqual(placed.after['a.go'], { '0:3': [1], '0:1': [2] })
  assert.deepEqual(placed.after['b.go'], { '0:0': [3] })
  assert.deepEqual(placed.lines['a.go'], { '0:2': [1], '0:3': [1], '0:1': [2] })
})

test("general comments, and comments on lines the diff hasn't, are kept apart", () => {
  const placed = placeThreads(files, [
    { id: 1 },
    { id: 2, file: 'a.go', side: 'new', line: 400, endLine: 400 },
    { id: 3, file: 'gone.go', side: 'new', line: 1, endLine: 1 },
  ])
  assert.deepEqual(placed.general, [1])
  assert.deepEqual(placed.offDiff, { 'a.go': [2] })
  assert.deepEqual(placed.elsewhere, [3])
})

test('files come in the reading order, then the rest as they were', () => {
  const ordered = inReadingOrder([{ path: 'a.go' }, { path: 'b.go' }, { path: 'c.go' }],
    [{ file: 'c.go', label: 'start here' }, { file: 'a.go', label: 'then' }, { file: 'nowhere.go' }])
  assert.deepEqual(ordered.map((f) => f.file.path + ':' + (f.stop ? f.stop.label : '')), ['c.go:start here', 'a.go:then', 'b.go:'])
})

test("a comment whose lines read differently now is out of date", () => {
  const placed = placeThreads(files, [
    { id: 1, file: 'a.go', side: 'new', line: 11, endLine: 12, code: 'new\nmore' },
    { id: 2, file: 'a.go', side: 'new', line: 11, endLine: 11, code: 'what it said before' },
    { id: 3, file: 'a.go', side: 'new', line: 11, endLine: 11 },
  ])
  assert.deepEqual([...placed.outdated], [2])
})

const { sideBySide } = require('../ui/review.js')

test('side by side, deleted lines face the added ones that replaced them', () => {
  const hunk = { lines: [
    { kind: 'context' }, // 0
    { kind: 'del' }, { kind: 'del' }, { kind: 'del' }, // 1-3
    { kind: 'add' }, { kind: 'add' }, // 4-5
    { kind: 'context' }, // 6
    { kind: 'add' }, // 7
    { kind: 'del' }, // 8
  ] }
  assert.deepEqual(sideBySide(hunk), [
    { left: 0, right: 0 },
    { left: 1, right: 4 },
    { left: 2, right: 5 },
    { left: 3, right: null },
    { left: 6, right: 6 },
    { left: null, right: 7 },
    { left: 8, right: null },
  ])
})

const { gaps, gapView } = require('../ui/review.js')

// Two hunks: new lines 24-30 (old 24-29) and 79-89 (old 78-83).
const twoHunks = [
  { oldStart: 24, newStart: 24, lines: [
    { kind: 'context' }, { kind: 'context' }, { kind: 'context' }, { kind: 'add' },
    { kind: 'context' }, { kind: 'context' }, { kind: 'context' }] },
  { oldStart: 78, newStart: 79, lines: [
    { kind: 'context' }, { kind: 'context' }, { kind: 'context' },
    { kind: 'add' }, { kind: 'add' }, { kind: 'add' }, { kind: 'add' }, { kind: 'add' },
    { kind: 'context' }, { kind: 'context' }, { kind: 'context' }] },
]

test('the gaps are what the diff leaves out, with old numbers to match', () => {
  assert.deepEqual(gaps(twoHunks, 120), [
    { index: 0, from: 1, to: 23, offset: 0 },
    { index: 1, from: 31, to: 78, offset: 1 },
    { index: 2, from: 90, to: 120, offset: 6 },
  ])
  // Before the file's been read, how long it is isn't known.
  assert.equal(gaps(twoHunks, null)[2].to, null)
})

test('a gap opens 20 lines at a time from the end asked for', () => {
  const gap = { index: 1, from: 31, to: 78, offset: 1 }
  const closed = gapView(gap, { down: 0, up: 0 }, '')
  assert.deepEqual([closed.down.length, closed.up.length, closed.hidden, closed.buttons], [0, 0, 48, ['down', 'up']])
  const opened = gapView(gap, { down: 20, up: 20 }, '')
  assert.deepEqual([opened.down[0], opened.down[19], opened.up[0], opened.up[19], opened.hidden, opened.buttons],
    [31, 50, 59, 78, 8, ['all']])
  const whole = gapView(gap, { down: 40, up: 20 }, '')
  assert.deepEqual([whole.down.length, whole.up.length, whole.hidden, whole.buttons], [48, 0, 0, []])
})

test("the file's ends open one way only", () => {
  assert.deepEqual(gapView({ index: 0, from: 1, to: 23, offset: 0 }, { down: 0, up: 0 }, 'first').buttons, ['up'])
  const end = gapView({ index: 2, from: 90, to: null, offset: 6 }, { down: 0, up: 0 }, 'last')
  assert.deepEqual([end.hidden, end.buttons], [null, ['down']])
  assert.deepEqual(gapView({ index: 0, from: 1, to: 0, offset: 0 }, { down: 0, up: 0 }, 'first').hidden, 0)
})

const { fileTree, matchesFilter } = require('../ui/review.js')

const names = (node, depth = 0) => [
  ...node.dirs.flatMap((d) => [' '.repeat(depth) + d.name + '/', ...names(d, depth + 1)]),
  ...node.files.map((f) => ' '.repeat(depth) + f.name),
]

test('files make a tree: folders first, then files, a folder with only one folder in it folded into it', () => {
  const tree = fileTree(['src/src/assets/icons/b.svg', 'src/src/assets/icons/a.svg', 'src/src/components/x.ts', 'readme.md', 'src/main.ts'])
  assert.deepEqual(names(tree), [
    'src/',
    ' src/',
    '  assets/icons/',
    '   a.svg',
    '   b.svg',
    '  components/',
    '   x.ts',
    ' main.ts',
    'readme.md',
  ])
  const icons = tree.dirs[0].dirs[0].dirs[0]
  assert.equal(icons.path, 'src/src/assets/icons')
  assert.deepEqual(icons.files.map((f) => f.path), ['src/src/assets/icons/a.svg', 'src/src/assets/icons/b.svg'])
})

test('the filter wants every word typed somewhere in the path, any case', () => {
  const path = 'path/to/some/file.js'
  for (const q of ['', 'path file', 'file.js', 'some file', 'some/file', 'FILE', ' file  path ']) {
    assert.ok(matchesFilter(path, q), q)
  }
  for (const q of ['other', 'path other', 'file/some']) {
    assert.ok(!matchesFilter(path, q), q)
  }
})

test("the Review tab is marked when the agent has updated the review since it was seen", () => {
  const { agentUpdated } = require('../ui/review.js')
  assert.equal(agentUpdated('', ''), false, 'nothing from the agent yet')
  assert.equal(agentUpdated('t1', null), true, 'first look: never seen here')
  assert.equal(agentUpdated('t1', 't1'), false, 'seen')
  assert.equal(agentUpdated('t2', 't1'), true, 'updated since')
  assert.equal(agentUpdated('t1', ''), true, 'the first update after looking')
})

test('the file tree counts each file’s comment threads, open or not', () => {
  const { threadsByFile } = require('../ui/review.js')
  const counts = threadsByFile([
    { id: 1, file: 'a.go', status: 'open' },
    { id: 2, file: 'a.go', status: 'open' },
    { id: 3, file: 'a.go', status: 'resolved' },
    { id: 4, file: 'b.go', status: 'dismissed' },
    { id: 5, status: 'open' },
  ])
  assert.deepEqual(Object.fromEntries(counts), { 'a.go': 3, 'b.go': 1 })
})

test('the comment arrows go to the thread after or before where the page is, round at the ends', () => {
  const { stepTo } = require('../ui/review.js')
  const tops = [-300, 40, 500, 900] // where each thread is; 40 is just under the bar
  assert.equal(stepTo(tops, 40, 'next'), 2, 'past the one at the bar')
  assert.equal(stepTo(tops, 40, 'prev'), 0)
  assert.equal(stepTo(tops, 41, 'next'), 2, 'a pixel off still counts as there')
  assert.equal(stepTo(tops, 1000, 'next'), 0, 'past the last: round to the first')
  assert.equal(stepTo(tops, -500, 'prev'), 3, 'before the first: round to the last')
  assert.equal(stepTo([], 0, 'next'), -1, 'none')
})

test('threads on lines the diff leaves out are placed in its gaps, opened enough to show them', () => {
  const { gaps, placeInGaps } = require('../ui/review.js')
  // Lines 1-9 hidden, a hunk at 10-11 (old 10-10: one line added), then 12-40 hidden.
  const hunks = [{ oldStart: 10, newStart: 10, lines: [
    { kind: 'context', old: 10, new: 10, text: 'ten' },
    { kind: 'add', new: 11, text: 'eleven' },
  ] }]
  const lines = Array.from({ length: 40 }, (_, i) => 'line ' + (i + 1))
  const g = gaps(hunks, lines.length)
  const placed = placeInGaps(g, [
    { id: 1, side: 'new', line: 30, endLine: 31, code: 'line 30\nline 31' },
    { id: 2, side: 'old', line: 5, code: 'something else' },  // old 5 is new 5, above the hunk
    { id: 3, side: 'new', line: 99 },                         // past the end of the file
  ], lines, hunks.length)
  assert.deepEqual(placed.after, { 31: [1], 5: [2] }, 'under the last line of each, in new numbers')
  assert.deepEqual([...placed.marked].sort((a, b) => a - b), [5, 30, 31])
  assert.deepEqual(placed.rest, [3])
  assert.deepEqual([...placed.outdated], [2], 'its line reads differently now')
  // The first gap opens upwards only, the last downwards only: each far
  // enough for its thread and 3 lines past it.
  assert.deepEqual(placed.reveal, { 0: { down: 0, up: 9 - (5 - 3) + 1 }, 1: { down: 31 + 3 - 12 + 1, up: 0 } })
})

test('a thread in a middle gap opens it from the nearer end', () => {
  const { gaps, placeInGaps } = require('../ui/review.js')
  const hunks = [
    { oldStart: 1, newStart: 1, lines: [{ kind: 'add', new: 1, text: 'a' }] },
    { oldStart: 100, newStart: 101, lines: [{ kind: 'add', new: 101, text: 'b' }] },
  ]
  const lines = Array.from({ length: 120 }, (_, i) => 'l' + (i + 1))
  const g = gaps(hunks, lines.length)
  // The middle gap is new lines 2-100.
  const near = (line) => placeInGaps(g, [{ id: 1, side: 'new', line }], lines, hunks.length).reveal[1]
  assert.deepEqual(near(10), { down: 10 + 3 - 2 + 1, up: 0 }, 'near the top')
  assert.deepEqual(near(95), { down: 0, up: 100 - (95 - 3) + 1 }, 'near the bottom')
})
