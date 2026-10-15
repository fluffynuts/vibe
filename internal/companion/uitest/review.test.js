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
