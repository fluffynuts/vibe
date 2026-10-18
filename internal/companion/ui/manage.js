// What the Profiles tab's ticks come to, apart from drawing them: which can
// be ticked, what "Select likely trash" ticks, and what a delete sends. The
// inventory is /api/manage's: profiles, each with its sandboxes, and the
// sandboxes vibe has no record of. Ticks are kept by ID.
(function (root) {
  'use strict'

  // pickable says whether row (a profile, a sandbox or an unknown sandbox)
  // can be ticked at all: one vibe keeps says why instead.
  function pickable(row) {
    return !row.kept
  }

  // likelyTrash is the IDs "Select likely trash" ticks: profiles that are
  // likely trash, and sandboxes that are, unless their profile is ticked
  // already (they go with it).
  function likelyTrash(inv) {
    const ids = new Set()
    for (const p of inv.profiles || []) {
      const whole = p.trash && pickable(p)
      if (whole) ids.add(p.id)
      for (const s of p.sandboxes || []) {
        if (!whole && s.trash && pickable(s)) ids.add(s.id)
      }
    }
    return ids
  }

  // coveredBy is the ticked profile a sandbox goes with, if any.
  function coveredBy(inv, ticked, sandboxId) {
    for (const p of inv.profiles || []) {
      if (ticked.has(p.id) && (p.sandboxes || []).some((s) => s.id === sandboxId)) return p
    }
    return null
  }

  // picked is what a delete sends: what's ticked and still there to be
  // picked, without the sandboxes a ticked profile takes with it.
  function picked(inv, ticked) {
    const ids = []
    for (const p of inv.profiles || []) {
      if (ticked.has(p.id) && pickable(p)) { ids.push(p.id); continue }
      for (const s of p.sandboxes || []) {
        if (ticked.has(s.id) && pickable(s)) ids.push(s.id)
      }
    }
    for (const u of inv.unknown || []) {
      if (ticked.has(u.id)) ids.push(u.id)
    }
    return ids
  }

  // summary says what's selected, in words, for beside the Delete button.
  function summary(inv, ticked) {
    let profiles = 0
    let sandboxes = 0
    for (const p of inv.profiles || []) {
      if (ticked.has(p.id) && pickable(p)) { profiles++; sandboxes += (p.sandboxes || []).length; continue }
      for (const s of p.sandboxes || []) if (ticked.has(s.id) && pickable(s)) sandboxes++
    }
    for (const u of inv.unknown || []) if (ticked.has(u.id)) sandboxes++
    const parts = []
    if (profiles) parts.push(profiles + (profiles === 1 ? ' profile' : ' profiles'))
    if (sandboxes) parts.push(sandboxes + (sandboxes === 1 ? ' sandbox' : ' sandboxes'))
    return parts.length ? parts.join(' and ') + ' selected' : 'nothing selected'
  }

  // words is query, lowercased, split into the words a row has to match.
  function words(query) {
    return String(query || '').toLowerCase().split(/\s+/).filter(Boolean)
  }

  // text is what a row is matched on: its names, folder, features and
  // flags, and "trash" when it's likely trash.
  function text(...parts) {
    return parts.flat().filter((p) => typeof p === 'string').join(' ').toLowerCase()
  }

  const matchesAll = (wanted, hay) => wanted.every((w) => hay.includes(w))

  // inState says whether a sandbox whose state is "running" or "stopped"
  // (or neither, when it's gone from sbx) is shown by the state filter:
  // "all", "running" or "stopped".
  const inState = (state, sandboxState) => !state || state === 'all' || state === sandboxState

  const stateOf = (s) => (s.flags || []).find((f) => f === 'running' || f === 'stopped')

  // filtered is inv narrowed to the sandboxes in state (see inState) that
  // match query: every word of it has to match, each anywhere in a
  // sandbox's own text or its profile's. A profile that matches by itself
  // keeps all its sandboxes in state, and shows with none only when every
  // state is. shown and total count sandboxes, with the ones vibe has no
  // record of.
  function filtered(inv, query, state) {
    const wanted = words(query)
    const everyState = inState(state, undefined)
    const profiles = inv.profiles || []
    const unknown = inv.unknown || []
    const total = profiles.reduce((n, p) => n + (p.sandboxes || []).length, 0) + unknown.length
    if (!wanted.length && everyState) return { profiles, unknown, shown: total, total }
    let shown = 0
    const keptProfiles = []
    for (const p of profiles) {
      const own = text(p.name, p.features, p.flags, p.trash ? 'trash' : '')
      const ofState = (p.sandboxes || []).filter((s) => inState(state, stateOf(s)))
      const sandboxes = matchesAll(wanted, own)
        ? ofState
        : ofState.filter((s) => matchesAll(wanted, own + ' ' + text(s.name, s.folder, s.flags, s.trash ? 'trash' : '')))
      if (sandboxes.length || (everyState && matchesAll(wanted, own))) {
        keptProfiles.push(Object.assign({}, p, { sandboxes }))
        shown += sandboxes.length
      }
    }
    const keptUnknown = unknown.filter((u) => inState(state, u.running ? 'running' : 'stopped') &&
      matchesAll(wanted, text(u.name, u.running ? 'running' : 'stopped')))
    shown += keptUnknown.length
    return { profiles: keptProfiles, unknown: keptUnknown, shown, total }
  }

  const api = { pickable, likelyTrash, coveredBy, picked, summary, filtered }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeManage = api
})(this)
