// The companion page. Everything that came from sbx — host names above all,
// which the sandbox itself chooses — goes into the page as text, never as
// markup: see el() and its use of textContent.
(function () {
  'use strict'

  // One companion tab per sandbox. The tab that is open holds a Web Lock for
  // as long as it lives; a tab that finds the lock taken tells the open one
  // (over a BroadcastChannel) to reload, then steps aside: it tries to close
  // itself, which browsers allow only for tabs a script opened, and failing
  // that says so. Raising the old tab is not something a page may do.
  const HANDOFF = 'vibe-companion-handoff'
  const RELOADED = 'vibe-companion-reloaded'

  function step_aside(channel) {
    channel.postMessage('reload')
    channel.close()
    window.close()
    const note = document.createElement('p')
    note.style.cssText = 'font: 16px system-ui, sans-serif; margin: 3em auto; max-width: 32em; text-align: center'
    note.textContent = 'The companion is already open in another tab, and has been reloaded. You can close this tab.'
    document.body.replaceChildren(note)
    document.title = 'Companion (already open)'
  }

  function start() {
    if (!navigator.locks || typeof BroadcastChannel === 'undefined') return boot()
    const channel = new BroadcastChannel(HANDOFF)
    channel.onmessage = (ev) => {
      if (ev.data !== 'reload') return
      try { sessionStorage.setItem(RELOADED, '1') } catch (e) { /* carry on */ }
      location.reload()
    }
    // After a reload we were asked for, the old page may not have let go
    // of the lock yet: wait for it rather than mistake ourselves for a newcomer.
    let reloaded = false
    try { reloaded = sessionStorage.getItem(RELOADED) === '1'; sessionStorage.removeItem(RELOADED) } catch (e) { /* carry on */ }
    const hold = () => { boot(); return new Promise(() => {}) } // never resolves: held until the tab goes
    if (reloaded) { navigator.locks.request(HANDOFF, hold); return }
    navigator.locks.request(HANDOFF, { ifAvailable: true }, (lock) => {
      if (!lock) { step_aside(channel); return }
      return hold()
    })
  }

  function boot() {

  const POLL_MS = 2000
  // How long the clipboard page gets to come up before the page says so and
  // offers to start it.
  const CLIPBOARD_PATIENCE_MS = 30000
  const TABS = ['conversation', 'log', 'rules', 'fs']
  // How often the conversation tab asks for what the agent has said since.
  const CONVO_POLL_MS = 700
  // The longest a tool's result is shown, in characters.
  const RESULT_SHOWN = 2000
  // The clipboard page, shown beside the conversation: its frame, waiting
  // notice and the controls that go with them.
  const CLIPBOARD_VIEWS = [
    { frame: 'convo-clipboard-frame', wait: 'convo-clipboard-wait', text: 'convo-clipboard-wait-text', help: 'convo-clipboard-wait-help', start: 'convo-clipboard-start', open: 'convo-clipboard-open' },
  ]

  const $ = (id) => document.getElementById(id)

  const store = {
    get(key) { try { return localStorage.getItem(key) } catch (e) { return null } },
    set(key, value) { try { localStorage.setItem(key, value) } catch (e) { /* private window: carry on */ } },
  }

  // The token rides in the URL's fragment, which the browser never sends
  // anywhere. Kept in localStorage too (per origin, so per sandbox), so a
  // tab opened without it still works.
  const token = (function () {
    const fromUrl = location.hash.slice(1)
    if (fromUrl) { store.set('vibe-companion-token', fromUrl); return fromUrl }
    return store.get('vibe-companion-token') || ''
  })()

  const state = {
    sandbox: '',
    clipboardUrl: '',
    clipboardReady: false,
    clipboardCanStart: false,
    clipboardWaitingSince: 0,
    clipboardStarting: false,
    conversation: false,
    tab: store.get('vibe-companion-tab') || 'log',
    filter: 'all',
    search: '',
    log: null,
    rules: null,
    fs: null,
    open: new Set(),
    failures: 0,
    timer: null,
  }

  // el builds an element. Children are strings (made into text nodes) or
  // nodes; there is deliberately no way to pass markup.
  function el(tag, props, ...children) {
    const node = document.createElement(tag)
    for (const [k, v] of Object.entries(props || {})) {
      if (k === 'class') node.className = v
      else if (k === 'text') node.textContent = v
      else if (k.startsWith('on')) node.addEventListener(k.slice(2), v)
      else if (v === true) node.setAttribute(k, '')
      else if (v !== false && v != null) node.setAttribute(k, v)
    }
    for (const c of children) {
      if (c == null || c === false) continue
      node.append(c instanceof Node ? c : document.createTextNode(String(c)))
    }
    return node
  }

  async function api(path, options) {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 15000)
    try {
      const headers = Object.assign({ 'X-Vibe-Token': token }, options && options.body ? { 'Content-Type': 'application/json' } : {})
      const res = await fetch(path, Object.assign({ headers, signal: controller.signal }, options || {}))
      let body = null
      try { body = await res.json() } catch (e) { /* not JSON */ }
      return { status: res.status, ok: res.ok, body: body || {} }
    } finally {
      clearTimeout(timeout)
    }
  }

  // --- the tab's icon -----------------------------------------------------

  // Each agent's icon, drawn small as SVG (32 units square): a nod to the
  // agent's own mark rather than a copy of it. An agent not here gets
  // vibe's hexagon.
  function rays(n, color) {
    let d = ''
    for (let i = 0; i < n; i++) {
      const a = (i * 2 * Math.PI) / n
      const r = i % 2 ? 11 : 14
      d += 'M16 16L' + (16 + r * Math.cos(a)).toFixed(1) + ' ' + (16 + r * Math.sin(a)).toFixed(1)
    }
    return '<path d="' + d + '" stroke="' + color + '" stroke-width="3.2" stroke-linecap="round"/>'
  }
  const AGENT_ICONS = {
    claude: rays(12, '#d97757'),
    codex: '<circle cx="16" cy="16" r="15" fill="#0d0d0d"/><g fill="none" stroke="#fff" stroke-width="2">' +
      '<ellipse cx="16" cy="16" rx="10" ry="4.5"/><ellipse cx="16" cy="16" rx="10" ry="4.5" transform="rotate(60 16 16)"/>' +
      '<ellipse cx="16" cy="16" rx="10" ry="4.5" transform="rotate(120 16 16)"/></g>',
    copilot: '<rect x="1" y="1" width="30" height="30" rx="7" fill="#24292f"/>' +
      '<path d="M5 15a11 9 0 0 1 22 0v6c0 3-5 6-11 6S5 24 5 21z" fill="#fff"/>' +
      '<g fill="#fff" stroke="#24292f" stroke-width="1.8"><rect x="6" y="8" width="9.5" height="8" rx="4"/><rect x="16.5" y="8" width="9.5" height="8" rx="4"/></g>' +
      '<rect x="12" y="19" width="2.2" height="4.5" rx="1.1" fill="#24292f"/><rect x="17.8" y="19" width="2.2" height="4.5" rx="1.1" fill="#24292f"/>',
    cursor: '<path d="M16 2l13 7.5L16 17 3 9.5z" fill="#d4d4d4"/><path d="M3 9.5L16 17v14L3 23.5z" fill="#8a8a8a"/>' +
      '<path d="M29 9.5L16 17v14l13-7.5z" fill="#3a3a3a"/><path d="M16 2l13 7.5v14L16 31 3 23.5v-14z" fill="none" stroke="#1a1a1a" stroke-width="1.2" stroke-linejoin="round"/>',
    devin: '<rect x="1" y="1" width="30" height="30" rx="7" fill="#0f766e"/>' +
      '<path d="M10 8h5a8 8 0 0 1 0 16h-5z" fill="none" stroke="#fff" stroke-width="3" stroke-linejoin="round"/>',
    'docker-agent': '<rect x="1" y="1" width="30" height="30" rx="7" fill="#1d63ed"/><g fill="#fff">' +
      '<rect x="5" y="16" width="5" height="4"/><rect x="11" y="16" width="5" height="4"/><rect x="17" y="16" width="5" height="4"/>' +
      '<rect x="11" y="11" width="5" height="4"/><rect x="17" y="11" width="5" height="4"/><rect x="17" y="6" width="5" height="4"/>' +
      '<path d="M4 21h22c2 0 2.5-2 2.5-3 0 5-5 9-13 9-6 0-9.5-3-11.5-6z"/></g>',
    droid: '<rect x="1" y="1" width="30" height="30" rx="7" fill="#1c1917"/>' +
      '<path d="M16 4v4" stroke="#f97316" stroke-width="2" stroke-linecap="round"/><circle cx="16" cy="4" r="2" fill="#f97316"/>' +
      '<rect x="6" y="9" width="20" height="16" rx="4" fill="#f97316"/>' +
      '<circle cx="12" cy="16" r="2.5" fill="#1c1917"/><circle cx="20" cy="16" r="2.5" fill="#1c1917"/>' +
      '<rect x="11" y="21" width="10" height="1.8" rx="0.9" fill="#1c1917"/>',
    gemini: '<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">' +
      '<stop offset="0" stop-color="#4285f4"/><stop offset=".6" stop-color="#9b72cb"/><stop offset="1" stop-color="#d96570"/></linearGradient></defs>' +
      '<path d="M16 1C16 9 23 16 31 16 23 16 16 23 16 31 16 23 9 16 1 16 9 16 16 9 16 1z" fill="url(#g)"/>',
    kiro: '<path d="M6 15a10 10 0 0 1 20 0v14l-3.3-2.5L19.3 29 16 26.5 12.7 29l-3.4-2.5L6 29z" fill="#7c3aed"/>' +
      '<ellipse cx="12.5" cy="15" rx="2" ry="3" fill="#fff"/><ellipse cx="19.5" cy="15" rx="2" ry="3" fill="#fff"/>',
    opencode: '<rect x="1" y="1" width="30" height="30" rx="5" fill="#0a0a0a"/>' +
      '<rect x="8" y="8" width="16" height="16" fill="none" stroke="#fff" stroke-width="3.5"/><rect x="12" y="12" width="8" height="8" fill="#6b6b6b"/>',
    shell: '<rect x="1" y="3" width="30" height="26" rx="5" fill="#1f2937"/>' +
      '<path d="M7 11l5 5-5 5" fill="none" stroke="#4ade80" stroke-width="2.8" stroke-linecap="round" stroke-linejoin="round"/>' +
      '<path d="M15 22h9" stroke="#4ade80" stroke-width="2.8" stroke-linecap="round"/>',
  }
  const VIBE_ICON = '<path d="M16 2l12.1 7v14L16 30 3.9 23V9z" fill="#a626a4"/>'

  // The dot on the icon: grey, pulsing, while the agent works; blue once it
  // has finished, until the tab is looked at again.
  const DOT = { working: ['#5f6368', '#b0b3b8'], done: ['#1d9bd8'] }
  const PULSE_MS = 1000 // a step each way, so a two-second pulse

  const favicon = { agent: '', dot: '', step: 0, timer: null, shown: '' }

  function drawFavicon() {
    let svg = AGENT_ICONS[favicon.agent] || VIBE_ICON
    const colors = DOT[favicon.dot]
    if (colors) {
      svg += '<circle cx="24" cy="24" r="7.5" fill="' + colors[favicon.step % colors.length] +
        '" stroke="#fff" stroke-width="2"/>'
    }
    const href = 'data:image/svg+xml,' + encodeURIComponent(
      '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">' + svg + '</svg>')
    if (href === favicon.shown) return
    favicon.shown = href
    $('favicon').href = href
  }

  function setFaviconAgent(agent) {
    if (agent === favicon.agent && favicon.shown) return
    favicon.agent = agent
    drawFavicon()
  }

  function setFaviconDot(dot) {
    if (dot === favicon.dot) return
    favicon.dot = dot
    favicon.step = 0
    clearInterval(favicon.timer)
    favicon.timer = null
    if (dot === 'working') {
      favicon.timer = setInterval(() => { favicon.step += 1; drawFavicon() }, PULSE_MS)
    }
    drawFavicon()
  }

  // Looking at the tab again is what clears a finished turn's blue dot.
  function seen() {
    if (favicon.dot === 'done' && document.visibilityState === 'visible' && document.hasFocus()) setFaviconDot('')
  }
  window.addEventListener('focus', seen)
  document.addEventListener('visibilitychange', seen)

  // --- the link to vibe -------------------------------------------------

  function setLink(up, text) {
    const link = $('link')
    link.classList.toggle('up', up)
    link.classList.toggle('down', !up)
    $('link-text').textContent = text
  }

  function banner(message) {
    const b = $('banner')
    b.hidden = !message
    b.textContent = message || ''
  }

  function toast(message, bad) {
    const t = $('toast')
    t.textContent = message
    t.className = 'toast' + (bad ? ' bad' : '')
    t.hidden = false
    clearTimeout(toast.timer)
    toast.timer = setTimeout(() => { t.hidden = true }, bad ? 8000 : 4000)
  }

  // reached is called with the outcome of every call to vibe: two failures
  // in a row mean vibe has gone (its session ended), and it says so; the
  // next success puts everything back.
  function reached(ok) {
    if (ok) {
      state.failures = 0
      setLink(true, 'connected')
      return
    }
    state.failures += 1
    if (state.failures >= 2) {
      setLink(false, 'vibe is not running for this sandbox — run vibe again, then this page reconnects')
    }
  }

  function problem(res) {
    if (res.status === 401) {
      banner("This page doesn't have the right token. Open the URL vibe printed (it ends in #…), or run vibe again.")
      return true
    }
    if (res.body && res.body.error) {
      banner(res.body.code === 'not-signed-in'
        ? "sbx isn't signed in to Docker, so there's nothing to show yet. Run 'sbx login' on this machine."
        : res.body.error)
      return true
    }
    return false
  }

  // --- reading ----------------------------------------------------------

  async function load(kind) {
    const path = { log: '/api/log', rules: '/api/rules', fs: '/api/fs-rules' }[kind]
    try {
      const res = await api(path)
      reached(true)
      if (res.ok) {
        banner('')
        state[kind] = res.body
        render(kind)
      } else {
        problem(res)
      }
    } catch (e) {
      reached(false)
    }
  }

  // The folder and git status, as the infobar in the sandbox shows them.
  function renderWhere(folder, git) {
    $('folder').textContent = folder
    $('folder-part').hidden = !folder
    const box = $('git')
    box.replaceChildren()
    $('git-part').hidden = !git
    if (!git) return
    const part = (cls, text) => {
      const span = document.createElement('span')
      span.className = cls
      span.textContent = text
      box.append(span)
    }
    part('branch', git.branch)
    if (git.staged) part('staged', '+' + git.staged)
    if (git.modified) part('modified', '~' + git.modified)
    if (git.untracked) part('untracked', '?' + git.untracked)
    if (git.conflicted) part('conflicted', '✘' + git.conflicted)
    if (git.ahead) part('ahead', '↑' + git.ahead)
    if (git.behind) part('behind', '↓' + git.behind)
    if (git.stashed) part('stashed', '#' + git.stashed)
  }

  async function loadState() {
    try {
      const res = await api('/api/state')
      reached(true)
      if (!res.ok) { problem(res); return }
      state.sandbox = res.body.sandbox || ''
      $('sandbox').textContent = state.sandbox
      renderWhere(res.body.folder || '', res.body.git || null)
      document.title = state.sandbox ? 'vibe · ' + state.sandbox : 'vibe companion'
      setFaviconAgent(res.body.agent || '')
      const url = res.body.clipboardUrl || ''
      const ready = !!(url && res.body.clipboardReady)
      state.clipboardCanStart = !!res.body.clipboardCanStart
      state.conversation = !!res.body.conversation
      $('tab-conversation').hidden = !state.conversation
      if (url !== state.clipboardUrl || ready !== state.clipboardReady) {
        if (url && !ready && (url !== state.clipboardUrl || state.clipboardReady)) {
          state.clipboardWaitingSince = Date.now()
        }
        state.clipboardUrl = url
        state.clipboardReady = ready
        for (const view of CLIPBOARD_VIEWS) {
          if (url) $(view.open).href = url
          // The frame only gets the page once vibe has seen it answer: a
          // frame on another origin can't be watched, and a browser's
          // "can't connect" page would otherwise sit there for good.
          if (ready) $(view.frame).src = url
          else $(view.frame).removeAttribute('src')
        }
      }
      renderClipboard()
      if (!state.conversation && state.tab === 'conversation') select('log')
    } catch (e) {
      reached(false)
    }
  }

  function renderClipboard() {
    const waiting = !!state.clipboardUrl && !state.clipboardReady
    const late = Date.now() - state.clipboardWaitingSince >= CLIPBOARD_PATIENCE_MS
    for (const view of CLIPBOARD_VIEWS) {
      $(view.frame).hidden = waiting || !state.clipboardUrl
      $(view.wait).hidden = !waiting && !!state.clipboardUrl
      if (!state.clipboardUrl) {
        // The sandbox has no clipboard page at all: say so beside the
        // conversation rather than wait for one.
        $(view.text).textContent = "This sandbox has no clipboard page, so there's nowhere to paste images to."
        $(view.help).hidden = true
        $(view.start).hidden = true
        continue
      }
      if (!waiting) continue
      $(view.text).textContent = state.clipboardStarting
        ? 'Starting the clipboard page in the sandbox…'
        : late ? "The clipboard page still isn't up." : 'Starting the clipboard page…'
      $(view.help).hidden = !late || state.clipboardStarting
      $(view.help).textContent = state.clipboardCanStart
        ? 'vibe can try starting it in the sandbox — that is safe to do even if it is already on its way.'
        : 'Check /tmp/clipboard.log and /tmp/clipboard-server.log in the sandbox.'
      $(view.start).hidden = !late || !state.clipboardCanStart
      $(view.start).disabled = state.clipboardStarting
    }
  }

  async function startClipboard() {
    if (state.clipboardStarting) return
    state.clipboardStarting = true
    renderClipboard()
    try {
      const res = await post('/api/clipboard/start', {})
      reached(true)
      if (res.ok) {
        toast('Ran: ' + (res.body.command || 'start-clipboard'))
      } else {
        toast((res.body && res.body.error) || 'Starting the clipboard page failed.', true)
      }
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    } finally {
      state.clipboardStarting = false
      // Give it a fresh wait before offering again.
      state.clipboardWaitingSince = Date.now()
      loadState()
    }
  }

  function tick() {
    if (document.visibilityState !== 'visible') return
    if (state.tab === 'conversation') { loadState(); return }
    loadState()
    load(state.tab)
  }

  // --- tabs -------------------------------------------------------------

  function select(tab) {
    if (!TABS.includes(tab)) tab = 'log'
    if (tab === 'conversation' && !state.conversation) tab = 'log'
    state.tab = tab
    store.set('vibe-companion-tab', tab)
    for (const t of TABS) {
      $('panel-' + t).hidden = t !== tab
      const button = $('tab-' + t)
      button.setAttribute('aria-selected', String(t === tab))
      button.tabIndex = t === tab ? 0 : -1
    }
    // What the agent said while the conversation was out of sight.
    if (tab === 'conversation' && convo.behind) {
      convo.behind = false
      $('convo-log').scrollTop = $('convo-log').scrollHeight
    }
    tick()
  }

  // --- the network log --------------------------------------------------

  function when(iso) {
    if (!iso) return ''
    const d = new Date(iso)
    if (isNaN(d)) return iso
    return d.toLocaleString([], { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }

  function statusCell(decision) {
    const word = decision === 'allowed' ? 'Allowed' : decision === 'blocked' ? 'Blocked' : decision === 'unblocked' ? 'Unblocked' : 'Unknown'
    return el('span', { class: 'status ' + (decision || 'unknown'), text: word })
  }

  function rawDetail(entry, extra, columns) {
    const dl = el('dl')
    for (const [k, v] of extra) {
      if (v) dl.append(el('dt', { text: k }), el('dd', { text: v }))
    }
    let pretty = ''
    try { pretty = JSON.stringify(entry.raw, null, 2) } catch (e) { /* leave empty */ }
    return el('tr', { class: 'detail' },
      el('td', { colspan: String(columns) }, dl, el('pre', { text: pretty })))
  }

  function renderLog() {
    const data = state.log
    if (!data) return
    const entries = data.entries || []
    const blocked = entries.filter((e) => e.decision === 'blocked').length
    const allowed = entries.filter((e) => e.decision === 'allowed').length
    const unblocked = entries.filter((e) => e.decision === 'unblocked').length
    const summary = $('log-summary')
    summary.textContent = ''
    summary.append(entries.length + ' total  ',
      el('span', { class: 'ok', text: '● ' + allowed + ' allowed' }), '  ',
      el('span', { class: 'bad', text: '● ' + blocked + ' blocked' }),
      unblocked ? '  ' : '',
      unblocked ? el('span', { class: 'warn', text: '● ' + unblocked + ' unblocked' }) : '')

    const needle = state.search.trim().toLowerCase()
    const shown = entries.filter((e) =>
      (state.filter === 'all' || e.decision === state.filter) &&
      (!needle || e.host.toLowerCase().includes(needle)))

    const body = $('log-table').tBodies[0]
    body.textContent = ''
    if (!shown.length) {
      body.append(el('tr', {}, el('td', { class: 'empty', colspan: '6',
        text: entries.length ? 'Nothing matches.' : 'No network access attempts yet.' })))
    }
    for (const e of shown) {
      const key = e.host
      const row = el('tr', { class: 'entry', tabindex: '0', 'aria-expanded': String(state.open.has(key)) },
        el('td', { class: 'when', text: when(e.lastSeen) }),
        el('td', { class: 'host', text: e.host }),
        el('td', { class: 'num', text: String(e.hits || '') }),
        el('td', {}, statusCell(e.decision)),
        el('td', { class: 'mono', text: e.rule || '' }),
        el('td', {}, actionFor(e)))
      const toggle = () => {
        if (state.open.has(key)) state.open.delete(key); else state.open.add(key)
        renderLog()
      }
      row.addEventListener('click', (ev) => { if (!ev.target.closest('button')) toggle() })
      row.addEventListener('keydown', (ev) => {
        if ((ev.key === 'Enter' || ev.key === ' ') && !ev.target.closest('button')) { ev.preventDefault(); toggle() }
      })
      body.append(row)
      if (state.open.has(key)) {
        body.append(rawDetail(e, [['Note', e.decision === 'unblocked'
          ? 'Blocked earlier, but a rule now allows it. Retry the request in the sandbox.' : ''], ['Reason', e.reason], ['Proxy', e.proxyType], ['Last seen', e.lastSeen]], 6))
      }
    }
    $('log-note').textContent = data.skipped
      ? data.skipped + ' entr' + (data.skipped === 1 ? 'y' : 'ies') + " from sbx had no host vibe could read, so aren't shown."
      : ''
  }

  function actionFor(entry) {
    if (entry.decision === 'blocked') {
      return el('button', { type: 'button', class: 'small', text: 'Allow',
        onclick: () => confirmChange('allow', entry.host) })
    }
    if (entry.decision === 'allowed' || entry.decision === 'unblocked') {
      return el('button', { type: 'button', class: 'small ghost', text: 'Block',
        onclick: () => confirmChange('deny', entry.host) })
    }
    return null
  }

  // --- rules ------------------------------------------------------------

  function renderRules(kind, tableId, noteId, withActions) {
    const data = state[kind]
    if (!data) return
    const rules = data.rules || []
    const body = $(tableId).tBodies[0]
    body.textContent = ''
    const columns = withActions ? 6 : 5
    if (!rules.length) {
      body.append(el('tr', {}, el('td', { class: 'empty', colspan: String(columns), text: 'No rules.' })))
    }
    rules.forEach((r, i) => {
      const key = kind + ':' + (r.id || i)
      const cells = [
        el('td', {}, statusCell(r.decision)),
        el('td', { class: 'mono', text: (r.resources || []).join('\n') }),
        el('td', { text: r.source || '' }),
        el('td', { text: r.scope || '' }),
        el('td', { text: withActions ? (r.protocol || '') : (r.status || '') }),
      ]
      if (withActions) {
        cells.push(el('td', {}, r.removable
          ? el('button', { type: 'button', class: 'small ghost', text: 'Remove',
              onclick: (ev) => { ev.stopPropagation(); confirmRemove(r) } })
          : null))
      }
      const row = el('tr', { class: 'entry', tabindex: '0' }, ...cells)
      const toggle = () => {
        if (state.open.has(key)) state.open.delete(key); else state.open.add(key)
        renderRules(kind, tableId, noteId, withActions)
      }
      row.addEventListener('click', (ev) => { if (!ev.target.closest('button')) toggle() })
      row.addEventListener('keydown', (ev) => {
        if ((ev.key === 'Enter' || ev.key === ' ') && !ev.target.closest('button')) { ev.preventDefault(); toggle() }
      })
      body.append(row)
      if (state.open.has(key)) {
        body.append(rawDetail(r, [['Rule', r.name], ['Id', r.id]], columns))
      }
    })
    const note = $(noteId)
    if (kind === 'fs') {
      note.textContent = "Read-only: sbx doesn't log filesystem access yet, so there are only the rules to show." +
        (data.skipped ? ' ' + data.skipped + " from sbx couldn't be read." : '')
    } else {
      note.textContent = data.skipped ? data.skipped + " rule(s) from sbx couldn't be read, so aren't shown." : ''
    }
  }

  function render(kind) {
    if (kind === 'log') renderLog()
    else if (kind === 'rules') renderRules('rules', 'rules-table', 'rules-note', true)
    else if (kind === 'fs') renderRules('fs', 'fs-table', 'fs-note', false)
  }

  // --- changing rules ---------------------------------------------------

  function confirmChange(decision, host) {
    const allow = decision === 'allow'
    confirmThen({
      title: (allow ? 'Allow ' : 'Block ') + host + '?',
      text: allow
        ? 'The sandbox will be able to reach this host. The rule applies to this sandbox only.'
        : 'The sandbox will be stopped from reaching this host. The rule applies to this sandbox only.',
      command: 'sbx policy ' + decision + ' network --sandbox ' + state.sandbox + ' ' + host,
      danger: !allow,
      run: () => post('/api/' + decision, { host }),
    })
  }

  function confirmRemove(rule) {
    confirmThen({
      title: 'Remove this rule?',
      text: (rule.resources || []).join(', ') + ' — a ' + (rule.decision || 'rule') + ' rule for this sandbox.',
      command: 'sbx policy rm network --sandbox ' + state.sandbox + ' --id ' + rule.id,
      danger: true,
      run: () => post('/api/remove', { id: rule.id }),
    })
  }

  async function post(path, payload) {
    const res = await api(path, { method: 'POST', body: JSON.stringify(payload) })
    return res
  }

  function confirmThen(opts) {
    const dialog = $('confirm')
    $('confirm-title').textContent = opts.title
    $('confirm-text').textContent = opts.text
    $('confirm-command').textContent = opts.command
    $('confirm-run').className = opts.danger ? 'danger' : ''
    dialog.returnValue = ''
    dialog.onclose = async () => {
      if (dialog.returnValue !== 'run') return
      try {
        const res = await opts.run()
        reached(true)
        if (res.ok) {
          toast('Done: ' + (res.body.command || opts.command))
        } else {
          toast((res.body && res.body.error) || 'That failed.', true)
        }
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
      }
      tick()
    }
    dialog.showModal()
  }

  // --- the conversation --------------------------------------------------

  const convo = { last: 0, running: false, polling: false, behind: false, lastPrompt: '', ticks: new Map() }

  function nearBottom(box) {
    return box.scrollHeight - box.scrollTop - box.clientHeight < 80
  }

  // stamp is when a message was said, in local time as 2026-10-12 14:03:
  // the same whatever the browser's locale, and not a date anyone can read
  // the wrong way round. at is 0 in conversations kept from before times
  // were recorded, which get none.
  function stamp(at) {
    if (!at) return null
    const d = new Date(at)
    const two = (n) => String(n).padStart(2, '0')
    const text = d.getFullYear() + '-' + two(d.getMonth() + 1) + '-' + two(d.getDate()) +
      ' ' + two(d.getHours()) + ':' + two(d.getMinutes())
    return el('time', { class: 'stamp', datetime: d.toISOString(), title: d.toLocaleString(), text })
  }

  // localLink is href as a link the page may offer: http(s) on this
  // machine's own loopback, where sbx publishes the sandbox's pages. The
  // agent's words never become a link anywhere else.
  function localLink(href) {
    let u
    try { u = new URL(href) } catch (e) { return null }
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return null
    if (u.username || u.password) return null
    if (u.hostname !== 'localhost' && u.hostname !== '127.0.0.1') return null
    return el('a', { href: u.href, target: '_blank', rel: 'noopener noreferrer', text: href })
  }

  // linked is text with the local addresses in it made into links, as nodes
  // (text stays text: there is no markup in it to read).
  const LOCAL_URL = /\bhttps?:\/\/(?:localhost|127\.0\.0\.1)(?::\d+)?(?![\w:-]|\.[\w-])(?:[/?#][^\s<>"'`]*)?/g
  function linked(text) {
    const nodes = []
    let at = 0
    for (const m of text.matchAll(LOCAL_URL)) {
      // Sentence punctuation after an address isn't part of it.
      const href = m[0].replace(/[.,;:!?)\]}'"]+$/, '')
      const link = localLink(href)
      if (!link) continue
      if (m.index > at) nodes.push(text.slice(at, m.index))
      nodes.push(link)
      at = m.index + href.length
    }
    if (at < text.length) nodes.push(text.slice(at))
    return nodes
  }

  function convoNote(cls, text, at) {
    const time = stamp(at)
    const words = cls === 'agent' ? el('span', {}, ...linked(text)) : el('span', { text })
    $('convo-log').append(time
      ? el('div', { class: 'msg ' + cls }, words, el('span', { class: 'foot' }, time))
      : el('div', { class: 'msg ' + cls }, words))
  }

  // reviewNote is a card for a link diffity has for the user, saying what
  // it is from its address: a tour, the changes against a ref, or the
  // uncommitted ones.
  function reviewNote(ev) {
    const link = localLink(ev.text || '')
    if (!link) return
    const u = new URL(link.href)
    const ref = u.searchParams.get('ref')
    const what = u.pathname.startsWith('/tour/') ? 'Diffity has a code tour for you:'
      : u.pathname.startsWith('/diff') ? (ref ? 'The changes against ' + ref + ' are ready to review in diffity:'
        : 'The uncommitted changes are ready to review in diffity:')
      : 'Diffity has something for you to look at:'
    $('convo-log').append(el('div', { class: 'msg review' },
      el('span', { text: what }), el('span', { class: 'review-link' }, link),
      el('span', { class: 'foot' }, stamp(ev.at))))
  }

  // The ticks on what the user said, as on a chat message: one when vibe has
  // it, two when the sandbox has it, two in blue once the agent has started
  // answering.
  const RECEIPT_TICKS = { sent: '✓', delivered: '✓✓', seen: '✓✓' }
  const RECEIPT_WORDS = { sent: 'Sent', delivered: 'Delivered', seen: 'The agent is answering' }

  function showReceipt(seq, receipt) {
    const ticks = convo.ticks.get(seq)
    if (!ticks || !RECEIPT_TICKS[receipt]) return
    ticks.textContent = RECEIPT_TICKS[receipt]
    ticks.className = 'ticks ' + receipt
    ticks.title = RECEIPT_WORDS[receipt]
    ticks.setAttribute('aria-label', RECEIPT_WORDS[receipt])
  }

  function userNote(ev) {
    const ticks = el('span', { class: 'ticks' })
    convo.ticks.set(ev.seq, ticks)
    $('convo-log').append(el('div', { class: 'msg user' }, el('span', { text: ev.text || '' }),
      el('span', { class: 'foot' }, stamp(ev.at), ticks)))
    showReceipt(ev.seq, ev.receipt || 'sent')
  }

  // What a tool was asked to do, in a few words: its input is the agent's
  // own, so only ever shown as text.
  function summarise(input) {
    if (!input || typeof input !== 'object') return ''
    for (const key of ['command', 'file_path', 'path', 'pattern', 'url', 'description', 'query']) {
      if (typeof input[key] === 'string' && input[key]) return input[key].split('\n')[0].slice(0, 160)
    }
    let json = ''
    try { json = JSON.stringify(input) } catch (e) { /* leave empty */ }
    return json.slice(0, 160)
  }

  function resultText(content) {
    if (typeof content === 'string') return content
    if (Array.isArray(content)) {
      return content.map((c) => (c && typeof c.text === 'string' ? c.text : '')).filter(Boolean).join('\n')
    }
    return ''
  }

  function showResult(block) {
    let text = resultText(block.content)
    if (!text) return
    if (text.length > RESULT_SHOWN) text = text.slice(0, RESULT_SHOWN) + '\n… (' + (text.length - RESULT_SHOWN) + ' more characters)'
    const log = $('convo-log')
    log.append(el('details', { class: 'tool-result' + (block.is_error ? ' bad' : '') },
      el('summary', { text: block.is_error ? 'tool error' : 'tool result' }),
      el('pre', { text })))
  }

  function showAgent(line, at) {
    if (!line || typeof line !== 'object') return
    const content = line.message && Array.isArray(line.message.content) ? line.message.content : []
    if (line.type === 'assistant') {
      for (const block of content) {
        if (block.type === 'text' && block.text) convoNote('agent', block.text, at)
        else if (block.type === 'tool_use') convoNote('tool', '⚙ ' + (block.name || 'tool') + '  ' + summarise(block.input))
      }
    } else if (line.type === 'user') {
      for (const block of content) if (block.type === 'tool_result') showResult(block)
    } else if (line.type === 'result') {
      if (line.is_error) {
        convoNote('error', typeof line.result === 'string' && line.result ? line.result : 'The agent reported an error.', at)
      }
      const bits = []
      if (typeof line.duration_ms === 'number') bits.push((line.duration_ms / 1000).toFixed(1) + 's')
      if (typeof line.total_cost_usd === 'number') bits.push('$' + line.total_cost_usd.toFixed(4))
      if (bits.length) convoNote('meta', bits.join(' · '))
    } else if (line.type === 'truncated') {
      convoNote('meta', '(that was too long to show)')
    }
  }

  function showEvent(ev) {
    if (ev.kind === 'reset') { $('convo-log').replaceChildren(); convo.ticks.clear() }
    else if (ev.kind === 'user') { convo.lastPrompt = ev.text || ''; userNote(ev) }
    else if (ev.kind === 'agent') showAgent(ev.agent, ev.at)
    else if (ev.kind === 'error') convoNote('error', ev.text || 'That failed.', ev.at)
    else if (ev.kind === 'review') reviewNote(ev)
    else if (ev.kind === 'done' && ev.text === 'stopped') convoNote('meta', 'Stopped.')
  }

  // running is the agent answering; background is it having answered, but
  // still waiting on something it left running (a review page, say). Then
  // the page goes on as if it were done: sending ends that work.
  function setConvoRunning(running, background) {
    if (running) setFaviconDot('working')
    else if (convo.running) setFaviconDot(document.visibilityState === 'visible' && document.hasFocus() ? '' : 'done')
    convo.running = running
    $('convo-send').disabled = running
    $('convo-stop').hidden = !running && !background
    $('convo-new').disabled = running
    $('convo-status').textContent = running ? 'The agent is working…'
      : background ? 'The agent has answered, and has something still running. Sending a prompt ends it.' : ''
  }

  async function pollConversation() {
    // Polled from other tabs, and with the page in the background too, so
    // the tab's icon can say when the agent is done.
    if (!state.conversation || convo.polling) return
    convo.polling = true
    try {
      const res = await api('/api/conversation?after=' + convo.last)
      reached(true)
      if (!res.ok) { problem(res); return }
      banner('')
      const log = $('convo-log')
      const stick = nearBottom(log)
      if (res.body.reset) { log.replaceChildren(); convo.ticks.clear() }
      for (const ev of res.body.events || []) showEvent(ev)
      if (res.body.receipt) showReceipt(res.body.receipt.seq, res.body.receipt.state)
      convo.last = res.body.last || 0
      setConvoRunning(!!res.body.running, !!res.body.background)
      if ((res.body.events || []).length) {
        if ($('panel-conversation').hidden) convo.behind = true
        else if (stick) log.scrollTop = log.scrollHeight
      }
    } catch (e) {
      reached(false)
    } finally {
      convo.polling = false
    }
  }

  // What is being typed is kept as it is typed, so a reload (or vibe being
  // started again, which reloads this page) puts it back.
  const DRAFT = 'vibe-companion-draft'
  const saveDraft = () => store.set(DRAFT, $('convo-input').value)

  async function sendPrompt() {
    const input = $('convo-input')
    const prompt = input.value.trim()
    if (!prompt || convo.running) return
    try {
      const res = await post('/api/conversation/send', { prompt })
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That could not be sent.', true); return }
      input.value = ''
      saveDraft()
      setConvoRunning(true)
      pollConversation()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  async function conversationCall(path) {
    try {
      const res = await post(path, {})
      reached(true)
      if (!res.ok) toast((res.body && res.body.error) || 'That failed.', true)
      pollConversation()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  // --- pasting into the clipboard from anywhere on the conversation tab ---

  function editable(node) {
    return !!(node && node.closest && node.closest('textarea, input, select, [contenteditable=""], [contenteditable="true"]'))
  }

  // The clipboard page is on another origin, so what was pasted here is
  // handed to it as a message. It takes them only from the page that
  // framed it.
  function toClipboardPage(files, text) {
    const frame = $('convo-clipboard-frame')
    if (!state.clipboardUrl || !state.clipboardReady || !frame.contentWindow) {
      toast("The clipboard page isn't up yet, so that wasn't kept.", true)
      return
    }
    let origin
    try { origin = new URL(state.clipboardUrl).origin } catch (e) { return }
    frame.contentWindow.postMessage({ type: 'vibe-clipboard-paste', files, text }, origin)
  }

  // When the conversation stacks the clipboard under itself the frame is wide
  // but short, so the clipboard page is asked to put its instructions beside
  // its list instead of above it.
  const stacked = window.matchMedia('(max-width: 900px)')
  function sendLayout() {
    const frame = $('convo-clipboard-frame')
    if (!state.clipboardUrl || !frame.contentWindow) return
    let origin
    try { origin = new URL(state.clipboardUrl).origin } catch (e) { return }
    frame.contentWindow.postMessage({ type: 'vibe-clipboard-layout', split: stacked.matches }, origin)
  }
  $('convo-clipboard-frame').addEventListener('load', sendLayout)
  stacked.addEventListener('change', sendLayout)

  // Files and images go to the clipboard; text goes into the prompt, unless
  // it was pasted where there is nothing to type into.
  function onPaste(ev) {
    if (state.tab !== 'conversation' || !ev.clipboardData) return
    // Some browsers (Linux ones especially) only list pasted images under
    // items, not files.
    let files = Array.from(ev.clipboardData.files || [])
    if (!files.length) {
      files = Array.from(ev.clipboardData.items || [])
        .filter((item) => item.kind === 'file')
        .map((item) => item.getAsFile())
        .filter(Boolean)
    }
    const text = ev.clipboardData.getData('text/plain')
    // Files win even when text rides along (a copied image often brings its
    // address or file name as text, which isn't what was meant).
    if (files.length) {
      ev.preventDefault()
      toClipboardPage(files, '')
    } else if (!files.length && text && !editable(ev.target)) {
      ev.preventDefault()
      toClipboardPage([], text)
    }
  }

  function setUpConversation() {
    $('convo-form').addEventListener('submit', (ev) => { ev.preventDefault(); sendPrompt() })
    $('convo-input').addEventListener('keydown', (ev) => {
      if (ev.isComposing) return
      if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); sendPrompt() }
      else if (ev.key === 'Escape' && convo.running) { ev.preventDefault(); conversationCall('/api/conversation/stop') }
      else if (ev.key === 'ArrowUp' && !ev.target.value && convo.lastPrompt) {
        ev.preventDefault()
        ev.target.value = convo.lastPrompt
        ev.target.setSelectionRange(convo.lastPrompt.length, convo.lastPrompt.length)
        saveDraft()
      }
    })
    $('convo-input').value = store.get(DRAFT) || ''
    $('convo-input').addEventListener('input', saveDraft)
    $('convo-stop').addEventListener('click', () => conversationCall('/api/conversation/stop'))
    $('convo-new').addEventListener('click', () => conversationCall('/api/conversation/new'))
    document.addEventListener('paste', onPaste)
    setInterval(pollConversation, CONVO_POLL_MS)
  }

  // --- wiring -----------------------------------------------------------

  for (const t of TABS) $('tab-' + t).addEventListener('click', () => select(t))
  $('tabs').addEventListener('keydown', (ev) => {
    if (ev.key !== 'ArrowRight' && ev.key !== 'ArrowLeft') return
    const visible = TABS.filter((t) => !$('tab-' + t).hidden)
    const at = visible.indexOf(state.tab)
    const next = visible[(at + (ev.key === 'ArrowRight' ? 1 : visible.length - 1)) % visible.length]
    select(next)
    $('tab-' + next).focus()
  })

  for (const button of $('log-filter').querySelectorAll('button')) {
    button.addEventListener('click', () => {
      state.filter = button.dataset.filter
      for (const b of $('log-filter').querySelectorAll('button')) {
        b.setAttribute('aria-pressed', String(b === button))
      }
      renderLog()
    })
  }
  $('log-search').addEventListener('input', (ev) => { state.search = ev.target.value; renderLog() })

  $('rule-form').addEventListener('submit', (ev) => {
    ev.preventDefault()
    const host = $('rule-host').value.trim()
    if (!host) return
    const decision = ev.submitter && ev.submitter.dataset.decision === 'deny' ? 'deny' : 'allow'
    confirmChange(decision, host)
  })

  for (const view of CLIPBOARD_VIEWS) $(view.start).addEventListener('click', startClipboard)

  setUpConversation()

  document.addEventListener('visibilitychange', tick)
  state.timer = setInterval(tick, POLL_MS)

  setLink(false, 'connecting…')
  loadState().then(() => select(state.tab))
}

  start()
})()
