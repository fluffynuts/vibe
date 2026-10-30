// Tests for ui/emoji.js, run by `node --test` (see emoji_test.go, which runs them under go test).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const emoji = require('../ui/emoji.js')

test('a name has the emoji it stands for, whatever its case', () => {
  assert.equal(emoji.named('rocket'), '🚀')
  assert.equal(emoji.named('ROCKET'), '🚀')
  assert.equal(emoji.named('not_a_thing'), '')
  assert.equal(emoji.named(''), '')
})

test('the usual aliases find the same emoji as the name', () => {
  assert.equal(emoji.named('+1'), '👍')
  assert.equal(emoji.named('thumbsup'), '👍')
  assert.equal(emoji.named('-1'), '👎')
  assert.equal(emoji.named('tada'), '🎉')
  assert.equal(emoji.named('hand'), emoji.named('raised_hand'))
})

test('there are enough of them, and each has a name and a character', () => {
  assert.ok(emoji.count > 400, 'only ' + emoji.count)
  for (const e of emoji.find('')) assert.fail('an empty query listed ' + e.name)
  const all = emoji.find('_').concat(emoji.find('a'), emoji.find('e'))
  for (const e of all) assert.ok(e.name && e.char, JSON.stringify(e))
})

test('what starts with the query comes first, shortest first', () => {
  const found = emoji.find('smi').map((e) => e.name)
  assert.ok(found.length > 1)
  assert.equal(found[0], 'smile')
  const starts = found.filter((n) => n.startsWith('smi'))
  assert.deepEqual(found.slice(0, starts.length), starts, 'a name that only has it inside came before one that starts with it')
  for (let i = 1; i < starts.length; i++) assert.ok(starts[i - 1].length <= starts[i].length)
})

test('a name with the query inside it is found, after those that start with it', () => {
  const found = emoji.find('rock').map((e) => e.name)
  assert.equal(found[0], 'rocket')
  assert.ok(found.includes('shamrock'))
  assert.ok(found.indexOf('rocket') < found.indexOf('shamrock'))
})

test('one found by an alias says which', () => {
  const found = emoji.find('+1')
  assert.equal(found[0].name, 'thumbsup')
  assert.equal(found[0].alias, '+1')
  assert.equal(emoji.find('thumbsup')[0].alias, '')
})

test('a limit is kept to', () => {
  assert.equal(emoji.find('a', 5).length, 5)
})

test('an empty or unknown query finds nothing', () => {
  assert.deepEqual(emoji.find(''), [])
  assert.deepEqual(emoji.find('qqqqzzzz'), [])
})

test('a name being typed is found at the cursor, after a space or the start of the text', () => {
  assert.deepEqual(emoji.at(':roc', 4), { start: 0, query: 'roc' })
  assert.deepEqual(emoji.at('hello :roc', 10), { start: 6, query: 'roc' })
  assert.deepEqual(emoji.at('(:roc', 5), { start: 1, query: 'roc' })
  assert.deepEqual(emoji.at('a\n:roc', 6), { start: 2, query: 'roc' })
})

test('it is the part before the cursor that is the query', () => {
  assert.deepEqual(emoji.at('hi :rocket there', 7), { start: 3, query: 'roc' })
})

test('one letter is enough, and a bare colon is not', () => {
  assert.deepEqual(emoji.at(':r', 2), { start: 0, query: 'r' })
  assert.deepEqual(emoji.at('hi :D', 5), { start: 3, query: 'D' })
  assert.equal(emoji.at(':', 1), null)
})

test('+1 and -1 are names: a sign and digits are offered, though they have no letter', () => {
  assert.deepEqual(emoji.at(':+', 2), { start: 0, query: '+' })
  assert.deepEqual(emoji.at(':+1', 3), { start: 0, query: '+1' })
  assert.deepEqual(emoji.at('ok :-1', 6), { start: 3, query: '-1' })
  assert.equal(emoji.find('+1')[0].name, 'thumbsup')
  assert.equal(emoji.find('-1')[0].name, 'thumbsdown')
})

test('only digits, or signs with nothing meant by them, are not a name being typed', () => {
  assert.equal(emoji.at(':12', 3), null)
  assert.equal(emoji.at(':100', 4), null)
  assert.equal(emoji.at(':--', 3), null)
  assert.equal(emoji.at(':__', 3), null)
})

test('the smileys people type are not names: a mouth, a bracket and a nose end it', () => {
  assert.equal(emoji.at(':)', 2), null)
  assert.equal(emoji.at(':-)', 3), emoji.at(':-)', 3)) // the ) is not a name character: nothing is being typed
  assert.equal(emoji.at(':(', 2), null)
})

test('100 is an emoji, and typing :100: gives it', () => {
  assert.deepEqual(emoji.closing(':100:', 5), { start: 0, char: '💯' })
})

test('a time, a URL, a port and a ratio are not a name', () => {
  assert.equal(emoji.at('at 12:30', 8), null)
  assert.equal(emoji.at('see http://example.com', 22), null)
  assert.equal(emoji.at('http:roc', 8), null)
  assert.equal(emoji.at('localhost:8080', 14), null)
  assert.equal(emoji.at('a:b', 3), null)
  assert.equal(emoji.at('key:value', 9), null)
})

test('a space ends it', () => {
  assert.equal(emoji.at(':roc ket', 8), null)
  assert.equal(emoji.at(':roc ', 5), null)
})

test('inside a code span or a fence it is not a name', () => {
  assert.equal(emoji.at('use `:roc', 9), null)
  assert.equal(emoji.at('```\n:roc', 8), null)
  assert.deepEqual(emoji.at('use `x` then :roc', 17), { start: 13, query: 'roc' })
})

test('typing the closing colon of a complete name gives the character', () => {
  assert.deepEqual(emoji.closing(':rocket:', 8), { start: 0, char: '🚀' })
  assert.deepEqual(emoji.closing('go :tada:', 9), { start: 3, char: '🎉' })
  assert.deepEqual(emoji.closing('ok :+1:', 7), { start: 3, char: '👍' })
})

test('a name that is not one, or not at the start of a word, or in code, stays as it was typed', () => {
  assert.equal(emoji.closing(':nope:', 6), null)
  assert.equal(emoji.closing('12:rocket:', 10), null)
  assert.equal(emoji.closing('`:rocket:', 9), null)
  assert.equal(emoji.closing(':rocket', 7), null)
  assert.equal(emoji.closing('::', 2), null)
})

test('a time like 12:30: is not turned into anything', () => {
  assert.equal(emoji.closing('at 12:30:', 9), null)
})

// Where the list goes: above the line the colon is on, its left edge where the colon is, and kept inside the box.
test('the list sits just above the caret line, at the caret', () => {
  const at = emoji.place({ caretLeft: 100, caretTop: 120, caretHeight: 20, listWidth: 260, listHeight: 80, boxWidth: 600, roomAbove: 500 })
  assert.deepEqual(at, { left: 100, top: 120 - 80 - 4, below: false })
})

test('near the right edge the list is moved left to stay inside the box', () => {
  const at = emoji.place({ caretLeft: 560, caretTop: 40, caretHeight: 20, listWidth: 260, listHeight: 80, boxWidth: 600, roomAbove: 500 })
  assert.equal(at.left, 600 - 260)
  assert.equal(at.below, false)
})

test('it never starts left of the box, even when the box is narrower than the list', () => {
  const at = emoji.place({ caretLeft: 30, caretTop: 40, caretHeight: 20, listWidth: 260, listHeight: 80, boxWidth: 200, roomAbove: 500 })
  assert.equal(at.left, 0)
})

test('with no room above the caret line it opens below it', () => {
  const at = emoji.place({ caretLeft: 10, caretTop: 20, caretHeight: 18, listWidth: 260, listHeight: 80, boxWidth: 600, roomAbove: 30 })
  assert.deepEqual(at, { left: 10, top: 20 + 18 + 4, below: true })
})

test('room above that is exactly enough keeps it above', () => {
  const at = emoji.place({ caretLeft: 0, caretTop: 90, caretHeight: 18, listWidth: 100, listHeight: 80, boxWidth: 600, roomAbove: 84 })
  assert.equal(at.below, false)
  const less = emoji.place({ caretLeft: 0, caretTop: 90, caretHeight: 18, listWidth: 100, listHeight: 80, boxWidth: 600, roomAbove: 83 })
  assert.equal(less.below, true)
})

test('nothing measured (no layout) is placed at the start rather than at NaN', () => {
  const at = emoji.place({ caretLeft: NaN, caretTop: NaN, caretHeight: NaN, listWidth: 0, listHeight: 0, boxWidth: 0, roomAbove: NaN })
  assert.ok(Number.isFinite(at.left) && Number.isFinite(at.top), JSON.stringify(at))
})

// A code span doesn't run across a blank line, so one stray backtick costs the paragraph it is in, not the message.
test('a stray backtick switches the picker off for its paragraph and no further', () => {
  const text = 'the `oops\n\nthen :roc'
  assert.deepEqual(emoji.at(text, text.length), { start: text.indexOf(':'), query: 'roc' })
  const same = 'the `oops :roc'
  assert.equal(emoji.at(same, same.length), null, 'inside the paragraph with the open backtick')
})

test('an open fence switches it off until it is closed, blank lines or not', () => {
  const open = '```js\nconst a = 1\n\n:roc'
  assert.equal(emoji.at(open, open.length), null)
  const closed = '```js\nx\n```\n\n:roc'
  assert.deepEqual(emoji.at(closed, closed.length), { start: closed.lastIndexOf(':'), query: 'roc' })
})

test('a closed span earlier, in the same paragraph, leaves the rest of it free', () => {
  const text = 'use `x` and `y`, then :roc'
  assert.deepEqual(emoji.at(text, text.length), { start: text.lastIndexOf(':'), query: 'roc' })
  assert.equal(emoji.closing('a `b :tada:', 11), null)
  assert.deepEqual(emoji.closing('a `b`\n\n:tada:', 13), { start: 7, char: '🎉' })
})

// The table is typed by hand: its shape is checked, so a typo is found here and not by whoever types the name.
test('every name has the shape of one, every character is an emoji and not letters or spaces, and no alias is another name', () => {
  const names = new Set()
  const aliases = new Map()
  for (const letter of 'abcdefghijklmnopqrstuvwxyz0123456789_+-'.split('')) {
    for (const e of emoji.find(letter)) names.add(e.name + '\u0000' + e.char)
  }
  const entries = [...names].map((s) => s.split('\u0000'))
  assert.ok(entries.length >= emoji.count, `only ${entries.length} found by letter, of ${emoji.count}`)
  const seen = new Set()
  for (const [name, char] of entries) {
    assert.match(name, /^[a-z0-9_+-]+$/, `the name ${JSON.stringify(name)}`)
    assert.ok(!/[A-Za-z\s]/.test(char), `${name}'s character ${JSON.stringify(char)} has letters or spaces in it`)
    assert.ok(char.length > 0, name + ' has no character')
    assert.ok(!seen.has(name), 'the name ' + name + ' is in the table twice')
    seen.add(name)
  }
  for (const e of emoji.find('')) assert.fail('listed ' + e.name)
  // An alias that is another entry's name would only ever be found as that name: one of them is a typo.
  for (const name of seen) {
    for (const e of emoji.find(name)) if (e.alias === name && seen.has(name)) aliases.set(name, e.name)
  }
  assert.deepEqual([...aliases], [], 'aliases that are also names')
})

// Emoticons: a few, converted when the word is finished. Every one is a way for ordinary text to change, so they start a word,
// end it, and are not in code.
test('the emoticons stand for their emoji', () => {
  for (const [emoticon, char] of [[':D', '😀'], [':)', '🙂'], ['(:', '🙂'], [':(', '🙁'], [':/', '😕'], [':P', '😛'], [';)', '😉'], [":'(", '😢'], ['<3', '❤️']]) {
    const text = 'ok ' + emoticon
    assert.deepEqual(emoji.emoticonBefore(text, text.length), { start: 3, char }, emoticon)
  }
})

test('letters in an emoticon may be either case', () => {
  assert.equal(emoji.emoticonBefore('ok :d', 5).char, '😀')
  assert.equal(emoji.emoticonBefore('ok :p', 5).char, '😛')
})

test('one at the start of the text, or after a space or a newline, is one', () => {
  assert.equal(emoji.emoticonBefore(':)', 2).char, '🙂')
  assert.equal(emoji.emoticonBefore('a\n:)', 4).char, '🙂')
  assert.equal(emoji.emoticonBefore('a\t:)', 4).char, '🙂')
})

test('what is not at the start of a word is not an emoticon: URLs, paths, times, keys', () => {
  for (const text of ['http://x.com', 'see http:/', 'C:/Users', 'a:/b', 'Re:D', 'key:D', '3:)', '12:(', 'x<3', 'a;)', 'foo:P']) {
    assert.equal(emoji.emoticonBefore(text, text.length), null, text)
  }
})

test('one is only the end of the text up to where it is asked: the earlier ones are left', () => {
  const text = 'a :) b :D'
  assert.equal(emoji.emoticonBefore(text, 4).char, '🙂')
  assert.equal(emoji.emoticonBefore(text, text.length).char, '😀')
  assert.equal(emoji.emoticonBefore(text, 6), null)
})

test('in code, an emoticon is not one', () => {
  assert.equal(emoji.emoticonBefore('use `a :)', 9), null)
  assert.equal(emoji.emoticonBefore('```\nx :D', 8), null)
  assert.equal(emoji.emoticonBefore(':(){ :|:& };:', 3), null, 'the fork bomb: :( is followed by ), and is at the start, but ")" is no end of a word')
})

test('a long one is taken in preference to a short one it ends with', () => {
  assert.equal(emoji.emoticonBefore("ok :'(", 6).char, '😢')
})

test('(: is an emoticon at the end of a word, and still the start of a name when letters follow', () => {
  assert.equal(emoji.emoticonBefore('go (:', 5).char, '🙂')
  assert.deepEqual(emoji.at('go (:roc', 8), { start: 4, query: 'roc' })
})

test('typing :D or :P or :O, the emoticon is the first thing offered', () => {
  const d = emoji.find('D')[0]
  assert.equal(d.name, 'grinning')
  assert.equal(d.emoticon, ':D')
  assert.equal(emoji.find('p')[0].char, '😛')
  assert.equal(emoji.find('O')[0].char, '😮')
  assert.equal(emoji.find('d', 8).filter((e) => e.name === 'grinning').length, 1, 'listed twice')
  assert.equal(emoji.find('ro')[0].emoticon, undefined, 'a name that is not an emoticon got one')
})
