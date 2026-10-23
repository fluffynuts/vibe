// Tests for ui/markdown.js, run by `node --test` (see markdown_test.go, which
// runs them as part of `go test` when node is there).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { parse, parseInline } = require('../ui/markdown.js')

// plain is a block's inline content as text, marks included, for compact checks.
function plain(inline) {
  return inline.map((n) => {
    switch (n.type) {
      case 'text': return n.text
      case 'code': return '`' + n.text + '`'
      case 'br': return '\n'
      case 'strong': return '**' + plain(n.children) + '**'
      case 'em': return '*' + plain(n.children) + '*'
      case 'del': return '~~' + plain(n.children) + '~~'
      case 'link': return '[' + plain(n.children) + '](' + n.href + ')'
    }
    throw new Error('unknown ' + n.type)
  }).join('')
}

test('a table with alignment, inline code and an escaped pipe', () => {
  const [t] = parse('| Where | Now | Next |\n|:---|---:|:-:|\n| `sbx ls` | none \\| silent | **20 s** |\n| short |')
  assert.equal(t.type, 'table')
  assert.deepEqual(t.align, ['left', 'right', 'center'])
  assert.deepEqual(t.head.map(plain), ['Where', 'Now', 'Next'])
  assert.deepEqual(t.rows.map((r) => r.map(plain)), [['`sbx ls`', 'none | silent', '**20 s**'], ['short', '', '']])
})

test('a pipe inside inline code does not split a cell', () => {
  const [t] = parse('| a | b |\n|---|---|\n| `x | y` | z |')
  assert.deepEqual(t.rows[0].map(plain), ['`x | y`', 'z'])
})

test('a line with pipes but no rule under it is just text', () => {
  const [p] = parse('this | that\nmore')
  assert.equal(p.type, 'para')
})

test('headings, ATX and setext', () => {
  const blocks = parse('# One\n\n### Three ###\n\nUniversal instructions\n----------------------\n\nTitle\n=====')
  assert.deepEqual(blocks.map((b) => [b.type, b.level, plain(b.inline)]),
    [['heading', 1, 'One'], ['heading', 3, 'Three'], ['heading', 2, 'Universal instructions'], ['heading', 1, 'Title']])
})

test('a fenced code block keeps its text exactly, markup and all', () => {
  const [c] = parse('```go\nfunc a() { *b* }\n<script>x</script>\n```')
  assert.deepEqual(c, { type: 'code', lang: 'go', text: 'func a() { *b* }\n<script>x</script>' })
})

test('an unclosed fence runs to the end', () => {
  const [c] = parse('~~~\nstill code')
  assert.equal(c.text, 'still code')
})

test('nested lists, ordered and not, with a continuation line', () => {
  const [l] = parse('1. first\n   more of first\n2. second\n   - inner a\n   - inner b\n3. third')
  assert.equal(l.ordered, true)
  assert.equal(l.items.length, 3)
  assert.equal(plain(l.items[0][0].inline), 'first\nmore of first')
  const inner = l.items[1][1]
  assert.equal(inner.type, 'list')
  assert.equal(inner.ordered, false)
  assert.deepEqual(inner.items.map((it) => plain(it[0].inline)), ['inner a', 'inner b'])
})

test('a list keeps going across blank lines between items', () => {
  const blocks = parse('- a\n\n- b\n\nafter')
  assert.equal(blocks[0].items.length, 2)
  assert.equal(blocks[1].type, 'para')
})

test('an ordered list remembers where it starts', () => {
  assert.equal(parse('4. four\n5. five')[0].start, 4)
})

test('a quote, and a rule', () => {
  const blocks = parse('> quoted **bold**\n> more\n\n---')
  assert.equal(blocks[0].type, 'quote')
  assert.equal(blocks[0].source, 'quoted **bold**\nmore')
  assert.equal(plain(blocks[0].blocks[0].inline), 'quoted **bold**\nmore')
  assert.equal(blocks[1].type, 'hr')
})

test('emphasis, strong and strikethrough', () => {
  assert.equal(plain(parseInline('**bold** and *it* and _it_ and ~~gone~~')), '**bold** and *it* and *it* and ~~gone~~')
})

test('snake_case and lone stars are left alone', () => {
  assert.deepEqual(parseInline('a snake_case_name, 2 * 3 * 4'), [{ type: 'text', text: 'a snake_case_name, 2 * 3 * 4' }])
})

test('code spans hide markup and can hold backticks', () => {
  assert.deepEqual(parseInline('``a `b` *c*``'), [{ type: 'code', text: 'a `b` *c*' }])
})

test('links, written and bare, without the sentence punctuation after them', () => {
  assert.equal(plain(parseInline('see [the docs](https://example.com/x "title") or http://localhost:5391/diff.')),
    'see [the docs](https://example.com/x) or [http://localhost:5391/diff](http://localhost:5391/diff).')
})

test('escapes', () => {
  assert.equal(plain(parseInline('\\*not em\\* and \\`not code\\`')), '*not em* and `not code`')
})

test('anything that looks like HTML is only ever text', () => {
  const blocks = parse('<script>alert(1)</script>\n\n<b>hi</b> & **<i>x</i>**')
  assert.equal(plain(blocks[0].inline), '<script>alert(1)</script>')
  assert.equal(plain(blocks[1].inline), '<b>hi</b> & **<i>x</i>**')
})

test('nothing, or only blank lines, is no blocks', () => {
  assert.deepEqual(parse(''), [])
  assert.deepEqual(parse('\n  \n'), [])
  assert.deepEqual(parse(undefined), [])
})

// What the agent writes is read here, and could be written to make each
// opener search to the end afresh: the time grows with the text, not its
// square. (The bound is many times what it takes; the old way took 11s.)
test('emphasis and links that never close take time in proportion to the text', () => {
  for (const unit of ['*a ', '_a ', '**a ', '[a ']) {
    const text = unit.repeat(40000)
    const began = process.hrtime.bigint()
    parse(text)
    const ms = Number(process.hrtime.bigint() - began) / 1e6
    assert.ok(ms < 2000, `${JSON.stringify(unit)} × 40000 took ${ms.toFixed(0)}ms`)
  }
})
