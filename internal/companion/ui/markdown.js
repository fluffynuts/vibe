// The Markdown the agent writes, read into a plain structure the page then
// builds elements from. Nothing here makes HTML: text stays text, so what
// the agent says can't become markup, whatever it contains. It reads the
// Markdown agents write — GitHub's flavour, as far as chat needs it —
// rather than every corner of CommonMark.
//
// parse(text) gives a list of blocks:
//   { type: 'para', inline }            { type: 'heading', level, inline }
//   { type: 'code', lang, text }        { type: 'hr' }
//   { type: 'quote', blocks, source }   (source: the quote's own Markdown, with the > marks taken off)
//   { type: 'list', ordered, start, items: [blocks, ...] }
//   { type: 'table', align: ['left'|'center'|'right'|null, ...], head: [inline, ...], rows: [[inline, ...], ...] }
// where inline is a list of:
//   { type: 'text', text }  { type: 'code', text }  { type: 'br' }
//   { type: 'strong', children }  { type: 'em', children }  { type: 'del', children }
//   { type: 'link', href, children }
(function (root) {
  'use strict'

  const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([^`\s]*)[^`]*$/
  const HEADING = /^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$/
  const HR = /^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$/
  const QUOTE = /^ {0,3}> ?(.*)$/
  const ITEM = /^( *)([-*+]|(\d{1,9})[.)])(?:[ \t]+(.*))?$/
  const SETEXT_1 = /^ {0,3}=+[ \t]*$/
  const SETEXT_2 = /^ {0,3}-+[ \t]*$/
  const TABLE_RULE = /^ {0,3}\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/

  const blank = (line) => /^\s*$/.test(line)

  // cells splits a table row on its unescaped pipes, outer ones aside.
  function cells(line) {
    let s = line.trim()
    if (s.startsWith('|')) s = s.slice(1)
    if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1)
    const out = []
    let cur = ''
    let inCode = false
    for (let i = 0; i < s.length; i++) {
      const c = s[i]
      if (c === '\\' && s[i + 1] === '|') { cur += '|'; i++; continue }
      if (c === '`') inCode = !inCode
      if (c === '|' && !inCode) { out.push(cur.trim()); cur = ''; continue }
      cur += c
    }
    out.push(cur.trim())
    return out
  }

  function alignOf(cell) {
    const left = cell.startsWith(':')
    const right = cell.endsWith(':')
    return left && right ? 'center' : right ? 'right' : left ? 'left' : null
  }

  // startsBlock says line opens a block that ends a paragraph before it.
  function startsBlock(line) {
    return FENCE.test(line) || HEADING.test(line) || HR.test(line) || QUOTE.test(line) || ITEM.test(line)
  }

  function parse(text) {
    const lines = String(text || '').replace(/\r\n?/g, '\n').replace(/\t/g, '    ').split('\n')
    return parseLines(lines)
  }

  function parseLines(lines) {
    const blocks = []
    let i = 0
    while (i < lines.length) {
      const line = lines[i]
      if (blank(line)) { i++; continue }

      let m = FENCE.exec(line)
      if (m) {
        const fence = m[1]
        const body = []
        i++
        while (i < lines.length && !(lines[i].trim().startsWith(fence[0].repeat(fence.length)) &&
            /^\s*[`~]+\s*$/.test(lines[i]) && lines[i].trim()[0] === fence[0])) {
          body.push(lines[i])
          i++
        }
        i++ // the closing fence, if there was one
        blocks.push({ type: 'code', lang: m[2] || '', text: body.join('\n') })
        continue
      }

      m = HEADING.exec(line)
      if (m) {
        blocks.push({ type: 'heading', level: m[1].length, inline: parseInline(m[2] || '') })
        i++
        continue
      }

      if (HR.test(line)) { blocks.push({ type: 'hr' }); i++; continue }

      if (QUOTE.test(line)) {
        const body = []
        while (i < lines.length && !blank(lines[i])) {
          const q = QUOTE.exec(lines[i])
          body.push(q ? q[1] : lines[i])
          i++
        }
        blocks.push({ type: 'quote', blocks: parseLines(body), source: body.join('\n') })
        continue
      }

      if (ITEM.test(line)) {
        const list = readList(lines, i)
        blocks.push(list.block)
        i = list.next
        continue
      }

      if (line.includes('|') && i + 1 < lines.length && TABLE_RULE.test(lines[i + 1]) && lines[i + 1].includes('-')) {
        const head = cells(line)
        const rule = cells(lines[i + 1])
        if (rule.length === head.length) {
          const rows = []
          i += 2
          while (i < lines.length && !blank(lines[i]) && lines[i].includes('|')) {
            const row = cells(lines[i])
            while (row.length < head.length) row.push('')
            rows.push(row.slice(0, head.length).map(parseInline))
            i++
          }
          blocks.push({ type: 'table', align: rule.map(alignOf), head: head.map(parseInline), rows })
          continue
        }
      }

      // A paragraph: lines up to a blank one or the start of another block,
      // or a setext underline, which makes it a heading.
      const para = [line.trim()]
      i++
      let level = 0
      while (i < lines.length && !blank(lines[i])) {
        if (SETEXT_1.test(lines[i])) { level = 1; i++; break }
        if (SETEXT_2.test(lines[i])) { level = 2; i++; break }
        if (startsBlock(lines[i])) break
        para.push(lines[i].trim())
        i++
      }
      const inline = parseInline(para.join('\n'))
      blocks.push(level ? { type: 'heading', level, inline } : { type: 'para', inline })
    }
    return blocks
  }

  // readList reads the list starting at lines[start]: its items, each the
  // lines indented under it, read as blocks of their own.
  function readList(lines, start) {
    const first = ITEM.exec(lines[start])
    const indent = first[1].length
    const ordered = first[3] !== undefined
    const block = { type: 'list', ordered, start: ordered ? Number(first[3]) : 1, items: [] }
    let i = start
    while (i < lines.length) {
      const m = ITEM.exec(lines[i])
      if (!m || m[1].length !== indent || (m[3] !== undefined) !== ordered) break
      const contentIndent = m[1].length + m[2].length + 1
      const body = [m[4] || '']
      i++
      while (i < lines.length) {
        const next = lines[i]
        if (blank(next)) {
          // A blank line ends the item unless what follows is still indented under it.
          let j = i + 1
          while (j < lines.length && blank(lines[j])) j++
          if (j < lines.length && lines[j].search(/\S/) >= contentIndent) {
            for (; i < j; i++) body.push('')
            continue
          }
          break
        }
        const nextIndent = next.search(/\S/)
        const sibling = ITEM.exec(next)
        if (sibling && sibling[1].length <= indent) break
        if (nextIndent >= contentIndent || (nextIndent > indent && sibling)) {
          body.push(next.slice(Math.min(nextIndent, contentIndent)))
        } else if (!startsBlock(next)) {
          body.push(next.trim()) // a lazy continuation of the item's text
        } else {
          break
        }
        i++
      }
      block.items.push(parseLines(body))
      // Blank lines between items keep the list going.
      let j = i
      while (j < lines.length && blank(lines[j])) j++
      if (j < lines.length && j !== i) {
        const m2 = ITEM.exec(lines[j])
        if (m2 && m2[1].length === indent && (m2[3] !== undefined) === ordered) { i = j; continue }
      }
    }
    return { block, next: i }
  }

  const PUNCT = /[!-/:-@[-`{-~]/
  const URL_AT = /^https?:\/\/[^\s<>"'`]+/

  // parseInline reads one line's (or paragraph's) worth of inline Markdown.
  function parseInline(text) {
    const out = []
    let buf = ''
    const flush = () => { if (buf) { out.push({ type: 'text', text: buf }); buf = '' } }
    const s = String(text)
    // Each opener searched to the end afresh would take time growing with
    // the square of the text: what one search learns is kept for the rest.
    const closers = { '*': new Map(), _: new Map() }
    let brackets = null
    let i = 0
    while (i < s.length) {
      const c = s[i]
      if (c === '\\' && i + 1 < s.length && PUNCT.test(s[i + 1])) { buf += s[i + 1]; i += 2; continue }
      if (c === '\n') { flush(); out.push({ type: 'br' }); i++; continue }
      if (c === '`') {
        let n = 1
        while (s[i + n] === '`') n++
        const fence = '`'.repeat(n)
        const end = s.indexOf(fence, i + n)
        if (end > 0) {
          flush()
          let code = s.slice(i + n, end).replace(/\n/g, ' ')
          if (code.length > 1 && code.startsWith(' ') && code.endsWith(' ')) code = code.slice(1, -1)
          out.push({ type: 'code', text: code })
          i = end + n
          continue
        }
        buf += fence
        i += n
        continue
      }
      if (c === '[') {
        if (!brackets) brackets = bracketPairs(s)
        const close = brackets.has(i) ? brackets.get(i) : -1
        if (close > 0 && s[close + 1] === '(') {
          const end = s.indexOf(')', close + 2)
          if (end > 0) {
            const href = s.slice(close + 2, end).trim().split(/\s+/)[0].replace(/^<|>$/g, '')
            flush()
            out.push({ type: 'link', href, children: parseInline(s.slice(i + 1, close)) })
            i = end + 1
            continue
          }
        }
      }
      if ((c === '*' || c === '_' || c === '~') && s[i + 1] === c) {
        const end = s.indexOf(c + c, i + 2)
        if (end > i + 2 && !/\s/.test(s[i + 2]) && !/\s/.test(s[end - 1]) && (c !== '_' || wordEdge(s, i, end + 2))) {
          flush()
          out.push({ type: c === '~' ? 'del' : 'strong', children: parseInline(s.slice(i + 2, end)) })
          i = end + 2
          continue
        }
      }
      if ((c === '*' || c === '_') && s[i + 1] !== c && i + 1 < s.length && !/\s/.test(s[i + 1])) {
        const end = emCloser(s, c, i + 1, closers[c])
        if (end > i + 1 && (c !== '_' || wordEdge(s, i, end + 1))) {
          flush()
          out.push({ type: 'em', children: parseInline(s.slice(i + 1, end)) })
          i = end + 1
          continue
        }
      }
      if (c === 'h' && (i === 0 || !/[\w/]/.test(s[i - 1]))) {
        const m = URL_AT.exec(s.slice(i))
        if (m) {
          // Sentence punctuation after an address isn't part of it.
          const href = m[0].replace(/[.,;:!?)\]}'"]+$/, '')
          flush()
          out.push({ type: 'link', href, children: [{ type: 'text', text: href }] })
          i += href.length
          continue
        }
      }
      buf += c
      i++
    }
    flush()
    return out
  }

  // wordEdge says the _ markers at open and just before close sit at word
  // edges, so snake_case_names aren't read as emphasis.
  function wordEdge(s, open, after) {
    return (open === 0 || !/\w/.test(s[open - 1])) && (after >= s.length || !/\w/.test(s[after]))
  }

  // emCloser is where emphasis opened with c just before from closes: the
  // first c from there that's neither half of a pair (strong's) nor after
  // a space, or -1. Where the search went from each c it passed is the same
  // whichever opener it's for, so it's kept in memo for the next.
  function emCloser(s, c, from, memo) {
    const passed = []
    let end = s.indexOf(c, from)
    while (end > 0 && !memo.has(end) && (s[end + 1] === c || /\s/.test(s[end - 1]))) {
      passed.push(end)
      end = s.indexOf(c, end + (s[end + 1] === c ? 2 : 1))
    }
    if (end > 0 && memo.has(end)) end = memo.get(end)
    for (const p of passed) memo.set(p, end)
    return end
  }

  // bracketPairs is where each [ in s closes, as its index: brackets nest,
  // and a backslash escapes the character after it.
  function bracketPairs(s) {
    const pairs = new Map()
    const open = []
    for (let i = 0; i < s.length; i++) {
      if (s[i] === '\\') { i++; continue }
      if (s[i] === '[') open.push(i)
      else if (s[i] === ']' && open.length) pairs.set(open.pop(), i)
    }
    return pairs
  }

  const api = { parse, parseInline }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeMarkdown = api
})(this)
