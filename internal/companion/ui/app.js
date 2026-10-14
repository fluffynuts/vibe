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
  const TABS = ['conversation', 'log', 'rules', 'fs', 'admin', 'manage']
  // The tabs about a sandbox: a page served for none ('vibe --manage') has
  // only the Profiles tab.
  const SANDBOX_TABS = ['log', 'rules', 'fs']
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

  // api calls vibe. options are fetch's, plus timeout (ms, 15s unless
  // said) and cancel: an AbortController whose abort ends the call early.
  async function api(path, options) {
    options = Object.assign({}, options || {})
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), options.timeout || 15000)
    if (options.cancel) options.cancel.signal.addEventListener('abort', () => controller.abort())
    delete options.timeout
    delete options.cancel
    try {
      const headers = Object.assign({ 'X-Vibe-Token': token }, options.body ? { 'Content-Type': 'application/json' } : {})
      const res = await fetch(path, Object.assign({ headers, signal: controller.signal }, options))
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

  // applyTheme gives the page the agent's theme colour (see html.themed in
  // app.css), or takes it away. The server only ever sends a plain rgb() or
  // hex colour; the browser is asked anyway, so nothing else lands in the CSS.
  function applyTheme(color) {
    const root = document.documentElement
    const ok = !!color && CSS.supports('color', color)
    if (ok === root.classList.contains('themed') && (!ok || root.style.getPropertyValue('--theme') === color)) return
    root.classList.toggle('themed', ok)
    if (ok) root.style.setProperty('--theme', color)
    else root.style.removeProperty('--theme')
    $('theme-color').setAttribute('content', ok ? color : '')
  }

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
      $('sandbox').textContent = state.sandbox || 'all profiles and sandboxes'
      renderWhere(res.body.folder || '', res.body.git || null)
      document.title = state.sandbox ? 'vibe · ' + state.sandbox : 'vibe · profiles'
      setFaviconAgent(res.body.agent || '')
      applyTheme(res.body.themeColor || '')
      const url = res.body.clipboardUrl || ''
      const ready = !!(url && res.body.clipboardReady)
      state.clipboardTrouble = res.body.clipboardTrouble || ''
      state.clipboardCanStart = !!res.body.clipboardCanStart
      state.conversation = !!res.body.conversation
      $('tab-conversation').hidden = !state.conversation
      state.admin = !!res.body.admin
      $('tab-admin').hidden = !state.admin
      $('tab-manage').hidden = !res.body.manage
      for (const t of SANDBOX_TABS) $('tab-' + t).hidden = res.body.sandboxTabs === false
      $('admin-dot').hidden = !res.body.upgrades
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
      if ($('tab-' + state.tab).hidden) select(state.tab)
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
      // What start-clipboard said went wrong, when it said, comes first.
      const why = state.clipboardTrouble ? 'The sandbox says: ' + state.clipboardTrouble + ' ' : ''
      $(view.help).textContent = why + (state.clipboardCanStart
        ? 'vibe can try starting it in the sandbox — that is safe to do even if it is already on its way.'
        : 'Check /tmp/clipboard.log and /tmp/clipboard-server.log in the sandbox.')
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
    if (state.tab === 'admin') { loadState(); loadAdmin(); loadInstructions(false); return }
    if (state.tab === 'manage') { loadState(); loadManage(); return }
    loadState()
    load(state.tab)
  }

  // --- tabs -------------------------------------------------------------

  // select shows tab, or the first tab the page has when it hasn't that one.
  function select(tab) {
    if (!TABS.includes(tab) || $('tab-' + tab).hidden) tab = TABS.find((t) => !$('tab-' + t).hidden) || 'log'
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
    const list = opts.list || []
    $('confirm-list').hidden = !list.length
    $('confirm-list').replaceChildren(...list.map((line) => el('li', { text: line })))
    $('confirm-command-note').textContent = opts.commandNote || 'This runs, on your machine:'
    $('confirm-command').textContent = opts.command
    $('confirm-command').hidden = !opts.command
    $('confirm-run').className = opts.danger ? 'danger' : ''
    $('confirm-run').textContent = opts.runLabel || 'Run'
    dialog.returnValue = ''
    dialog.onclose = async () => {
      if (dialog.returnValue !== 'run') return
      try {
        const res = await opts.run()
        reached(true)
        if (res.ok) {
          toast(opts.started ? opts.started : 'Done: ' + (res.body.command || opts.command))
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

  // --- the Admin tab ------------------------------------------------------

  // What the tab last showed: the server's answer, and the features as the
  // profile has them (to tell what the user has ticked since).
  const adminView = { data: null, features: '', jobLines: -1 }

  async function loadAdmin() {
    try {
      const res = await api('/api/admin')
      reached(true)
      if (!res.ok) { problem(res); return }
      renderAdmin(res.body)
    } catch (e) {
      reached(false)
    }
  }

  function pill(id, text, cls) {
    const p = $(id)
    p.textContent = text
    p.className = 'pill' + (cls ? ' ' + cls : '')
  }

  function renderAdmin(data) {
    adminView.data = data
    const st = data.status || {}
    const job = data.job
    const busy = !!(job && job.running)

    pill('admin-state', st.running ? 'running' : st.exists ? 'stopped' : 'not created',
      st.running ? 'ok' : 'bad')
    $('admin-start').hidden = !st.exists || st.running
    $('admin-stop').hidden = !st.running
    for (const id of ['admin-start', 'admin-stop', 'admin-reinit', 'admin-memories', 'admin-recompose']) {
      $(id).disabled = busy
    }
    $('admin-memories').disabled = busy || !st.running
    $('admin-reinit').disabled = busy || !st.profile
    $('admin-info').textContent = st.info || ''
    $('admin-error').hidden = !st.error
    $('admin-error').textContent = st.error || ''

    // The editors are drawn once (so typing isn't undone by a poll), but
    // whether Optimise can run follows the sandbox: redraw them when it
    // starts or stops, unless one holds work not yet saved.
    if (instr.info && instr.info.running !== !!st.running) {
      const busy = Object.values(instr.eds).some((ed) => ed.running || ed.before !== null || ed.box.value !== ed.base)
      if (!busy) loadInstructions(true)
    }
    renderJob(job, 'admin', adminView)
    renderAdminFeatures(st, busy)
    renderAdminUpgrades(data.upgrades || {}, st.commands || {})
    renderAdminGit(st.gitToken || {})
    renderAdminBackups(st.backups || [], busy)
  }

  // renderJob draws the page's one job — an Admin action, or a delete from
  // the Profiles tab — on the card whose ids start with prefix.
  function renderJob(job, prefix, view) {
    $(prefix + '-job-card').hidden = !job
    if (!job) return
    const words = { start: 'Start', stop: 'Stop', reinit: 'Rebuild', rebuild: 'Rebuild', recompose: 'Re-compose', update: 'Update', restore: 'Restore', memories: 'Save memories', delete: 'Delete' }
    $(prefix + '-job-h').firstChild.textContent = (words[job.kind] || 'Last action') + ' '
    pill(prefix + '-job-state', job.running ? (job.cancelled ? 'cancelling…' : job.warning ? 'taking too long' : 'working…')
      : job.cancelled ? 'cancelled' : job.error ? 'failed' : 'done',
      job.running ? (job.warning ? 'warn' : 'busy') : job.error || job.cancelled ? 'bad' : 'ok')
    $(prefix + '-cancel').hidden = !job.running || job.cancelled
    view.jobKind = job.kind
    const lines = (job.lines || []).slice()
    if (job.error) lines.push('', 'failed: ' + job.error)
    if (lines.length !== view.jobLines) {
      view.jobLines = lines.length
      const box = $(prefix + '-job')
      const stick = nearBottom(box)
      box.textContent = lines.join('\n')
      if (stick) box.scrollTop = box.scrollHeight
    }
  }

  // The checklist is drawn afresh only when the profile's features change
  // (after a re-compose), so what the user is ticking isn't undone by the
  // next poll.
  function renderAdminFeatures(st, busy) {
    const list = $('admin-features')
    const note = $('admin-features-note')
    $('admin-recompose').hidden = !st.guided
    if (!st.guided) {
      list.replaceChildren()
      adminView.features = ''
      note.textContent = st.profile
        ? "This profile doesn't record the features it was composed from, so it can't be re-composed here (vibe -R builds a fresh one)."
        : 'There is no profile to compose.'
      return
    }
    note.textContent = 'Tick what the sandbox should have; re-composing rebuilds it. New features go after the ones it has.'
    const features = st.features || []
    const key = JSON.stringify(features.map((f) => [f.name, f.on]))
    if (key === adminView.features) return
    adminView.features = key
    list.replaceChildren(...features.map((f) => {
      const box = el('input', { type: 'checkbox', 'data-name': f.name, 'data-was': f.on ? '1' : '' })
      box.checked = !!f.on
      const was = el('span', { class: 'was' })
      const mark = () => { was.textContent = box.checked === !!f.on ? '' : box.checked ? '(adding)' : '(removing)' }
      box.addEventListener('change', mark)
      return el('li', {}, el('label', {}, box, el('span', { text: f.label || f.name }), was))
    }))
  }

  // pickedFeatures is the features ticked, those the profile has first (in
  // its order), then those being added.
  function pickedFeatures() {
    const st = (adminView.data && adminView.data.status) || {}
    const ticked = new Set(Array.from($('admin-features').querySelectorAll('input:checked')).map((b) => b.dataset.name))
    const had = (st.features || []).filter((f) => f.on).map((f) => f.name)
    const kept = had.filter((n) => ticked.has(n))
    const added = (st.features || []).map((f) => f.name).filter((n) => ticked.has(n) && !had.includes(n))
    return { features: kept.concat(added), added, removed: had.filter((n) => !ticked.has(n)) }
  }

  function renderAdminUpgrades(up, commands) {
    const found = up.found || []
    $('admin-upgrades').textContent = up.checking ? 'Checking for newer releases…'
      : !up.checked ? 'Not checked yet.'
      : found.length ? 'Newer releases are available:' : 'vibe and Docker SBX are up to date.'
    $('admin-upgrades-list').replaceChildren(...found.map((l) => el('li', { text: l })))
    $('admin-check').disabled = !!up.checking
    const cmd = $('admin-upgrade-command')
    cmd.hidden = !found.length
    // --upgrade asks which to install; -f installs them all, unasked.
    cmd.textContent = (commands['upgrade'] || 'vibe --upgrade') + '  ·  all of them, unasked: ' + (commands['upgrade-all'] || 'vibe --upgrade -f')
  }

  function link(href, text) {
    return el('a', { href, target: '_blank', rel: 'noopener noreferrer', text })
  }

  // renderAdminGit says where the token stands and, when there's none or
  // it is running out, exactly what to do on GitHub — the steps the
  // terminal gives. Giving vibe the token stays in the terminal.
  function renderAdminGit(git) {
    const box = $('admin-git')
    const steps = $('admin-git-steps')
    const strong = (cls, text) => el('span', { class: cls, text })
    const code = (text) => el('code', { text })
    const step = (...parts) => el('li', {}, ...parts)
    const command = git.command || 'vibe --git-token'
    const repo = git.repo ? strong('gh-repo', git.repo) : strong('gh-repo', 'the repositories the agent may push to')

    const status = []
    if (git.has) {
      status.push('The agent can push, with ' + (git.source === 'default' ? 'the default token' : 'its own token'))
      status.push(git.expiry ? ', which ' + git.expiry + '.' : '.')
      if (git.rules && git.rules.length) status.push(' Its rules: ' + git.rules.join(', ') + '.')
    } else {
      status.push("There's no token, so the agent can't push. To give it one:")
    }
    if (git.renewUrl) status.push(' To renew it:')
    box.className = 'note' + (git.expired ? ' bad-text' : '')
    box.replaceChildren(...status.map((p) => document.createTextNode(p)))

    let list = []
    if (git.newTokenUrl) {
      list = [
        step(link(git.newTokenUrl, "Open GitHub's form for a new token"), ' — filled in as far as GitHub allows: called ',
          code(git.tokenName || 'vibe'), ', lasting ' + (git.days || 30) + ' days.'),
        step('Under Repository access, change ', strong('gh-leave', '"All repositories"'), ' to ',
          strong('gh-pick', '"Only select repositories"'), ', and choose ', repo, " — GitHub won't let vibe pick it for you."),
        step('Check the permissions: ', strong('gh-perms', 'Contents, Pull requests and Workflows, read and write'), '.'),
        step(strong('gh-last', 'Generate token'), ', and copy it.'),
        step('In a terminal, run ', code(command), ' and paste the token when it asks.'),
      ]
    } else if (git.renewUrl) {
      list = [
        step(link(git.renewUrl, 'Open your tokens on GitHub'), ', and click the one called ', code(git.tokenName || 'vibe'), '.'),
        step(strong('gh-last', 'Regenerate token'), ', pick how long it should last, and copy the new one.'),
        step('In a terminal, run ', code(command), ' and paste it when it asks. If the token is gone from the list, the same command makes a new one.'),
      ]
    }
    steps.hidden = !list.length
    steps.replaceChildren(...list)
  }

  function renderAdminBackups(backups, busy) {
    const list = $('admin-backups')
    if (!backups.length) { list.replaceChildren(el('li', { class: 'muted', text: 'No backups yet.' })); return }
    list.replaceChildren(...backups.map((b) => el('li', {},
      el('span', { text: b.label }), el('span', { class: 'spacer' }),
      el('button', { type: 'button', class: 'small ghost', text: 'Restore…', disabled: busy,
        onclick: () => adminConfirm('restore', { backup: b.id }, {
          title: 'Restore the backup of ' + b.label + '?',
          text: "The profile goes back to how it was then, and the sandbox is rebuilt from it. The profile as it is now is backed up first. The agent's memories and this conversation are kept.",
          danger: true,
        }) }))))
  }

  async function adminDo(kind, extra) {
    try {
      const res = await post('/api/admin/do', Object.assign({ kind }, extra || {}))
      reached(true)
      if (!res.ok) toast((res.body && res.body.error) || 'That failed.', true)
      loadAdmin()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  function adminConfirm(kind, extra, opts) {
    const commands = (adminView.data && adminView.data.status && adminView.data.status.commands) || {}
    confirmThen(Object.assign({
      command: commands[kind] || '',
      started: 'Started — the Last action box shows how it goes.',
      run: () => post('/api/admin/do', Object.assign({ kind }, extra || {})),
    }, opts))
  }

  // A rebuild cancelled part-way can leave the sandbox half-made: that's
  // asked about first. The others are safe to cancel at once.
  const REBUILDS = ['reinit', 'recompose', 'restore']

  async function cancelAdmin() {
    try {
      const res = await post('/api/admin/cancel', {})
      reached(true)
      if (!res.ok) toast((res.body && res.body.error) || 'That could not be cancelled.', true)
      else if (!res.body.cancelled) toast('Nothing was running to cancel.')
      loadAdmin()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  function setUpAdmin() {
    $('admin-cancel').addEventListener('click', () => {
      if (!REBUILDS.includes(adminView.jobKind)) { cancelAdmin(); return }
      confirmThen({
        title: 'Cancel the rebuild?',
        text: 'Stopping part-way may leave the sandbox half-made. Afterwards, the card says what state it is in, and Rebuild makes it again from its profile.',
        command: 'vibe --cancel',
        danger: true,
        started: 'Cancelling — the Last action box says how it ended.',
        run: () => post('/api/admin/cancel', {}),
      })
    })
    $('admin-start').addEventListener('click', () => adminDo('start'))
    $('admin-memories').addEventListener('click', () => adminDo('memories'))
    $('admin-stop').addEventListener('click', () => adminConfirm('stop', {}, {
      title: 'Stop sandbox ' + state.sandbox + '?',
      text: 'The agent stops, and the terminal session ends — vibe and this page stay up, and the terminal attaches again when you start the sandbox (here, or with vibe in its folder).',
      danger: true,
    }))
    $('admin-reinit').addEventListener('click', () => adminConfirm('reinit', {}, {
      title: 'Rebuild sandbox ' + state.sandbox + '?',
      text: "It is destroyed and created again from its profile, which takes a few minutes. The agent's memories are copied out first and put back, and this conversation is kept. The terminal attaches again when it's done.",
      danger: true,
    }))
    $('admin-recompose').addEventListener('click', () => {
      const pick = pickedFeatures()
      const change = [
        pick.added.length ? 'adding ' + pick.added.join(', ') : '',
        pick.removed.length ? 'removing ' + pick.removed.join(', ') : '',
      ].filter(Boolean).join('; ')
      adminConfirm('recompose', { features: pick.features }, {
        title: 'Re-compose ' + state.sandbox + (change ? ', ' + change + '?' : ' with the same features?'),
        text: "The profile is backed up, composed afresh from " + (pick.features.length ? pick.features.join(', ') : 'the base features alone') +
          ", and the sandbox rebuilt from it — a few minutes. The agent's memories and this conversation are kept.",
        danger: true,
      })
    })
    $('admin-check').addEventListener('click', async () => {
      try {
        const res = await post('/api/admin/upgrades', {})
        reached(true)
        if (!res.ok) toast((res.body && res.body.error) || 'That failed.', true)
        loadAdmin()
      } catch (e) {
        reached(false)
      }
    })
  }

  // --- the Profiles tab ---------------------------------------------------

  // What the tab last showed, and what's ticked, by ID (see manage.js).
  const manageView = { inv: null, key: '', ticked: new Set(), jobLines: -1, busy: false }
  const M = window.vibeManage

  async function loadManage() {
    try {
      // Taking stock asks sbx, which vibe gives 20 seconds.
      const res = await api('/api/manage', { timeout: 30000 })
      reached(true)
      if (!res.ok) { problem(res); return }
      renderManage(res.body)
    } catch (e) {
      reached(false)
    }
  }

  function renderManage(data) {
    const inv = data.inventory || { profiles: [], unknown: [] }
    const job = data.job
    const busy = !!(job && job.running)
    // A delete has just ended: what was ticked is gone, or failed and says so.
    if (manageView.busy && !busy) manageView.ticked.clear()
    manageView.busy = busy
    manageView.inv = inv
    $('manage-wait').hidden = true
    $('manage-error').hidden = !inv.error
    $('manage-error').textContent = inv.error || ''
    // Drawn afresh only when what's there changes, so ticking isn't undone
    // by the next poll.
    const key = JSON.stringify(inv) + busy
    if (key !== manageView.key) {
      manageView.key = key
      drawManage()
    }
    updateManage()
    renderJob(job, 'manage', manageView)
  }

  // Flags that say something's gone are trash's tell, so they stand out.
  const BAD_FLAGS = ['no sandboxes', 'every sandbox or folder gone', 'profile gone', 'gone from sbx', 'folder gone']

  function flagPills(flags) {
    return (flags || []).map((f) => el('span', {
      class: 'flag' + (BAD_FLAGS.includes(f) ? ' bad' : f === 'running' ? ' ok' : ''), text: f,
    }))
  }

  function tickBox(row, label, covered) {
    const box = el('input', { type: 'checkbox', 'data-id': row.id })
    box.checked = manageView.ticked.has(row.id) || !!covered
    box.disabled = manageView.busy || !M.pickable(row) || !!covered
    if (!M.pickable(row)) box.title = "Can't be deleted: " + row.kept
    else if (covered) box.title = 'Goes with profile ' + covered.name
    box.addEventListener('change', () => {
      if (box.checked) manageView.ticked.add(row.id)
      else manageView.ticked.delete(row.id)
      // A profile ticked or unticked takes its sandboxes with it.
      if (row.id.startsWith('profile:')) drawManage()
      updateManage()
    })
    return el('label', { class: 'mgr-pick' }, box, label)
  }

  // What each action is called on its button, and what it asks first (none:
  // it just runs). The terminal's way is given too, as every action's is.
  const MANAGE_ACTIONS = {
    start: { label: 'Start' },
    stop: { label: 'Stop…', ask: (name) => ({
      title: 'Stop sandbox ' + name + '?',
      text: 'The agent stops. A vibe attached to it keeps its page up, to start it again from.',
      command: 'vibe --stop <its folder>' }) },
    rebuild: { label: 'Rebuild…', ask: (name) => ({
      title: 'Rebuild sandbox ' + name + '?',
      text: "It's destroyed and made again from its profile, which takes a few minutes, and left running or stopped as it was. The agent's memories are kept; anything installed by hand, or kept outside its folder, goes.",
      command: 'vibe -r <its folder>' }) },
    update: { label: 'Update…', ask: (name, isProfile) => ({
      title: 'Bring ' + (isProfile ? 'profile ' + name + ' and its sandboxes' : 'sandbox ' + name) + ' up to date?',
      text: "A profile the library has moved on from is composed afresh (backed up first, and not if it was edited by hand), and each sandbox built from what has since changed is rebuilt, left running or stopped as it was. The agent's memories are kept.",
      command: isProfile ? 'vibe --update-sandboxes' : 'vibe <its folder>' }) },
    recompose: { label: 'Re-compose…', ask: (name) => ({
      title: 'Re-compose profile ' + name + '?',
      text: "It's backed up and composed afresh from its features, replacing any edits made to it by hand, and its sandboxes are rebuilt — a few minutes each. One with a vibe attached, this page's own, or one whose folder is gone is left as it is.",
      command: 'vibe -C <a folder it is for>' }) },
  }

  function actionButtons(row, isProfile) {
    return (row.actions || []).map((kind) => {
      const a = MANAGE_ACTIONS[kind]
      if (!a) return null
      return el('button', { type: 'button', class: 'small ghost', text: a.label, disabled: manageView.busy,
        onclick: () => manageAction(kind, row, isProfile) })
    })
  }

  async function manageAction(kind, row, isProfile) {
    const run = () => post('/api/manage/do', { kind, id: row.id })
    const a = MANAGE_ACTIONS[kind]
    if (!a.ask) {
      try {
        const res = await run()
        reached(true)
        if (!res.ok) toast((res.body && res.body.error) || 'That failed.', true)
        loadManage()
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
      }
      return
    }
    confirmThen(Object.assign({
      commandNote: 'From a terminal:',
      danger: kind !== 'update',
      runLabel: a.label.replace('…', ''),
      started: 'Started — the Last action box shows how it goes.',
      run,
    }, a.ask(row.name, isProfile)))
  }

  function keptNote(row) {
    return row.kept ? el('span', { class: 'mgr-kept muted', text: "can't be deleted: " + row.kept }) : null
  }

  function drawManage() {
    const inv = manageView.inv
    const list = $('manage-list')
    if (!inv) return
    const groups = (inv.profiles || []).map((p) => {
      const head = el('div', { class: 'mgr-row mgr-head' },
        tickBox(p, el('span', {}, el('strong', { text: p.name }),
          p.features && p.features.length ? el('span', { class: 'muted', text: ' ' + p.features.join(', ') }) : null)),
        ...flagPills(p.flags), keptNote(p), el('span', { class: 'spacer' }), ...actionButtons(p, true))
      const sandboxes = (p.sandboxes || []).map((s) => el('li', { class: 'mgr-row' },
        tickBox(s, el('span', {}, el('span', { text: s.name + (s.own ? ' (this page)' : '') }),
          el('code', { class: 'mgr-folder', text: s.folder })), M.coveredBy(inv, manageView.ticked, s.id)),
        ...flagPills(s.flags), keptNote(s), el('span', { class: 'spacer' }), ...actionButtons(s, false)))
      return el('div', { class: 'mgr-profile' }, head, sandboxes.length ? el('ul', { class: 'mgr-sandboxes' }, ...sandboxes) : null)
    })
    if (!groups.length && !inv.error) groups.push(el('p', { class: 'muted', text: 'No profiles yet.' }))
    const unknown = inv.unknown || []
    if (unknown.length) {
      groups.push(el('h3', { class: 'mgr-unknown-h', text: 'Sandboxes vibe has no record of' }),
        el('ul', { class: 'mgr-sandboxes' }, ...unknown.map((u) => el('li', { class: 'mgr-row' },
          tickBox(u, el('span', { text: u.name })), ...flagPills([u.running ? 'running' : 'stopped'])))))
    }
    list.replaceChildren(...groups)
  }

  function updateManage() {
    const inv = manageView.inv || { profiles: [], unknown: [] }
    $('manage-count').textContent = M.summary(inv, manageView.ticked)
    $('manage-delete').disabled = manageView.busy || !M.picked(inv, manageView.ticked).length
    $('manage-trash').disabled = manageView.busy
    $('manage-none').disabled = manageView.busy
  }

  async function deleteTicked() {
    const ids = M.picked(manageView.inv, manageView.ticked)
    if (!ids.length) return
    try {
      const res = await post('/api/manage/plan', { ids })
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || "That can't be deleted.", true); loadManage(); return }
      confirmThen({
        title: 'Delete ' + M.summary(manageView.inv, manageView.ticked).replace(/ ticked$/, '') + '?',
        text: "There's no undo. This deletes:",
        list: res.body.lines || [],
        commandNote: 'From a terminal, vibe --cleanup does the same.',
        command: '',
        danger: true,
        runLabel: 'Delete',
        started: 'Deleting — the Last action box shows how it goes.',
        run: () => post('/api/manage/delete', { ids }),
      })
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  function setUpManage() {
    $('manage-trash').addEventListener('click', () => {
      if (!manageView.inv) return
      manageView.ticked = M.likelyTrash(manageView.inv)
      drawManage()
      updateManage()
    })
    $('manage-none').addEventListener('click', () => {
      manageView.ticked.clear()
      drawManage()
      updateManage()
    })
    $('manage-delete').addEventListener('click', deleteTicked)
    $('manage-cancel').addEventListener('click', async () => {
      try {
        const res = await post('/api/manage/cancel', {})
        reached(true)
        if (!res.ok) toast((res.body && res.body.error) || 'That could not be cancelled.', true)
        loadManage()
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
      }
    })
  }

  // --- the agent's instructions, on the Admin tab ------------------------

  // INSTRUCTIONS_HINT is the size past which a file is worth tidying: these
  // are loaded into every session, so their size costs on every turn.
  const INSTRUCTIONS_HINT = 8 * 1024

  // One editor per file: what it was loaded as (base, version), and the
  // state of an Optimise — running (with its AbortController), or done and
  // waiting for Accept or Revert (with the text from before it).
  const instr = { loaded: false, info: null, eds: {} }

  async function loadInstructions(force) {
    if (instr.loaded && !force) return
    try {
      const res = await api('/api/admin/instructions')
      reached(true)
      if (!res.ok) { $('admin-instructions-note').textContent = (res.body && res.body.error) || "The agent's instructions can't be edited here."; return }
      instr.loaded = true
      renderInstructions(res.body)
    } catch (e) {
      reached(false)
    }
  }

  function renderInstructions(info) {
    instr.info = info
    $('admin-instructions-note').textContent = !info.applies
      ? 'The ' + (info.agent || 'sandbox') + " agent doesn't load these files yet: they're kept, and used by sandboxes whose agent does."
      : 'Loaded by the agent at the start of every session: the universal file first, then the profile\'s.'
    $('admin-instructions').replaceChildren(...(info.files || []).map((f) => instructionsEditor(f, info)))
  }

  function sizeText(n) {
    return n < 1024 ? n + ' bytes' : (n / 1024).toFixed(1) + ' KB'
  }

  function instructionsEditor(file, info) {
    const box = el('textarea', { spellcheck: 'true', 'aria-label': file.label + ' — ' + file.path, readonly: !file.editable })
    box.value = file.text || ''
    const meta = el('p', { class: 'meta' })
    const status = el('p', { class: 'status', role: 'status' })
    const save = el('button', { type: 'button', text: 'Save' })
    const optimise = el('button', { type: 'button', class: 'ghost', text: 'Optimise' })
    const accept = el('button', { type: 'button', text: 'Accept', hidden: true })
    const revert = el('button', { type: 'button', class: 'ghost', text: 'Revert', hidden: true })
    const cancel = el('button', { type: 'button', class: 'ghost', text: 'Cancel', hidden: true })
    const ed = { file, box, meta, status, save, optimise, accept, revert, cancel,
      base: file.text || '', version: file.version, before: null, running: null, timer: null }
    instr.eds[file.which] = ed

    const why = !file.editable ? file.note || "This file can't be edited here."
      : !info.canOptimise ? 'Optimising isn\'t available for ' + (info.agent || 'this agent') + ' yet.'
      : !info.running ? 'Start the sandbox to optimise: the agent does it.' : ''
    optimise.title = why || 'Ask the agent for a tidier version, to accept or revert'

    const refresh = () => {
      const bytes = new Blob([box.value]).size
      meta.textContent = sizeText(bytes) + (bytes > INSTRUCTIONS_HINT ? ' — large for something loaded every session: Optimise may trim it' : '') +
        (box.value !== ed.base && !ed.before ? ' · unsaved changes' : '')
      meta.className = 'meta' + (bytes > INSTRUCTIONS_HINT ? ' warn' : '')
      const reviewing = ed.before !== null
      save.hidden = reviewing
      accept.hidden = revert.hidden = !reviewing
      cancel.hidden = !ed.running
      optimise.hidden = !!ed.running
      save.disabled = !file.editable || !!ed.running || box.value === ed.base
      optimise.disabled = !!why || reviewing || !!ed.running
      box.readOnly = !file.editable || !!ed.running
    }
    ed.refresh = refresh
    box.addEventListener('input', refresh)
    save.addEventListener('click', () => saveInstructions(ed))
    accept.addEventListener('click', () => saveInstructions(ed))
    revert.addEventListener('click', () => {
      replaceText(box, ed.before)
      ed.before = null
      setStatus(ed, 'Reverted to your version.')
      refresh()
    })
    optimise.addEventListener('click', () => optimiseInstructions(ed))
    cancel.addEventListener('click', () => { if (ed.running) ed.running.abort() })
    if (file.note) setStatus(ed, file.note)
    refresh()
    return el('section', { class: 'instr' },
      el('h3', {}, el('span', { text: file.label }), el('span', { class: 'path', text: file.path })),
      box, meta, el('div', { class: 'bar' }, save, accept, revert, optimise, cancel), status)
  }

  function setStatus(ed, text, cls, spinning) {
    ed.status.className = 'status' + (cls ? ' ' + cls : '')
    ed.status.replaceChildren(...(spinning ? [el('span', { class: 'spinner', 'aria-hidden': 'true' })] : []), text)
  }

  // replaceText puts text in the box the way typing would, so Ctrl-Z can
  // take it back; where the browser can't, it just sets it.
  function replaceText(box, text) {
    box.focus()
    box.select()
    let done = false
    try { done = document.execCommand(text ? 'insertText' : 'delete', false, text) } catch (e) { /* not supported */ }
    if (!done || box.value !== text) box.value = text
    box.setSelectionRange(0, 0)
    box.scrollTop = 0
  }

  async function saveInstructions(ed, resolve) {
    const payload = { which: ed.file.which, text: ed.box.value, base: ed.base, version: ed.version }
    if (resolve) payload.resolve = resolve
    ed.save.disabled = ed.accept.disabled = true
    setStatus(ed, 'Saving…', '', true)
    try {
      const res = await api('/api/admin/instructions', { method: 'POST', body: JSON.stringify(payload), timeout: 60000 })
      reached(true)
      if (res.status === 409 && res.body.conflict) { askChanged(ed, res.body); return }
      if (!res.ok || !res.body.saved) { setStatus(ed, (res.body && res.body.error) || 'That could not be saved.', 'bad'); return }
      ed.base = res.body.text
      ed.version = res.body.version
      if (ed.box.value !== res.body.text) replaceText(ed.box, res.body.text) // merged
      ed.before = null
      setStatus(ed, savedText(res.body), res.body.failed ? 'bad' : 'ok')
    } catch (e) {
      reached(false)
      setStatus(ed, 'Could not reach vibe.', 'bad')
    } finally {
      ed.accept.disabled = false
      ed.refresh()
    }
  }

  // savedText says where a save went: which sandboxes have it now, which
  // get it when they start, and how the terminal session picks it up.
  function savedText(r) {
    const parts = ['Saved.']
    if (r.updated && r.updated.length) parts.push('In use now in: ' + r.updated.join(', ') + '.')
    if (r.notRunning && r.notRunning.length) parts.push('When they next start: ' + r.notRunning.join(', ') + '.')
    for (const [name, why] of Object.entries(r.failed || {})) parts.push("Couldn't give it to " + name + ': ' + why)
    if (instr.info && instr.info.applies) {
      parts.push('\nThe next prompt here uses it. A session in the terminal picks it up when it restarts — or ask it to re-read ~/.claude/vibe-instructions.md.')
    }
    return parts.join(' ')
  }

  // askChanged offers what to do about a file changed on disk since it was
  // loaded: merge (the default, when it can), overwrite, or reload.
  function askChanged(ed, r) {
    const dialog = $('changed')
    $('changed-path').textContent = ed.file.path
    const merge = dialog.querySelector('input[value="merge"]')
    merge.disabled = !r.canMerge
    $('changed-why').textContent = r.canMerge ? '' : '(not possible: ' + (r.why || 'the changes overlap') + ')'
    dialog.querySelector('input[value="' + (r.canMerge ? 'merge' : 'overwrite') + '"]').checked = true
    dialog.returnValue = ''
    dialog.onclose = () => {
      ed.refresh()
      if (dialog.returnValue !== 'go') { setStatus(ed, 'Not saved: the file changed on disk.', 'bad'); return }
      const pick = dialog.querySelector('input[name="changed"]:checked').value
      if (pick === 'reload') {
        ed.base = r.diskText
        ed.version = r.diskVersion
        ed.before = null
        replaceText(ed.box, r.diskText)
        setStatus(ed, 'Reloaded from disk: your changes here were discarded.')
        ed.refresh()
        return
      }
      // The disk version is the new base for an overwrite; a merge keeps
      // the page's base, which is what the merge works from.
      if (pick === 'overwrite') ed.version = r.diskVersion
      saveInstructions(ed, pick)
    }
    dialog.showModal()
  }

  async function optimiseInstructions(ed) {
    const started = Date.now()
    ed.running = new AbortController()
    const tickTime = () => setStatus(ed, 'Optimising… ' + Math.round((Date.now() - started) / 1000) + 's — the agent is reading it', '', true)
    tickTime()
    ed.timer = setInterval(tickTime, 1000)
    ed.refresh()
    try {
      const res = await api('/api/admin/instructions/optimise', {
        method: 'POST', body: JSON.stringify({ which: ed.file.which, text: ed.box.value }),
        // The server gives up first (at 10 minutes) and says why.
        timeout: 11 * 60000, cancel: ed.running,
      })
      reached(true)
      if (!res.ok) { setStatus(ed, (res.body && res.body.error) || 'Optimising failed — nothing changed.', 'bad'); return }
      ed.before = ed.box.value
      replaceText(ed.box, res.body.text)
      setStatus(ed, 'Here is the agent\'s version (' + sizeText(new Blob([ed.before]).size) + ' → ' +
        sizeText(new Blob([res.body.text]).size) + '). Edit it if you like, then Accept to save it, or Revert.')
    } catch (e) {
      setStatus(ed, ed.running && ed.running.signal.aborted ? 'Cancelled — nothing changed.' : 'Could not reach vibe.', 'bad')
    } finally {
      clearInterval(ed.timer)
      ed.running = null
      ed.refresh()
    }
  }

  // --- the conversation --------------------------------------------------

  const convo = { last: 0, running: false, polling: false, behind: false, lastPrompt: '', ticks: new Map(), typing: null }

  // showTyping keeps the "typing" bubble below the agent's latest message
  // for as long as it is answering, and takes it away once it has. Its
  // output comes a whole message at a time, so between messages it is
  // always still at work: there's no quiet to wait out first, and nothing
  // the page doesn't draw (the agent's start-up, its thinking, hooks) can
  // take the bubble away.
  function showTyping() {
    const log = $('convo-log')
    const show = convo.running
    if (!convo.typing) {
      // Hidden from screen readers: the log around it is announced, and
      // the bubble comes and goes with every tool call. The Stop button
      // and the ticks already say a turn is running.
      convo.typing = el('div', { class: 'typing', 'aria-hidden': 'true' },
        el('span'), el('span'), el('span'))
    }
    if (!show) { convo.typing.remove(); return }
    if (convo.typing.parentNode === log && log.lastChild === convo.typing) return
    const stick = nearBottom(log)
    log.append(convo.typing)
    if (stick && !$('panel-conversation').hidden) log.scrollTop = log.scrollHeight
  }

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

  // markdown is the agent's Markdown as elements, built from what
  // markdown.js read: every piece of text set as text, never as markup. A
  // link becomes one only where localLink allows (this machine's own pages);
  // any other shows its address instead.
  function markdown(text) {
    const inline = (nodes) => nodes.map((n) => {
      switch (n.type) {
        case 'text': return document.createTextNode(n.text)
        case 'code': return el('code', { text: n.text })
        case 'br': return el('br')
        case 'strong': return el('strong', {}, ...inline(n.children))
        case 'em': return el('em', {}, ...inline(n.children))
        case 'del': return el('del', {}, ...inline(n.children))
        case 'link': {
          const link = localLink(n.href)
          if (link) { link.replaceChildren(...inline(n.children)); return link }
          const label = n.children.map((c) => c.text || '').join('')
          return document.createTextNode(label === n.href ? n.href : label + ' (' + n.href + ')')
        }
      }
      return document.createTextNode('')
    })
    const blocks = (list) => list.map((b) => {
      switch (b.type) {
        case 'para': return el('p', {}, ...inline(b.inline))
        case 'heading': return el('h' + Math.min(6, Math.max(1, b.level)), {}, ...inline(b.inline))
        case 'code': return el('pre', {}, el('code', { text: b.text }))
        case 'hr': return el('hr')
        case 'quote': return el('blockquote', {}, ...blocks(b.blocks))
        case 'list': return el(b.ordered ? 'ol' : 'ul', b.ordered && b.start !== 1 ? { start: String(b.start) } : {},
          ...b.items.map((item) => el('li', {}, ...blocks(item))))
        case 'table': {
          const cell = (tag, content, i) => el(tag, b.align[i] ? { class: 'align-' + b.align[i] } : {}, ...inline(content))
          return el('div', { class: 'table-scroll' }, el('table', {},
            el('thead', {}, el('tr', {}, ...b.head.map((c, i) => cell('th', c, i)))),
            el('tbody', {}, ...b.rows.map((r) => el('tr', {}, ...r.map((c, i) => cell('td', c, i)))))))
        }
      }
      return document.createTextNode('')
    })
    return el('div', { class: 'md' }, ...blocks(window.vibeMarkdown.parse(text)))
  }

  function convoNote(cls, text, at) {
    const time = stamp(at)
    const words = cls === 'agent' ? markdown(text) : el('span', { text })
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
  // quietMinutes, when not 0, is how long the agent has been answering
  // without a word: long enough that it may be stuck. The turn isn't
  // stopped — that's the user's call.
  function setConvoRunning(running, background, quietMinutes) {
    if (running) setFaviconDot('working')
    else if (convo.running) setFaviconDot(document.visibilityState === 'visible' && document.hasFocus() ? '' : 'done')
    convo.running = running
    $('convo-send').disabled = running
    // Only while it answers: what it has left running ends with the next
    // prompt anyway.
    $('convo-stop').hidden = !running
    $('convo-new').disabled = running
    // While it answers, the typing bubble says so.
    const status = $('convo-status')
    status.classList.toggle('warn-text', !!(running && quietMinutes))
    status.textContent = running && quietMinutes
      ? 'No output from the agent for ' + quietMinutes + ' minutes — it may be stuck. Stop it, or check "sbx ls" in a terminal; it is still being waited for.'
      : !running && background ? 'The agent has answered, and has something still running. Sending a prompt ends it.' : ''
  }

  async function pollConversation() {
    // Polled from other tabs, and with the page in the background too, so
    // the tab's icon can say when the agent is done.
    if (!state.conversation || convo.polling) return
    convo.polling = true
    try {
      const res = await api('/api/conversation?after=' + convo.last + '&commands=' + slash.seq)
      reached(true)
      if (!res.ok) { problem(res); return }
      banner('')
      const log = $('convo-log')
      const stick = nearBottom(log)
      if (res.body.reset) { log.replaceChildren(); convo.ticks.clear() }
      if (Array.isArray(res.body.commands)) {
        slash.commands = res.body.commands.filter((c) => typeof c === 'string')
        slash.seq = res.body.commandsSeq || 0
        if (slash.open) updateCommands(false)
      }
      for (const ev of res.body.events || []) showEvent(ev)
      if (res.body.receipt) showReceipt(res.body.receipt.seq, res.body.receipt.state)
      convo.last = res.body.last || 0
      setConvoRunning(!!res.body.running, !!res.body.background, res.body.quietMinutes || 0)
      if ((res.body.events || []).length) {
        if ($('panel-conversation').hidden) convo.behind = true
        else if (stick) log.scrollTop = log.scrollHeight
      }
      showTyping()
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
      closeCommands()
      setConvoRunning(true)
      showTyping()
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

  // --- completing slash commands -----------------------------------------

  // The slash commands the agent has (learned from it as it starts, so none
  // until it has answered once), and the pop-over offering them while the
  // prompt's first word is one being typed. The closest match is always the
  // one picked, so Tab completes it; the arrow keys pick another.
  const slash = { commands: [], seq: -1, shown: [], at: 0, open: false }

  // commandWord is the command being typed: the prompt's first word, while
  // it starts with "/" and the cursor is in it; null otherwise.
  function commandWord(input) {
    const v = input.value
    if (!v.startsWith('/') || input.selectionStart !== input.selectionEnd) return null
    const space = v.search(/\s/)
    const end = space < 0 ? v.length : space
    const caret = input.selectionStart
    if (caret < 1 || caret > end) return null
    return { query: v.slice(1, end), end }
  }

  // matching is the commands with query in them: those starting with it
  // first, shortest first, so the closest match leads. Nothing typed yet
  // lists them all, alphabetically.
  function matching(query) {
    if (!query) return slash.commands.slice().sort((a, b) => a.localeCompare(b))
    const q = query.toLowerCase()
    const starts = []
    const within = []
    for (const name of slash.commands) {
      const at = name.toLowerCase().indexOf(q)
      if (at === 0) starts.push(name)
      else if (at > 0) within.push(name)
    }
    starts.sort((a, b) => a.length - b.length || a.localeCompare(b))
    return starts.concat(within)
  }

  function closeCommands() {
    slash.open = false
    $('convo-commands').hidden = true
    $('convo-commands').replaceChildren()
    $('convo-input').setAttribute('aria-expanded', 'false')
    $('convo-input').removeAttribute('aria-activedescendant')
  }

  // updateCommands shows the pop-over for what is being typed, or hides
  // it; fresh is a change to the text, which picks the closest match again.
  function updateCommands(fresh) {
    const input = $('convo-input')
    const word = commandWord(input)
    if (!word) { closeCommands(); return }
    const list = $('convo-commands')
    const shown = matching(word.query)
    if (!slash.commands.length && !word.query) {
      list.replaceChildren(el('li', { class: 'hint', 'aria-disabled': 'true',
        text: "The agent's slash commands show here once it has answered a prompt." }))
      slash.shown = []
    } else if (!shown.length) {
      closeCommands()
      return
    } else {
      if (fresh || slash.at >= shown.length) slash.at = 0
      slash.shown = shown
      const q = word.query.toLowerCase()
      list.replaceChildren(...shown.map((name, i) => {
        const at = q ? name.toLowerCase().indexOf(q) : -1
        const label = at < 0 ? [name]
          : [name.slice(0, at), el('mark', { text: name.slice(at, at + q.length) }), name.slice(at + q.length)]
        const item = el('li', { id: 'convo-command-' + i, role: 'option', 'aria-selected': String(i === slash.at) },
          '/', ...label)
        // mousedown, not click: the prompt keeps the focus.
        item.addEventListener('mousedown', (ev) => { ev.preventDefault(); acceptCommand(i) })
        return item
      }))
    }
    slash.open = true
    list.hidden = false
    input.setAttribute('aria-expanded', 'true')
    if (slash.shown.length) {
      input.setAttribute('aria-activedescendant', 'convo-command-' + slash.at)
      const picked = $('convo-command-' + slash.at)
      if (picked) picked.scrollIntoView({ block: 'nearest' })
    } else {
      input.removeAttribute('aria-activedescendant')
    }
  }

  function moveCommand(by) {
    if (!slash.shown.length) return
    slash.at = (slash.at + by + slash.shown.length) % slash.shown.length
    updateCommands(false)
  }

  // acceptCommand puts the picked command in place of the word being typed,
  // with a space after it, ready for what goes with it.
  function acceptCommand(i) {
    const input = $('convo-input')
    const word = commandWord(input)
    const name = slash.shown[i]
    if (!word || !name) { closeCommands(); return }
    const rest = input.value.slice(word.end).replace(/^[ \t]+/, '')
    input.value = '/' + name + ' ' + rest
    const caret = name.length + 2
    input.setSelectionRange(caret, caret)
    saveDraft()
    closeCommands()
  }

  function setUpCommands() {
    const input = $('convo-input')
    // Ahead of the prompt's other keys: while the pop-over is open, the
    // arrows, Tab, Enter and Escape are its.
    input.addEventListener('keydown', (ev) => {
      if (!slash.open || ev.isComposing) return
      const plain = !ev.ctrlKey && !ev.metaKey && !ev.altKey && !ev.shiftKey
      let handled = true
      if (ev.key === 'ArrowDown' && plain) moveCommand(1)
      else if (ev.key === 'ArrowUp' && plain) moveCommand(-1)
      else if ((ev.key === 'Tab' || ev.key === 'Enter') && plain && slash.shown.length) acceptCommand(slash.at)
      else if (ev.key === 'Escape') closeCommands()
      else handled = false
      if (handled) { ev.preventDefault(); ev.stopImmediatePropagation() }
    })
    input.addEventListener('input', () => updateCommands(true))
    // The cursor moving into or out of the first word.
    input.addEventListener('click', () => updateCommands(false))
    input.addEventListener('keyup', (ev) => {
      if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(ev.key)) updateCommands(false)
    })
    input.addEventListener('blur', closeCommands)
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
    setUpCommands()
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
  setUpAdmin()
  setUpManage()

  document.addEventListener('visibilitychange', tick)
  state.timer = setInterval(tick, POLL_MS)

  setLink(false, 'connecting…')
  loadState().then(() => select(state.tab))
}

  start()
})()
