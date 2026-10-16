// What the Review tab works out before drawing: which line of the diff each
// comment thread goes under, and the order to show the files in. Lines are
// known by "hunk:line" within their file (their indexes), so a thread's
// place doesn't depend on how the page draws it.
(function (root) {
  'use strict'

  // placeThreads places threads on files' lines:
  //   after[path]["h:l"]  — the threads to show after that line: the last
  //                         line of the diff they're on, by their side;
  //   lines[path]["h:l"]  — the threads a line is part of (to mark it);
  //   general             — threads on the whole review;
  //   offDiff[path]       — threads on a file in the diff, on lines it hasn't;
  //   elsewhere           — threads on files not in the diff at all;
  //   outdated            — threads whose lines read differently now from
  //                         when they were made (a Set).
  function placeThreads(files, threads) {
    const out = { after: {}, lines: {}, general: [], offDiff: {}, elsewhere: [], outdated: new Set() }
    const byPath = new Map(files.map((f) => [f.path, f]))
    for (const t of threads) {
      if (!t.file) { out.general.push(t.id); continue }
      const f = byPath.get(t.file)
      if (!f) { out.elsewhere.push(t.id); continue }
      const first = t.line
      const last = Math.max(t.endLine || t.line, t.line)
      const number = t.side === 'old' ? 'old' : 'new'
      let lastKey = null
      const text = []
      ;(f.hunks || []).forEach((h, hi) => h.lines.forEach((l, li) => {
        const n = l[number]
        if (!n || n < first || n > last) return
        const key = hi + ':' + li
        ;((out.lines[f.path] = out.lines[f.path] || {})[key] = out.lines[f.path][key] || []).push(t.id)
        lastKey = key
        text.push(l.text)
      }))
      // The lines it was made on read differently now: it may no longer
      // be about what's there. (Only told when they're all in the diff.)
      if (typeof t.code === 'string' && text.length === last - first + 1 && text.join('\n') !== t.code) out.outdated.add(t.id)
      if (lastKey) {
        ;((out.after[f.path] = out.after[f.path] || {})[lastKey] = out.after[f.path][lastKey] || []).push(t.id)
      } else {
        (out.offDiff[f.path] = out.offDiff[f.path] || []).push(t.id)
      }
    }
    return out
  }

  // inTreeOrder is files as { file, stop, step }, in the order the file
  // tree lists them (folders first, each by name), so the files, the tree
  // and the comment arrows all go the same way. stop is the file's place in
  // the reading order, if it has one, and step its number there (from 1,
  // counting the stops on files in the diff).
  function inTreeOrder(files, order) {
    const byPath = new Map(files.map((f) => [f.path, f]))
    const stops = new Map()
    for (const stop of order || []) {
      if (byPath.has(stop.file) && !stops.has(stop.file)) stops.set(stop.file, { stop, step: stops.size + 1 })
    }
    const paths = []
    const walk = (node) => {
      for (const d of node.dirs) walk(d)
      for (const f of node.files) paths.push(f.path)
    }
    walk(fileTree(files.map((f) => f.path)))
    return paths.map((path) => {
      const s = stops.get(path)
      return { file: byPath.get(path), stop: s ? s.stop : null, step: s ? s.step : 0 }
    })
  }

  // sideBySide lays out hunk's lines as rows of { left, right }: indexes
  // into its lines, or null for a blank. Context lines sit on both sides; a
  // run of deleted lines faces the run of added lines that follows it, the
  // first of each together, the longer run going on against blanks.
  function sideBySide(hunk) {
    const rows = []
    let dels = []
    let adds = []
    const flush = () => {
      for (let k = 0; k < Math.max(dels.length, adds.length); k++) {
        rows.push({ left: k < dels.length ? dels[k] : null, right: k < adds.length ? adds[k] : null })
      }
      dels = []
      adds = []
    }
    hunk.lines.forEach((l, i) => {
      if (l.kind === 'del') {
        // A deletion after additions starts a run of its own.
        if (adds.length) flush()
        dels.push(i)
      } else if (l.kind === 'add') {
        adds.push(i)
      } else {
        flush()
        rows.push({ left: i, right: i })
      }
    })
    flush()
    return rows
  }

  // GAP_STEP is how many hidden lines a click on a gap shows, as on GitHub.
  const GAP_STEP = 20

  // gaps are the stretches of a file's new side its diff leaves out: before
  // each hunk (index: the hunk's), and after the last (index: how many
  // hunks there are), as { index, from, to, offset }: new line numbers, the
  // old ones being offset fewer. to is null after the last hunk until the
  // file's length, total, is known.
  function gaps(hunks, total) {
    const out = []
    let newEnd = 0
    let oldEnd = 0
    hunks.forEach((h, i) => {
      out.push({ index: i, from: newEnd + 1, to: h.newStart - 1, offset: h.newStart - h.oldStart })
      const newCount = h.lines.filter((l) => l.kind !== 'del').length
      const oldCount = h.lines.filter((l) => l.kind !== 'add').length
      newEnd = h.newStart + newCount - 1
      oldEnd = h.oldStart + oldCount - 1
    })
    out.push({ index: hunks.length, from: newEnd + 1, to: total === null || total === undefined ? null : total, offset: newEnd - oldEnd })
    return out
  }

  // gapView is what of gap shows, given how far it has been opened from
  // each end (revealed: { down, up }, in lines): the new line numbers shown
  // at its top (down, carrying on from the hunk above) and its foot (up,
  // leading into the hunk below), how many are still hidden (null when not
  // yet known), and the buttons it offers. end is 'first' for the gap
  // before the first hunk, which opens upwards only, 'last' for the one
  // after the last, which opens downwards only, else ''.
  function gapView(gap, revealed, end) {
    const size = gap.to === null ? null : Math.max(0, gap.to - gap.from + 1)
    let down = revealed.down || 0
    let up = end === 'last' ? 0 : revealed.up || 0
    if (end === 'first') down = 0
    if (size !== null && down + up >= size) { down = size; up = 0 }
    const range = (from, n) => Array.from({ length: Math.max(0, n) }, (_, k) => from + k)
    const shownDown = range(gap.from, size === null ? down : Math.min(down, size))
    const shownUp = size === null ? [] : range(gap.to - up + 1, up)
    const hidden = size === null ? null : size - shownDown.length - shownUp.length
    let buttons = []
    if (hidden !== 0) {
      if (end === 'first') buttons = ['up']
      else if (end === 'last') buttons = ['down']
      else buttons = hidden <= GAP_STEP ? ['all'] : ['down', 'up']
    }
    return { down: shownDown, up: shownUp, hidden, buttons }
  }

  // fileTree is paths as a tree for the file browser: { name, path, dirs,
  // files } for a folder (the root's name and path are ""), { name, path }
  // for a file; folders before files, each in name order. A folder holding
  // just one folder and no files is folded into it ("assets/icons"), as
  // GitHub shows them.
  function fileTree(paths) {
    const root = { name: '', path: '', dirs: [], files: [] }
    for (const path of paths) {
      const parts = path.split('/')
      let node = root
      for (let i = 0; i < parts.length - 1; i++) {
        let next = node.dirs.find((d) => d.name === parts[i])
        if (!next) {
          next = { name: parts[i], path: parts.slice(0, i + 1).join('/'), dirs: [], files: [] }
          node.dirs.push(next)
        }
        node = next
      }
      node.files.push({ name: parts[parts.length - 1], path })
    }
    const tidy = (node) => {
      node.dirs = node.dirs.map((d) => {
        while (d.files.length === 0 && d.dirs.length === 1) {
          const only = d.dirs[0]
          d = { name: d.name + '/' + only.name, path: only.path, dirs: only.dirs, files: only.files }
        }
        return tidy(d)
      })
      node.dirs.sort((a, b) => a.name.localeCompare(b.name))
      node.files.sort((a, b) => a.name.localeCompare(b.name))
      return node
    }
    return tidy(root)
  }

  // matchesFilter says path has every word of query in it, any case: words
  // can match anywhere, in any order, and "some/file" matches as it reads.
  function matchesFilter(path, query) {
    const lower = path.toLowerCase()
    return String(query || '').toLowerCase().split(/\s+/).filter(Boolean).every((w) => lower.includes(w))
  }

  // agentUpdated says the agent has updated the review (last at at, "" if
  // never) since the user last saw it (seen: the at they saw, null when this
  // browser never has, so all of it is news). Times are compared as given,
  // not ordered: they only move on.
  function agentUpdated(at, seen) {
    return !!at && at !== seen
  }

  // threadsByFile is how many comment threads each file has, open or not,
  // for the tree: those on the whole review aren't on a file.
  function threadsByFile(threads) {
    const out = new Map()
    for (const t of threads) if (t.file) out.set(t.file, (out.get(t.file) || 0) + 1)
    return out
  }

  // stepTo is which of the threads at tops (in page order, where each now
  // is) the comment arrows go to from a page scrolled so line is where a
  // thread is shown (just under the bar): the next past it, or the one
  // before; round to the first after the last, and back. One within a few
  // pixels of line is the one there. -1 when there are none.
  function stepTo(tops, line, dir) {
    if (!tops.length) return -1
    const NEAR = 2
    if (dir === 'next') {
      const i = tops.findIndex((top) => top > line + NEAR)
      return i < 0 ? 0 : i
    }
    for (let i = tops.length - 1; i >= 0; i--) if (tops[i] < line - NEAR) return i
    return tops.length - 1
  }

  // GAP_CONTEXT is how many lines past a thread's lines are opened with
  // them, when they're in a gap.
  const GAP_CONTEXT = 3

  // placeInGaps places threads on lines the diff leaves out, in its gaps
  // (made knowing the file's length, from its lines; hunkCount hunks):
  //   after[n]  — the threads to show after new line n: their last line;
  //   marked    — the new lines they're on (a Set);
  //   reveal[i] — how far to open gap i ({ down, up }) to show each thread's
  //               lines and GAP_CONTEXT past them: from the nearer end, but
  //               the first gap only upwards and the last only downwards;
  //   outdated  — those whose lines read differently now (a Set);
  //   rest      — those in no gap: past the end of the file, say.
  // A context line is the same on both sides, so an old line n is new line
  // n + the gap's offset.
  function placeInGaps(fileGaps, threads, lines, hunkCount) {
    const out = { after: {}, marked: new Set(), reveal: {}, outdated: new Set(), rest: [] }
    for (const t of threads) {
      const last = Math.max(t.endLine || t.line, t.line)
      const g = fileGaps.find((g) => {
        const shift = t.side === 'old' ? g.offset : 0
        return g.to !== null && t.line + shift >= g.from && last + shift <= g.to
      })
      if (!g) { out.rest.push(t.id); continue }
      const shift = t.side === 'old' ? g.offset : 0
      const lo = t.line + shift
      const hi = last + shift
      for (let n = lo; n <= hi; n++) out.marked.add(n)
      ;(out.after[hi] = out.after[hi] || []).push(t.id)
      if (typeof t.code === 'string' && lines.slice(lo - 1, hi).join('\n') !== t.code) out.outdated.add(t.id)
      const size = g.to - g.from + 1
      const fromTop = Math.min(size, hi + GAP_CONTEXT - g.from + 1)
      const fromBottom = Math.min(size, g.to - (lo - GAP_CONTEXT) + 1)
      const down = g.index !== 0 && (g.index === hunkCount || fromTop <= fromBottom)
      const r = out.reveal[g.index] = out.reveal[g.index] || { down: 0, up: 0 }
      if (down) r.down = Math.max(r.down, fromTop)
      else r.up = Math.max(r.up, fromBottom)
    }
    return out
  }

  // positionOf is which of the threads at tops (in page order) the page is
  // at, with line where a thread shows (just under the bar): the last one
  // at it or scrolled past it, else the first. -1 when there are none.
  function positionOf(tops, line) {
    if (!tops.length) return -1
    let at = 0
    tops.forEach((top, i) => { if (top <= line + 2) at = i })
    return at
  }

  // currentOf is which of the files at tops (their sections' tops, in page
  // order) the page is in, with line just under the bar: the last one
  // reaching up to it; -1 above the first.
  function currentOf(tops, line) {
    let at = -1
    tops.forEach((top, i) => { if (top <= line + 2) at = i })
    return at
  }

  // diffSquares is the five squares GitHub shows of a file's change: split
  // between added and deleted by their share of the lines changed (each
  // there at all getting one), or, with fewer than five changed, one each
  // and the rest grey.
  function diffSquares(adds, dels) {
    const total = adds + dels
    let add = adds
    let del = dels
    if (total >= 5) {
      add = Math.round((5 * adds) / total)
      if (adds && !add) add = 1
      if (dels && add === 5) add = 4
      del = 5 - add
    }
    return [...Array(add).fill('add'), ...Array(del).fill('del'), ...Array(5 - add - del).fill('none')]
  }

  // inStepOrder is entries ({ file, ... }: things on the page, in its
  // order; file null for the whole review's) in the reading order: the
  // whole review's first, then those on files with a step (steps: path to
  // its number), by step, then the rest; each group as the page has them.
  function inStepOrder(entries, steps) {
    const rank = (e) => e.file === null ? 0 : steps.has(e.file) ? steps.get(e.file) : Infinity
    return entries.map((e, i) => ({ e, i })).sort((a, b) => (rank(a.e) - rank(b.e)) || (a.i - b.i)).map((x) => x.e)
  }

  // formatText is what a formatting button does to value, with start to
  // end selected: { from, to, insert } (value[from, to) becomes insert) and
  // what's selected after (selStart, selEnd, in the new text). Bold,
  // italic, code and links wrap the selection (or leave the cursor between
  // their marks); code over several lines and code blocks go on lines of
  // their own; quotes and lists take in each whole line selected.
  function formatText(kind, value, start, end) {
    const sel = value.slice(start, end)
    const wrap = (before, after) => ({ from: start, to: end, insert: before + sel + after,
      selStart: start + before.length, selEnd: start + before.length + sel.length })
    if (kind === 'code' && sel.includes('\n')) kind = 'codeblock'
    switch (kind) {
      case 'bold': return wrap('**', '**')
      case 'italic': return wrap('_', '_')
      case 'code': return wrap('`', '`')
      case 'link': {
        const insert = '[' + sel + '](url)'
        return sel
          ? { from: start, to: end, insert, selStart: start + sel.length + 3, selEnd: start + sel.length + 6 }
          : { from: start, to: end, insert, selStart: start + 1, selEnd: start + 1 }
      }
      case 'codeblock': {
        const before = (start > 0 && value[start - 1] !== '\n' ? '\n' : '') + '```\n'
        const after = '\n```' + (end < value.length && value[end] !== '\n' ? '\n' : '')
        return wrap(before, after)
      }
    }
    const from = value.lastIndexOf('\n', start - 1) + 1
    const nl = value.indexOf('\n', end)
    const to = nl < 0 ? value.length : nl
    const prefix = { quote: () => '> ', bullet: () => '- ', numbered: (i) => (i + 1) + '. ' }[kind]
    if (!prefix) throw new Error('no such formatting: ' + kind)
    const insert = value.slice(from, to).split('\n').map((line, i) => prefix(i) + line).join('\n')
    return start === end
      ? { from, to, insert, selStart: from + insert.length, selEnd: from + insert.length }
      : { from, to, insert, selStart: from, selEnd: from + insert.length }
  }

  const api = { formatText, inStepOrder, diffSquares, currentOf, positionOf, placeInGaps, placeThreads, inTreeOrder, sideBySide, gaps, gapView, GAP_STEP, fileTree, matchesFilter, agentUpdated, threadsByFile, stepTo }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeReview = api
})(this)
