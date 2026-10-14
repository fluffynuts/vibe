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

  // summary says what's ticked, in words, for beside the Delete button.
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
    return parts.length ? parts.join(' and ') + ' ticked' : 'nothing ticked'
  }

  const api = { pickable, likelyTrash, coveredBy, picked, summary }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeManage = api
})(this)
