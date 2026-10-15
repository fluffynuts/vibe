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

  const api = { placeThreads, inReadingOrder }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeReview = api
})(this)
