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

  // inReadingOrder is files as { file, stop }: those the reading order has a
  // stop on first, in its order, then the rest as they came.
  function inReadingOrder(files, order) {
    const byPath = new Map(files.map((f) => [f.path, f]))
    const out = []
    const seen = new Set()
    for (const stop of order || []) {
      const f = byPath.get(stop.file)
      if (!f || seen.has(f.path)) continue
      seen.add(f.path)
      out.push({ file: f, stop })
    }
    for (const f of files) if (!seen.has(f.path)) out.push({ file: f, stop: null })
    return out
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

  const api = { placeThreads, inReadingOrder, sideBySide, gaps, gapView, GAP_STEP, fileTree, matchesFilter }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeReview = api
})(this)
