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
  const TABS = ['conversation', 'review', 'log', 'rules', 'fs', 'admin', 'manage']
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
    { frame: 'convo-clipboard-frame', wait: 'convo-clipboard-wait', text: 'convo-clipboard-wait-text', help: 'convo-clipboard-wait-help', start: 'convo-clipboard-start', open: 'convo-clipboard-open', note: 'convo-clipboard-note' },
  ]

  const $ = (id) => document.getElementById(id)

  // The clipboard page is told it is framed here, which says where it is
  // already, so it leaves out its title.
  function framedClipboardUrl(url) {
    try {
      const framed = new URL(url)
      framed.searchParams.set('framed', '1')
      return framed.href
    } catch (e) {
      return url
    }
  }

  const store = {
    get(key) { try { return localStorage.getItem(key) } catch (e) { return null } },
    set(key, value) { try { localStorage.setItem(key, value) } catch (e) { /* private window: carry on */ } },
  }

  // The token rides in the URL's fragment, which the browser never sends
  // anywhere. Kept in localStorage too (per origin, so per sandbox), so a
  // tab opened without it still works. Only a fragment shaped like a token
  // (as vibe makes them) is taken for one: anything else there, from an
  // in-page link say, would otherwise replace the token kept, and the page
  // would have none.
  const token = (function () {
    const fromUrl = location.hash.slice(1)
    if (/^[0-9a-f]{64}$/.test(fromUrl)) { store.set('vibe-companion-token', fromUrl); return fromUrl }
    return store.get('vibe-companion-token') || ''
  })()

  const state = {
    sandbox: '',
    // attention is the tabs vibe last said want looking at, and why;
    // reviewAt when the agent last updated the review ("" if never).
    attention: {},
    reviewAt: '',
    clipboardUrl: '',
    clipboardReady: false,
    clipboardCanStart: false,
    clipboardWaitingSince: 0,
    clipboardStarting: false,
    conversation: false,
    tab: store.get('vibe-companion-tab') || 'conversation',
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

  // The cross over the icon while vibe isn't there: red, outlined in white
  // so it shows on any agent's icon.
  const CROSS = '<path d="M7 7L25 25M25 7L7 25" stroke="#fff" stroke-width="8" stroke-linecap="round"/>' +
    '<path d="M7 7L25 25M25 7L7 25" stroke="#d93025" stroke-width="4.5" stroke-linecap="round"/>'

  const favicon = { agent: '', dot: '', step: 0, timer: null, shown: '', down: false }

  function drawFavicon() {
    let svg = AGENT_ICONS[favicon.agent] || VIBE_ICON
    // Disconnected, what the dot says is stale: the cross says what is.
    const colors = favicon.down ? null : DOT[favicon.dot]
    if (favicon.down) svg += CROSS
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

  // setLink shows whether vibe is reached ('up', 'down' or 'connecting') on
  // the power button, and says it, in words, to everything that can't see
  // its colour: its tooltip and label, the screen reader's live region, and
  // the first line of its menu.
  function setLink(how, text) {
    const link = $('link')
    for (const s of ['up', 'down', 'connecting']) link.classList.toggle(s, s === how)
    link.title = text
    link.setAttribute('aria-label', 'Power menu: ' + text)
    $('link-text').textContent = text
    $('power-status').textContent = 'Status: ' + (how === 'up' ? 'connected' : how === 'down' ? 'not connected — ' + text : text)
  }

  // setUpPowerMenu makes the power button open its menu, and nothing more: it
  // never acts on a click. The menu is the same wherever vibe is reached or
  // not, as what it holds works without vibe.
  function setUpPowerMenu() {
    const button = $('link')
    const menu = $('power-menu')
    const close = () => { menu.hidden = true; button.setAttribute('aria-expanded', 'false') }
    button.addEventListener('click', () => {
      if (!menu.hidden) { close(); return }
      menu.hidden = false
      button.setAttribute('aria-expanded', 'true')
      const first = menu.querySelector('a, button:not(:disabled)')
      if (first) first.focus()
      showPowerActions()
    })
    for (const action of ['stop', 'restart']) {
      $('power-' + action).addEventListener('click', () => { close(); askPower(action, state.sandbox, powerChecked) })
    }
    menu.addEventListener('click', (ev) => { if (ev.target.closest('a')) close() })
    menu.addEventListener('keydown', (ev) => { if (ev.key === 'Escape') { ev.stopPropagation(); close(); button.focus() } })
    document.addEventListener('pointerdown', (ev) => { if (!menu.hidden && !ev.target.closest('.power-wrap')) close() })
  }

  // What the power menu can do to this sandbox, as vibe last said.
  let powerChecked = null

  // showPowerActions fills the menu's Stop and Restart from what vibe says
  // can be done: each is disabled, with the reason beside it, when it can't,
  // as it can't when vibe isn't there.
  async function showPowerActions() {
    $('power-actions').hidden = !state.power || !state.sandbox
    if (!state.power || !state.sandbox) return
    const set = (action, ok, why) => {
      const item = $('power-' + action)
      item.disabled = !ok
      item.querySelector('.power-why').textContent = ok ? '' : ' (' + why + ')'
    }
    if (offline.down) {
      set('stop', false, offlineText())
      set('restart', false, offlineText())
      return
    }
    try {
      const res = await api('/api/power')
      if (!res.ok || res.body.error) throw new Error((res.body && res.body.error) || 'vibe could not say')
      powerChecked = res.body
      for (const action of ['stop', 'restart']) {
        const can = (res.body.actions || {})[action] || { ok: false, why: 'vibe did not say' }
        set(action, can.ok, can.why || '')
      }
    } catch (e) {
      powerChecked = null
      set('stop', false, 'vibe could not be asked: ' + e.message)
      set('restart', false, 'vibe could not be asked: ' + e.message)
    }
  }

  // askPower asks before stopping or restarting a sandbox's vibe, naming
  // the sandbox and its folder, and what it would interrupt, then asks vibe
  // to and shows what it says as it goes. check is what vibe said of it.
  function askPower(action, sandbox, check) {
    const can = ((check || {}).actions || {})[action] || {}
    const folder = (check || {}).folder || ''
    const words = {
      stop: { title: 'Stop sandbox ', ing: 'Stopping', label: 'Stop',
        text: 'This ends vibe for ' + folder + ' and stops the sandbox.' + (sandbox === state.sandbox ? ' This page disconnects; vibe in that folder starts it again.' : '') },
      restart: { title: 'Restart vibe for sandbox ', ing: 'Restarting', label: 'Restart',
        text: 'This ends vibe for ' + folder + ' and starts it again, headless, on the same port.' + (sandbox === state.sandbox ? ' This page refreshes by itself when it is back.' : '') },
      start: { title: 'Start vibe for sandbox ', ing: 'Starting vibe for', label: 'Start',
        text: 'This starts a headless vibe for ' + folder + ', which can be talked to from its page. A terminal session is not something to start from here: run vibe in the folder for one.' },
    }[action]
    confirmThen({
      title: words.title + "'" + sandbox + "'?",
      text: words.text,
      list: can.warns || [],
      commandNote: '',
      danger: action !== 'start',
      runLabel: words.label,
      started: words.ing + " '" + sandbox + "'",
      run: async () => {
        // What the user was shown is sent too: if more would be interrupted now, vibe asks again.
        const res = await post('/api/power', { sandbox, action, confirmed: true, warns: can.warns || [] })
        if (res.ok) showPowerProgress(action, sandbox)
        return res
      },
    })
  }

  // askPowerForRow asks vibe what the action would interrupt for the sandbox
  // of a row of the Profiles tab, then asks the user.
  async function askPowerForRow(action, row) {
    let check = { folder: row.folder }
    try {
      const res = await api('/api/power?sandbox=' + encodeURIComponent(row.name))
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'vibe could not say what that would interrupt.', true); return }
      check = res.body
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
      return
    }
    askPower(action, row.name, check)
  }

  const power = { timer: null }
  // How many polls of the log (every 700ms) a restart gets before its box can be closed regardless: about two minutes.
  const POWER_SLOW_POLLS = 170
  // How long "Restarted" is shown before the page reloads by itself, to be read.
  const POWER_RELOAD_AFTER_MS = 2000

  // closeThisTab closes the tab, which a browser allows only for one a script opened: when this one stays (the next line
  // runs), it says so in into, rather than being a button that did nothing.
  function closeThisTab(into) {
    window.close()
    into.textContent = "Your browser won't let a page close its own tab: close this tab yourself."
  }

  // showPowerProgress shows what the action says as it runs, from the log
  // vibe keeps. It ends in one of three ways, told in words: vibe has gone
  // (a stop), vibe is back under a new boot (a restart), or the action ended
  // and vibe is as it was (it didn't take effect).
  function showPowerProgress(action, sandbox) {
    const dialog = $('power-progress')
    const ing = { stop: 'Stopping ', restart: 'Restarting ', start: 'Starting vibe for ' }[action]
    $('power-progress-title').textContent = ing + "'" + sandbox + "'"
    $('power-log').textContent = ''
    // Only this page's own vibe is ended by its own sandbox's stop or restart,
    // so only then does the page gone, or come back under a new boot, say how it ended.
    const own = sandbox === state.sandbox && action !== 'start'
    // Which run of vibe is asked, from the log's own answer: a restart is the
    // log answered under another boot, which the page's regular poll of the
    // state may not have seen yet.
    const boot = state.boot
    // This page's own vibe restarted is a new vibe, perhaps a new build: the page refreshes itself to meet it, once
    // it is up. Nothing is there to be pressed until then (a reload before would find nothing to load), so the box
    // says so, and has no way to close it: Escape included.
    const refreshes = own && action === 'restart'
    const closeButton = $('power-close')
    const reloadButton = $('power-reload')
    let closable = !refreshes
    const buttons = (close, reload) => { closeButton.hidden = !close; reloadButton.hidden = !reload; closable = close }
    buttons(!refreshes, false)
    closeButton.textContent = 'Close'
    $('power-end').textContent = refreshes ? 'This page will refresh by itself when vibe has restarted.' : ''
    dialog.oncancel = (ev) => { if (!closable) ev.preventDefault() }
    let next = 0
    let away = 0
    let ended = false
    let asking = false
    let polls = 0
    let reloadTimer = null
    const end = (text) => { ended = true; $('power-end').textContent = text; if (state.manage) loadManage() }
    clearInterval(power.timer)
    power.timer = setInterval(async function pollPower() {
      // One question at a time: a slow answer is not asked for again, which would add its text twice.
      if (ended || asking) return
      asking = true
      // A backstop, not an end: past it the person is told what to do if the page hasn't refreshed, and the restart carries on.
      polls += 1
      if (refreshes && !closable && polls >= POWER_SLOW_POLLS) {
        buttons(true, true)
        $('power-end').textContent = "This is taking longer than expected. If this page doesn't refresh by itself, reload it. The restart carries on."
      }
      try {
        const res = await api('/api/power/log?sandbox=' + encodeURIComponent(sandbox) + '&from=' + next)
        if (!res.ok) throw new Error('not ok')
        reached(true)
        away = 0
        if (res.body.text) {
          $('power-log').append(res.body.text)
          $('power-log').scrollTop = $('power-log').scrollHeight
        }
        next = res.body.next
        if (own && boot && res.body.boot && res.body.boot !== boot) {
          // Serving under a new boot isn't up: what started it (a restart) is done when the vibe has said its page is up
          // and its sandbox started, which is when the helper ends.
          if (action === 'restart' && !res.body.running) {
            end('Restarted: the new vibe is up and serving. Refreshing this page…')
            buttons(false, false)
            // Not at once, so that it can be read.
            reloadTimer = setInterval(function reloadSoon() { clearInterval(reloadTimer); location.reload() }, POWER_RELOAD_AFTER_MS)
          }
        } else if (!res.body.running && next > 0) {
          // Answered, by the same vibe, after what ran has finished: a stop that worked leaves nothing to answer.
          end(own && action === 'stop'
            ? "The stop didn't take effect: vibe is still running. What it said is above."
            : own
              ? "That ended and didn't take effect: vibe is as it was. What it said is above."
              : 'That has ended. What it said is above; the Profiles tab shows how the sandbox is now.')
          buttons(true, false)
        }
      } catch (e) {
        reached(false)
        away += 1
        if (own && action === 'stop' && away >= 2) {
          end('vibe has ended. What it said is above; the sandbox is stopped when it says so.')
          buttons(true, false)
          // Stopped on purpose, from here: Close closes the tab, as far as the browser lets it.
          offline.stoppedHere = true
          showGlass()
          closeButton.textContent = 'Close this tab'
          closeButton.onclick = () => closeThisTab($('power-end'))
        }
      } finally {
        asking = false
      }
    }, 700)
    closeButton.onclick = () => dialog.close()
    reloadButton.onclick = () => location.reload()
    dialog.onclose = () => { clearInterval(power.timer); clearInterval(reloadTimer) }
    dialog.showModal()
  }

  // The banner says what went wrong, each problem kept under what it came
  // from — the page's state, the conversation, or a tab's own load — so
  // that only that source's next success takes it away. A tab's problem is
  // shown on that tab; the state's and the conversation's on every tab, as
  // they're asked for on every tab.
  const problems = new Map()
  const EVERY_TAB = ['state', 'conversation']

  function banner(source, message) {
    if (message) problems.set(source, message)
    else problems.delete(source)
    showBanner()
  }

  function showBanner() {
    const shown = new Set()
    for (const [source, message] of problems) {
      if (EVERY_TAB.includes(source) || source === state.tab) shown.add(message)
    }
    const b = $('banner')
    b.hidden = shown.size === 0
    b.textContent = [...shown].join('\n')
  }

  // toast says message for a few seconds. With action ({ label, run }), it
  // carries a button, and stays longer to give time to press it — and for
  // as long as the pointer is on it or the button has focus.
  function toast(message, bad, action) {
    const t = $('toast')
    t.replaceChildren(message)
    if (action) {
      t.append(' ', el('button', { type: 'button', class: 'toast-action', text: action.label,
        onclick: () => { hideToast(); action.run() } }))
    }
    t.className = 'toast' + (bad ? ' bad' : '')
    t.hidden = false
    raiseToast()
    toast.lasts = action ? 10000 : bad ? 8000 : 4000
    toast.held = false
    clearTimeout(toast.timer)
    toast.timer = setTimeout(hideToast, toast.lasts)
  }

  function hideToast() {
    clearTimeout(toast.timer)
    const t = $('toast')
    t.hidden = true
    if (t.hidePopover && t.matches(':popover-open')) t.hidePopover()
  }

  // raiseToast puts the toast over everything: in the browser's top layer,
  // above whatever went there before it (a dialog, say), so nothing on the
  // page ever covers it. (Without popovers, its z-index does what it can.)
  function raiseToast() {
    const t = $('toast')
    if (t.hidden || !t.showPopover) return
    if (t.matches(':popover-open')) t.hidePopover()
    t.showPopover()
  }

  function holdToast(held) {
    if (held === toast.held || $('toast').hidden) return
    toast.held = held
    clearTimeout(toast.timer)
    if (!held) toast.timer = setTimeout(hideToast, toast.lasts)
  }

  // showProgress brings the Last action box with id card into view, with
  // focus on it, so a keyboard user is taken there too. The box shows once
  // the page has heard of the job, which may not be yet.
  async function showProgress(card, load) {
    if ($(card).hidden) await load()
    const heading = $(card).querySelector('h2')
    $(card).scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    heading.focus({ preventScroll: true })
  }

  // showAttention marks each tab attention names with a dot, and says why
  // in the tab's tooltip; a tab it doesn't name loses both. The reasons come
  // with the dot, so there's no dot without them.
  function showAttention(attention, pulsing) {
    for (const tab of document.querySelectorAll('[role=tab]')) {
      const reasons = Array.isArray(attention[tab.dataset.tab]) ? attention[tab.dataset.tab] : []
      let dot = tab.querySelector('.tab-dot')
      if (!reasons.length) {
        if (dot) dot.remove()
        tab.removeAttribute('title')
        continue
      }
      if (!dot) tab.append(dot = el('span', { class: 'tab-dot', 'aria-hidden': 'true' }))
      dot.classList.toggle('working', !!pulsing && pulsing.has(tab.dataset.tab))
      tab.title = reasons.join('\n')
    }
  }

  // showAllAttention draws the dots vibe asked for, with the page's own:
  // the Review tab's, when the agent has updated the review, and the
  // Conversation tab's, when it has said something out of sight (pulsing
  // while it's still at work).
  function showAllAttention() {
    renderBlockedNotice()
    const attention = withBlockedNews(withReviewNews(state.attention))
    if (!convo.news) { showAttention(attention); return }
    showAttention(Object.assign({}, attention, { conversation: [convo.running ? 'The agent is working' : 'The agent has answered'] }),
      convo.running ? new Set(['conversation']) : null)
  }

  // REVIEW_SEEN keeps when the agent last updated the review as the user
  // has seen it, per browser (and per sandbox: storage is per origin).
  const REVIEW_SEEN = 'vibe-companion-review-seen'

  // withReviewNews is attention with the Review tab's, when the agent has
  // updated the review since the user last saw it.
  function withReviewNews(attention) {
    if (!R.agentUpdated(state.reviewAt, store.get(REVIEW_SEEN))) return attention
    return Object.assign({}, attention, { review: (attention.review || []).concat('The agent has updated the review') })
  }

  // reviewSeen takes the dot off the Review tab: the user is looking at
  // what's there.
  function reviewSeen() {
    if (!state.loaded || !R.agentUpdated(state.reviewAt, store.get(REVIEW_SEEN))) return
    store.set(REVIEW_SEEN, state.reviewAt)
    showAllAttention()
  }

  // BLOCKED_SEEN keeps the hosts the user has seen blocked and left that way,
  // as a JSON list, per browser (and per sandbox: storage is per origin). It's
  // what makes a dot on the Network log tab mean a host that's new: one the
  // user has looked at and left blocked, or chose to block, isn't news.
  const BLOCKED_SEEN = 'vibe-companion-blocked-seen'

  function seenBlocked() {
    const stored = store.get(BLOCKED_SEEN)
    if (stored === null || stored === undefined || stored === '') return null
    try {
      const list = JSON.parse(stored)
      return Array.isArray(list) ? list : null
    } catch (e) { return null }
  }

  // BLOCKED_DISMISSED keeps the hosts the user said not to be told of again, as a JSON list, per
  // browser and sandbox. Not forgotten when a host stops being blocked: one called every few
  // minutes is blocked again each time, and isn't news each time.
  const BLOCKED_DISMISSED = 'vibe-companion-blocked-dismissed'
  const MAX_DISMISSED = 500

  function dismissedBlocked() {
    try {
      const list = JSON.parse(store.get(BLOCKED_DISMISSED) || '[]')
      return Array.isArray(list) ? list : []
    } catch (e) { return [] }
  }

  // noteBlocked works out which of the hosts vibe says are blocked are new.
  // Not known yet (null) is not "none are": nothing is taken as seen. With
  // nothing kept (a browser's first visit), what's blocked now is taken as
  // seen: a long history isn't news. A host that stops being blocked is
  // forgotten, so being blocked again is news. And what's blocked while the
  // Network log tab is open is being looked at.
  function noteBlocked(hosts) {
    state.newBlocked = []
    if (!Array.isArray(hosts)) return
    const seen = seenBlocked()
    if (seen === null) { store.set(BLOCKED_SEEN, JSON.stringify(hosts)); return }
    const kept = state.tab === 'log' ? hosts : seen.filter((h) => hosts.includes(h))
    if (kept.length !== seen.length || kept.some((h, i) => h !== seen[i])) store.set(BLOCKED_SEEN, JSON.stringify(kept))
    const dismissed = dismissedBlocked()
    state.newBlocked = hosts.filter((h) => !kept.includes(h) && !dismissed.includes(h))
  }

  function dismissBlocked() {
    const hosts = state.newBlocked || []
    if (!hosts.length) return
    store.set(BLOCKED_DISMISSED, JSON.stringify(dismissedBlocked().concat(hosts).slice(-MAX_DISMISSED)))
    noteBlocked(state.blockedHosts)
    showAllAttention()
  }

  // The conversation says too that hosts are newly blocked, as it is there the agent will have said it
  // couldn't get somewhere, and the Network log tab is where to let it.
  function renderBlockedNotice() {
    const hosts = state.newBlocked || []
    $('convo-blocked').hidden = hosts.length === 0
    if (!hosts.length) return
    const named = hosts.slice(0, 3).join(', ') + (hosts.length > 3 ? ' and ' + (hosts.length - 3) + ' more' : '')
    $('convo-blocked-text').textContent = (hosts.length === 1 ? '1 host the sandbox tried to reach is blocked: ' : hosts.length + ' hosts the sandbox tried to reach are blocked: ') +
      named + '. You can unblock ' + (hosts.length === 1 ? 'it' : 'them') + ' in the Network log tab.'
  }

  // withBlockedNews is attention with the Network log tab's, when hosts are
  // blocked that the user hasn't seen.
  function withBlockedNews(attention) {
    const hosts = state.newBlocked || []
    if (!hosts.length) return attention
    const named = hosts.slice(0, 3).join(', ') + (hosts.length > 3 ? ' and ' + (hosts.length - 3) + ' more' : '')
    const text = hosts.length + ' new blocked host' + (hosts.length === 1 ? '' : 's') + ': ' + named
    return Object.assign({}, attention, { log: (attention.log || []).concat(text) })
  }

  // blockedSeen takes the dot off the Network log tab: the user is looking at
  // what's blocked.
  function blockedSeen() {
    if (!(state.newBlocked || []).length) return
    noteBlocked(state.blockedHosts)
    showAllAttention()
  }

  // reached is called with the outcome of every call to vibe: two failures
  // in a row mean vibe has gone (its session ended), and it says so; the
  // next success puts everything back.
  function reached(ok) {
    if (ok) {
      state.failures = 0
      setLink('up', 'connected')
      disconnected(false)
      return
    }
    state.failures += 1
    if (state.failures >= 2) {
      setLink('down', offlineText())
      disconnected(true)
    }
  }

  function offlineText() {
    return state.sandboxTabs === false
      ? "vibe --manage isn't running — run it again, then this page reconnects"
      : 'vibe is not running for this sandbox — run vibe again, then this page reconnects'
  }

  // What the page shows while vibe isn't there: a cross on its icon, and
  // frosted glass over everything, as nothing on it can work. The glass can
  // be dismissed until vibe is back; the cross and the status stay.
  const offline = { down: false, dismissed: false, stoppedHere: false }
  const BEHIND_GLASS = ['.top', '#tabs', '#banner', 'main']

  function disconnected(down) {
    if (down === offline.down) return
    if (down) started()
    offline.down = down
    offline.dismissed = false
    favicon.down = down
    drawFavicon()
    if (!down) offline.stoppedHere = false
    if (down) {
      // Not the progress of a stop or restart, which is what is watched while vibe is away.
      for (const d of document.querySelectorAll('dialog[open]')) if (d.id !== 'power-progress') d.close('cancel')
    }
    showGlass()
  }

  // The same glass, in the page from the start, says it's starting until
  // there's something to see: lifted once the tab it opens on has its data,
  // or when there's an error to see, or Disconnected to say instead.
  function started() { $('starting').hidden = true }

  function startedIfReady() {
    if (!state.loaded || $('panel-' + state.tab).hidden) return
    if (state.tab === 'conversation' && !convo.loaded) return
    started()
  }

  function showGlass() {
    const shown = offline.down && !offline.dismissed
    $('offline').hidden = !shown
    // What it says depends on whether the user stopped vibe from here, which may be known only after it went.
    $('offline-dismiss').textContent = offline.stoppedHere ? 'Close this tab' : 'Dismiss until it reconnects'
    if (offline.down) {
      $('offline-text').textContent = offline.stoppedHere
        ? 'You stopped vibe from this page. You can close this tab; run vibe again to come back.'
        : offlineText()
    }
    for (const sel of BEHIND_GLASS) {
      const node = document.querySelector(sel)
      if (node) node.inert = shown
    }
    if (shown) $('offline-dismiss').focus()
  }

  // Asked every few seconds whatever tab is showing, and in the background
  // too: a page out of sight is just when its icon has to say vibe has gone.
  // The ping needs no token and does no work; it's ours only if it's this
  // page's vibe answering.
  const PING_MS = 3000
  async function ping() {
    try {
      const res = await api('/api/ping', { timeout: PING_MS })
      reached(res.ok && res.body.app === 'vibe-companion' && (!state.loaded || res.body.sandbox === state.sandbox))
    } catch (e) {
      reached(false)
    }
  }

  function problem(source, res) {
    if (res.status === 401) {
      banner(source, "This page doesn't have the right token. Open the URL vibe printed (it ends in #…), or run vibe again.")
      return true
    }
    if (res.body && res.body.error) {
      banner(source, res.body.code === 'not-signed-in'
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
        banner(kind, res.body.unresolved || '')
        state[kind] = res.body
        render(kind)
      } else {
        problem(kind, res)
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
    const part = (cls, text, title) => {
      const span = document.createElement('span')
      span.className = cls
      span.textContent = text
      span.title = title
      box.append(span)
    }
    const files = (n, what) => n + ' ' + what + ' file' + (n === 1 ? '' : 's')
    // The glyphs and the order are the command-line prompt's (powerline's), so that what is read here is read as there.
    if (git.detached) part('branch detached', git.branch, 'detached HEAD at ' + git.branch.replace(/^@/, '') + ': not on a branch')
    else part('branch', git.branch, 'branch ' + git.branch)
    if (git.operation) part('operation', git.operation, git.operation + ' under way')
    if (git.staged) part('staged', '+' + git.staged, files(git.staged, 'staged'))
    if (git.modified) part('modified', '!' + git.modified, files(git.modified, 'modified or deleted, not staged'))
    if (git.untracked) part('untracked', '?' + git.untracked, files(git.untracked, 'untracked'))
    if (git.conflicted) part('conflicted', '✘' + git.conflicted, files(git.conflicted, 'conflicted'))
    if (git.ahead) part('ahead', '⇡' + git.ahead, git.ahead + ' commit' + (git.ahead === 1 ? '' : 's') + ' not pushed')
    if (git.behind) part('behind', '⇣' + git.behind, git.behind + ' commit' + (git.behind === 1 ? '' : 's') + ' fetched but not merged')
    if (git.stashed) part('stashed', '#' + git.stashed, git.stashed + ' stash' + (git.stashed === 1 ? '' : 'es'))
    if (git.noUpstream) part('noupstream', 'no upstream', 'this branch follows no branch, so there is nothing for it to be ahead of or behind')
  }

  async function loadState() {
    try {
      const res = await api('/api/state')
      reached(true)
      if (!res.ok) { problem('state', res); started(); return }
      banner('state', '')
      state.sandbox = res.body.sandbox || ''
      state.loaded = true
      state.sandboxTabs = res.body.sandboxTabs !== false
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
      state.viewFiles = !!res.body.viewFiles
      state.choice = state.conversation && !!res.body.choice
      $('convo-choice').hidden = !state.choice
      if (state.choice) initChoice()
      state.admin = !!res.body.admin
      state.power = !!res.body.power
      state.manage = !!res.body.manage
      state.boot = res.body.boot || ''
      state.usage = !!res.body.usage
      state.signin = !!res.body.signin
      state.signinStart = !!res.body.signinStart
      $('admin-signin').hidden = !state.signin
      if (state.signin && !signInShown.asked) loadSignIn()
      $('usage').hidden = !state.usage
      $('tab-admin').hidden = !state.admin
      $('tab-manage').hidden = !res.body.manage
      $('tab-review').hidden = !res.body.review
      for (const t of SANDBOX_TABS) $('tab-' + t).hidden = res.body.sandboxTabs === false
      state.attention = res.body.attention || {}
      renderPackagesWaiting(res.body.packagesWaiting || 0)
      renderSyncWaiting(res.body.packagesToFetch || 0)
      renderStaleWaiting(res.body.packagesStale || 0, res.body.packagesStaleProjects || [])
      state.blockedHosts = res.body.blockedHosts
      noteBlocked(state.blockedHosts)
      state.reviewAt = res.body.review ? res.body.reviewAt || '' : ''
      showAllAttention()
      // A page left open across a rebuild of vibe is the old page: say so.
      if (res.body.pageVersion) {
        if (!state.pageVersion) state.pageVersion = res.body.pageVersion
        $('page-outdated').hidden = res.body.pageVersion === state.pageVersion
      }
      const moved = url !== state.clipboardUrl
      if (moved || ready !== state.clipboardReady) {
        if (url && !ready && (moved || state.clipboardReady)) {
          state.clipboardWaitingSince = Date.now()
        }
        if (moved) state.clipboardLoaded = ''
        state.clipboardUrl = url
        state.clipboardReady = ready
        for (const view of CLIPBOARD_VIEWS) {
          if (url) $(view.open).href = url
          // The frame only gets the page once vibe has seen it answer: a
          // frame on another origin can't be watched, and a browser's
          // "can't connect" page would otherwise sit there for good. Once
          // it has the page, it keeps it while the address stays the same:
          // the page carries on by itself when its server comes back (the
          // sandbox restarted, say), where loading it again would flash.
          if (ready && state.clipboardLoaded !== url) $(view.frame).src = framedClipboardUrl(url)
          else if (!url || (moved && !ready)) $(view.frame).removeAttribute('src')
        }
        if (ready) state.clipboardLoaded = url
      }
      renderGitAuthor(res.body.gitAuthor)
      renderClipboard()
      if ($('tab-' + state.tab).hidden) select(state.tab, false)
    } catch (e) {
      reached(false)
    }
  }

  // --- the agent's git author -------------------------------------------------

  // Git won't commit without an author, and a sandbox can come without one. The host asks whether to give the
  // agent the user's own: a dialog over the whole page, which can't be put away without choosing (that is why
  // it's judicious: nothing else on the page is held up like this); "later" leaves a reminder in the
  // conversation, which can be dismissed until the sandbox is started again.
  const gitAuthor = { sending: false }

  function renderGitAuthor(g) {
    const phase = (g && g.phase) || ''
    const dialog = $('git-author')
    $('convo-git-author').hidden = phase !== 'reminder'
    if (phase !== 'ask') {
      if (dialog.open) dialog.close()
      return
    }
    $('git-author-shown').textContent = g.shown || ''
    $('git-author-push').textContent = g.push || ''
    if (!dialog.open) dialog.showModal()
  }

  async function answerGitAuthor(answer) {
    if (gitAuthor.sending) return
    gitAuthor.sending = true
    const buttons = ['give', 'leave', 'later'].map((a) => $('git-author-' + a))
    buttons.forEach((b) => { b.disabled = true })
    $('git-author-problem').hidden = true
    try {
      const res = await post('/api/git-author', { answer })
      reached(true)
      if (res.ok) {
        renderGitAuthor(res.body.gitAuthor)
        // What was kept, and how to undo it: long, so with a button, which keeps it up longer.
        if (res.body.kept) toast(res.body.kept, false, { label: 'OK', run() {} })
      }
      else if (res.status === 409) {
        toast(res.body.error || 'That is no longer being asked.', true)
        await loadState()
      } else {
        $('git-author-problem').textContent = (res.body && res.body.error) || 'That could not be done.'
        $('git-author-problem').hidden = false
      }
    } catch (e) {
      reached(false)
      $('git-author-problem').textContent = 'Could not reach vibe.'
      $('git-author-problem').hidden = false
    } finally {
      gitAuthor.sending = false
      buttons.forEach((b) => { b.disabled = false })
    }
  }

  $('git-author').addEventListener('cancel', (ev) => ev.preventDefault())
  for (const answer of ['give', 'leave', 'later']) $('git-author-' + answer).addEventListener('click', () => answerGitAuthor(answer))
  $('convo-git-author-fix').addEventListener('click', () => answerGitAuthor('fix'))
  $('convo-git-author-dismiss').addEventListener('click', () => answerGitAuthor('dismiss'))

  function renderClipboard() {
    // A pane already showing the page keeps it while its server is away.
    const kept = !!state.clipboardUrl && state.clipboardLoaded === state.clipboardUrl
    for (const view of CLIPBOARD_VIEWS) {
      $(view.note).hidden = !kept || state.clipboardReady
      $(view.note).textContent = "— not answering just now; it picks up again by itself"
      $(view.note).title = $(view.note).textContent
    }
    const waiting = !!state.clipboardUrl && !state.clipboardReady && !kept
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

  // How often the page, while in view, asks vibe to keep the sandbox awake:
  // well inside the minute after which sbx stops one nothing is attached to.
  const AWAKE_MS = 20000
  let awakeAsked = 0

  // unlessBusy runs load unless the last one of its name hasn't been answered:
  // vibe can be slow (a sandbox with every CPU busy), and a poll started
  // every couple of seconds on top of those unanswered would use up the
  // browser's few connections to it, so that even the ping couldn't get
  // through, and the page would say vibe had gone.
  const polling = new Set()
  function unlessBusy(name, load) {
    if (polling.has(name)) return Promise.resolve()
    polling.add(name)
    return Promise.resolve(load()).finally(() => polling.delete(name))
  }

  function tick() {
    if (document.visibilityState !== 'visible') return
    // Only in view: out of sight, sbx is left to stop an idle sandbox, and
    // coming back asks at once (it starts it again, if it was stopped).
    if (state.sandboxTabs !== false && Date.now() - awakeAsked >= AWAKE_MS) {
      awakeAsked = Date.now()
      // Whether vibe is there at all is the ping's to say.
      post('/api/awake', {}).catch(() => {})
    }
    const loads = state.tab === 'conversation' ? [unlessBusy('state', loadState)]
      : state.tab === 'admin' ? [unlessBusy('state', loadState), unlessBusy('admin', loadAdmin), unlessBusy('packages', loadPackages), unlessBusy('sync', loadSync), unlessBusy('instructions', () => loadInstructions(false))]
      : state.tab === 'manage' ? [unlessBusy('state', loadState), unlessBusy('manage', loadManage)]
      : state.tab === 'review' ? [unlessBusy('state', loadState), unlessBusy('review', () => loadReview(reviewView.files === null))]
      : [unlessBusy('state', loadState), unlessBusy('tab ' + state.tab, () => load(state.tab))]
    if (state.usage) loads.push(unlessBusy('usage', loadUsage))
    if (gate.active && gate.fetching) loads.push(unlessBusy('gate', refreshGate))
    Promise.allSettled(loads).then(startedIfReady)
  }

  // --- tabs -------------------------------------------------------------

  // select shows tab, or the first tab the page has when it hasn't that one.
  function select(tab, remember = true) {
    const wanted = tab
    if (!TABS.includes(tab) || $('tab-' + tab).hidden) tab = TABS.find((t) => !$('tab-' + t).hidden) || 'log'
    state.tab = tab
    // Only a tab that was asked for is a choice to keep: where it wasn't there to go to (no conversation yet, on a first
    // open), the one stood in for isn't, or it would stay the choice once the conversation is.
    if (remember && tab === wanted) store.set('vibe-companion-tab', tab)
    showBanner()
    for (const t of TABS) {
      $('panel-' + t).hidden = t !== tab
      const button = $('tab-' + t)
      button.setAttribute('aria-selected', String(t === tab))
      button.tabIndex = t === tab ? 0 : -1
    }
    if (tab === 'log') blockedSeen()
    if (tab === 'admin' && state.signin) loadSignIn()
    if (tab === 'review') reviewSeen()
    // A comment asked for, not yet gone to, isn't gone to once you're elsewhere.
    else reviewView.goTo = null
    if (tab === 'conversation' && convo.news) { convo.news = false; showAllAttention() }
    // Opening the conversation is to say something: the cursor goes in the
    // prompt box. (Arrowing along the tabs keeps focus on the tabs.)
    if (tab === 'conversation') $('convo-input').focus({ preventScroll: true })
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
  }

  // namesOneHost says whether a logged host is a single host (with a port,
  // perhaps), not a pattern: what the sandbox asked to reach can be one of
  // the agent's making, and allowing it would let in far more than it says.
  function namesOneHost(host) {
    return !/[*?[\]!/]/.test(host.replace(/^\[[0-9A-Fa-f:.]+\]/, ''))
  }

  function actionFor(entry) {
    if (entry.decision === 'blocked') {
      if (!namesOneHost(entry.host)) return null
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
      note.textContent = "Read-only: sbx doesn't log filesystem access yet, so there are only the rules to show."
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
      command: 'sbx policy rm network --sandbox ' + state.sandbox + ' --id ' + rule.id + ' --force',
      danger: true,
      run: () => post('/api/remove', { id: rule.id }),
    })
  }

  // post sends payload to path as JSON; options are api's.
  function post(path, payload, options) {
    return api(path, Object.assign({}, options, { method: 'POST', body: JSON.stringify(payload) }))
  }

  function confirmThen(opts) {
    const dialog = $('confirm')
    $('confirm-title').textContent = opts.title
    $('confirm-text').textContent = opts.text
    const list = opts.list || []
    $('confirm-list').hidden = !list.length
    $('confirm-list').replaceChildren(...list.map((line) => el('li', { text: line })))
    $('confirm-command-note').textContent = opts.commandNote == null ? 'This runs, on your machine:' : opts.commandNote
    $('confirm-command-note').hidden = opts.commandNote === ''
    $('confirm-command').textContent = opts.command
    $('confirm-command').hidden = !opts.command
    // safe: what it confirms is destructive, so Cancel is the obvious
    // choice: on the right, focused, and the one that looks like the action.
    const actions = dialog.querySelector('.actions')
    const cancel = actions.querySelector('[value=cancel]')
    const run = $('confirm-run')
    actions.classList.toggle('safe', !!opts.safe)
    if (opts.safe) actions.append(run, cancel)
    else actions.append(cancel, run)
    cancel.className = opts.safe ? '' : 'ghost'
    cancel.autofocus = !!opts.safe
    run.autofocus = !opts.safe
    run.className = opts.danger || opts.safe ? 'danger' : ''
    run.textContent = opts.runLabel || 'Run'
    dialog.returnValue = ''
    dialog.onclose = async () => {
      if (dialog.returnValue !== 'run') {
        if (opts.cancelled) opts.cancelled()
        return
      }
      try {
        const res = await opts.run()
        reached(true)
        // asked: run asked something else instead, which says what's what.
        if (res.asked) return
        // warning: done, but not all of it (see the server's changing).
        if (res.ok && res.body.warning) {
          toast(res.body.warning, true)
        } else if (res.ok) {
          toast(opts.started ? opts.started : 'Done: ' + (res.body.command || opts.command), false,
            opts.progress ? { label: 'Show progress', run: opts.progress } : null)
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
    // A toast showing stays over the dialog.
    raiseToast()
  }

  // --- the default branch, when origin can't say ---------------------------

  // defaultBranchSetter offers to set the repository's default branch, for
  // when origin can't say which it is (see docs/plan-new-conversation.md):
  // the name last set is suggested, and the repository's branches offered.
  // set is called with what vibe then says of it.
  let defaultBranchSetters = 0
  function defaultBranchSetter(why, set) {
    const listId = 'default-branches-' + (++defaultBranchSetters)
    const input = el('input', { type: 'text', list: listId, spellcheck: 'false', autocomplete: 'off', 'aria-label': 'The default branch' })
    const list = el('datalist', { id: listId })
    const error = el('p', { class: 'note bad-text', role: 'alert', hidden: true })
    const button = el('button', { type: 'button', class: 'ghost', text: 'Set' })
    const fail = (text) => { error.textContent = text; error.hidden = false }
    button.addEventListener('click', async () => {
      if (!input.value.trim()) { fail('Name the default branch.'); return }
      error.hidden = true
      button.disabled = true
      let res
      try {
        // It may wake the sandbox, to set it there.
        res = await post('/api/default-branch', { name: input.value }, { timeout: 4 * 60 * 1000 })
        reached(true)
      } catch (e) {
        reached(false)
        res = { ok: false, body: { error: 'Could not reach vibe.' } }
      }
      button.disabled = false
      if (!res.ok) { fail(res.body.error || 'That failed.'); return }
      if (res.body.warning) toast(res.body.warning, true)
      set(res.body)
    })
    // Enter sets it: in a form, it would press the form's first button.
    input.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.isComposing) { ev.preventDefault(); button.click() }
    })
    // The suggestions are a help, not a need: without them, a name can
    // still be typed.
    api('/api/default-branch').then((res) => {
      if (!res.ok) return
      if (!input.value) input.value = res.body.preferred || ''
      list.replaceChildren(...(res.body.branches || []).map((b) => el('option', { value: b })))
    }, () => {})
    return el('div', { class: 'default-setter' },
      el('p', { class: 'muted note', text: why }),
      el('div', { class: 'default-setter-row' }, el('span', { class: 'muted', text: 'Set it for this repository:' }), input, button),
      list, error)
  }

  // --- the Review tab -----------------------------------------------------

  // The review, and the diff it's of: the diff is worked out when the tab
  // is first shown and on Refresh; polls fetch the review alone (git on a
  // large repository every couple of seconds would be costly). drafts are
  // replies being typed, by thread, kept across redraws.
  // layout is 'split' (side by side, the default) or 'unified' (one column,
  // for a narrow window), kept for this browser.
  const REVIEW_LAYOUT = 'vibe-review-layout'
  // paneThreads are the review's threads the comments pane shows, in its
  // order: as the comment arrows go (the page's order, or the reading
  // order), or by their latest comment, newest first.
  function paneThreads() {
    const rv = reviewView.review
    if (!rv) return []
    const pane = reviewView.pane
    const byId = new Map(rv.threads.map((t) => [t.id, t]))
    const onPage = Array.from($('panel-review').querySelectorAll('.review-thread')).map((card) => {
      const file = card.closest('.review-file')
      return { file: file ? file.dataset.path : null, id: Number(card.id.replace('review-thread-', '')) }
    })
    let ids = (reviewView.order === 'reading' ? R.inStepOrder(onPage, reviewSteps()) : onPage).map((e) => e.id)
    ids = ids.concat(rv.threads.map((t) => t.id).filter((id) => !ids.includes(id)))
    let threads = ids.map((id) => byId.get(id)).filter(Boolean)
    if (pane.filter === 'open') threads = threads.filter((t) => t.status === 'open')
    if (pane.filter === 'yours') threads = threads.filter((t) => t.comments.length && t.comments[0].author === 'user')
    if (pane.order === 'newest') {
      const latest = (t) => t.comments.reduce((m, c) => (c.at > m ? c.at : m), '')
      threads = threads.map((t, i) => ({ t, i })).sort((a, b) => latest(b.t).localeCompare(latest(a.t)) || a.i - b.i).map((x) => x.t)
    }
    return threads
  }

  // drawCommentsPane draws the comments pane's cards: each thread, where
  // it is, its state, how it starts, and how many replies it has; one the
  // agent had the last word on, still open, is marked as awaiting your answer.
  function drawCommentsPane() {
    const rv = reviewView.review
    $('review-comments-count').textContent = String(rv ? rv.threads.length : 0)
    const pane = reviewView.pane
    for (const b of $('review-comments').querySelectorAll('[data-filter]')) b.setAttribute('aria-pressed', String(b.dataset.filter === pane.filter))
    // Path and reading order are the comment arrows' own (as chosen under the
    // count); newest first is the pane's alone.
    const order = $('review-comments-order')
    order.querySelector('[value=reading]').disabled = reviewSteps().size === 0
    order.value = pane.order === 'newest' ? 'newest' : reviewView.order
    const threads = paneThreads()
    $('review-comments-list').replaceChildren(...(threads.length ? threads.map((t) => {
      const first = t.comments[0] || { author: '', body: '' }
      const replies = t.comments.length - 1
      const where = t.file ? t.file + ':' + t.line + (t.endLine > t.line ? '–' + t.endLine : '') + (t.side === 'old' ? ' (old)' : '') : 'on the whole review'
      const last = t.comments[t.comments.length - 1]
      const awaiting = t.status === 'open' && !!last && last.author === 'agent'
      return el('button', { type: 'button', class: 'comment-card ' + t.status + (awaiting ? ' awaiting' : ''), 'data-thread': String(t.id), onclick: () => goToThread(t.id) },
        el('span', { class: 'comment-card-where' }, el('code', { text: where }),
          t.status === 'open' ? null : el('span', { class: 'pill', text: t.status }),
          awaiting ? el('span', { class: 'pill warn', text: 'awaiting your answer' }) : null),
        el('span', { class: 'comment-card-said' }, el('strong', { text: first.author === 'agent' ? 'Agent: ' : 'You: ' }), first.body.replace(/\s+/g, ' ').slice(0, 160)),
        replies ? el('span', { class: 'comment-card-replies muted', text: plural(replies, 'reply', 'replies') }) : null)
    }) : [el('p', { class: 'muted', text: pane.filter === 'all' ? 'No comments yet.' : 'None of these.' })]))
  }

  function openCommentsPane(open) {
    reviewView.pane.open = open
    const pane = $('review-comments')
    pane.classList.toggle('open', open)
    pane.setAttribute('aria-hidden', String(!open))
    pane.inert = !open
    $('review-comments-open').setAttribute('aria-expanded', String(open))
    if (open) {
      // Just below the review's bar, wherever it is now.
      $('panel-review').style.setProperty('--review-pane-top', Math.max(0, $('panel-review').querySelector(':scope > .bar').getBoundingClientRect().bottom) + 'px')
      drawCommentsPane()
      const first = pane.querySelector('.comment-card')
      if (first) first.focus({ preventScroll: true })
    }
  }

  // goToThread closes the comments pane and goes to thread id: its reply
  // box ready. A tree filter hiding it is cleared; one in a file ticked as
  // viewed stays folded, saying so.
  function goToThread(id) {
    openCommentsPane(false)
    let card = $('review-thread-' + id)
    if (card && card.closest('.review-file[hidden]')) {
      const filter = $('review-filter')
      filter.value = ''
      filter.dispatchEvent(new Event('input', { bubbles: true }))
      card = $('review-thread-' + id)
    }
    if (!card) return
    if (card.closest('[hidden]')) { toast("That comment is in a file you've ticked as viewed: untick it to see it."); return }
    const i = arrowCards().indexOf(card)
    if (i >= 0) { reviewView.at = i; reviewView.steering = true; countComments() }
    card.scrollIntoView({ behavior: 'smooth', block: 'start' })
    ;(card.querySelector('textarea[data-thread]') || card).focus({ preventScroll: true })
  }

  function setUpCommentsPane() {
    const pane = $('review-comments')
    $('review-comments-open').addEventListener('click', () => openCommentsPane(!reviewView.pane.open))
    $('review-comments-close').addEventListener('click', () => { openCommentsPane(false); $('review-comments-open').focus() })
    for (const b of pane.querySelectorAll('[data-filter]')) b.addEventListener('click', () => { reviewView.pane.filter = b.dataset.filter; drawCommentsPane() })
    $('review-comments-order').addEventListener('change', (ev) => {
      if (ev.target.value === 'newest') { reviewView.pane.order = 'newest'; drawCommentsPane(); return }
      reviewView.pane.order = 'arrows'
      setCommentOrder(ev.target.value)
    })
    pane.addEventListener('keydown', (ev) => { if (ev.key === 'Escape') { ev.stopPropagation(); openCommentsPane(false); $('review-comments-open').focus() } })
    document.addEventListener('keydown', (ev) => {
      if (ev.key === 'Escape' && reviewView.pane.open && !typingInto(ev.target)) openCommentsPane(false)
    })
  }

  // REVIEW_ORDER keeps, per browser, the order the comment arrows go in.
  const REVIEW_ORDER = 'vibe-review-comment-order'
  // REVIEW_REPLY_NEXT keeps, per browser, whether Ctrl+Enter in a reply box
  // goes on to the next comment after replying ('off' if not).
  const REVIEW_REPLY_NEXT = 'vibe-review-reply-next'
  // REVIEW_WHITESPACE keeps, per browser, whether the diff leaves out
  // changes of whitespace alone ('ignore').
  const REVIEW_WHITESPACE = 'vibe-review-whitespace'
  // The file filter is kept for this browser tab, as the Profiles tab's is.
  const REVIEW_FILTER = 'vibe-review-filter'
  // fileLines are files' lines, fetched the first time one of their gaps
  // is opened (and again after Refresh); opened is how far each gap has
  // been opened, by file, then gap: { down, up }.
  const reviewView = { review: null, files: null, diffError: '', key: '', drafts: new Map(), loading: false,
    layout: store.get(REVIEW_LAYOUT) === 'unified' ? 'unified' : 'split', fileLines: new Map(), linesAsked: new Set(), opened: new Map(),
    filter: sessionGet(REVIEW_FILTER), collapsed: new Set(),
    // composer is the comment being written on a line ({ path, side, line,
    // code, text, mode: 'write' or 'preview' }); editing, the unsent comment
    // being edited ({ thread, at: when it was said, text }); ask, the
    // thread last asked about and whether the agent is still answering it.
    composer: null, editing: null, ask: null,
    // agentAtWork is vibe saying the agent is at a turn the review began
    // (its start, a Submit or an Ask): closing the review stops it.
    agentAtWork: false,
    // announced is the review's start and state as last announced by toast (see announceReview).
    announced: null,
    // options are the reviews that can be started, while none is open
    // (null until known), or why they couldn't be told.
    options: null, optionsError: '',
    // earlier is the edited comments (by when they were said) showing what
    // they said before.
    earlier: new Set(),
    // pane is the comments pane: open, which threads it shows ('all',
    // 'open', 'yours': those you started) and in what order ('arrows', as
    // the comment arrows go, or 'newest').
    pane: { open: false, filter: 'all', order: 'arrows' },
    // colours is each file side's colouring, by "path|side", as vibe gave
    // it ({ language, lines, skipped }), or { loading } / { error }.
    colours: new Map(),
    // at is which of the shown threads the page is at, for the bar's
    // "3/5"; steering, that the arrows put it there, so the next step goes
    // on from it (the last threads can't always be scrolled up to the bar),
    // until the user scrolls themselves.
    at: 0, steering: false,
    // current is the file the page is in, marked in the tree (null above
    // the first); fileSteering, that a click in the tree set it, until the
    // user scrolls themselves.
    current: null, fileSteering: false,
    // shown is the lines on the page, by "path|side": line number to text.
    shown: new Map(),
    // order is the order the comment arrows go in: 'path', as the files
    // are shown, or 'reading', by the agent's steps.
    order: store.get(REVIEW_ORDER) === 'reading' ? 'reading' : 'path',
    // replyNext: Ctrl+Enter in a reply box goes on to the next comment.
    replyNext: store.get(REVIEW_REPLY_NEXT) !== 'off',
    // changed counts the review vibe gave back for something done on the
    // page: a poll that set out before it brings an older review, which is
    // dropped (see tookReview).
    changed: 0,
    // diffWanted: the diff was asked for while a load was on its way, to
    // load once it's in: when that load wasn't of the diff, or the diff it
    // set out for has gone stale since (epoch: see forgetFiles).
    diffWanted: false, loadingDiff: false, epoch: 0, loadingEpoch: 0,
    // unfolded is the long files the user has asked to see (see
    // REVIEW_FOLD_LINES): kept shown however often the diff is drawn.
    unfolded: new Set(),
    // optionsLoading: what can be reviewed is being asked, so it isn't
    // asked again meanwhile.
    optionsLoading: false,
    // goTo is the comment a link in the conversation asked to go to, until
    // the review is drawn to go to it (see openThread).
    goTo: null,
    // ignoreSpace: the diff leaves out changes of whitespace alone.
    ignoreSpace: store.get(REVIEW_WHITESPACE) === 'ignore' }

  // changeReview asks vibe for a change to the review (do, ask, submit,
  // start), taking the review it gives back (see tookReview): every change
  // from the page goes this way, so a poll that set out before can't undo
  // it.
  async function changeReview(path, body) {
    const res = await post(path, body)
    if (res.ok && res.body && res.body.review) tookReview(res.body.review)
    return res
  }

  // doReview asks vibe to do req to the review (see changeReview).
  function doReview(req) {
    return changeReview('/api/review/do', req)
  }

  // tookReview takes rv, the review vibe gave back for something done on
  // the page, as the review.
  // announceReview says, once each, that the agent has begun reviewing (so the user waits for it) and that it
  // has finished, by a toast: the badge says it too, but only to someone looking at it. What was last drawn is
  // remembered, so a redraw says nothing more; a review already finished when the page opens is nothing new.
  function announceReview(rv) {
    const was = reviewView.announced
    const now = { started: String(rv.started || ''), state: rv.state || '' }
    reviewView.announced = now
    if (now.state === 'in progress' && !(was && was.started === now.started && was.state === 'in progress')) {
      toast('The agent is reviewing your changes. Wait for it to finish, then add your comments.', false, { label: 'OK', run() {} })
    } else if (now.state === 'done' && was && was.started === now.started && was.state === 'in progress') {
      toast('The agent has finished reviewing: it is yours to read now.')
    }
  }

  function tookReview(rv) {
    reviewView.review = rv
    reviewView.changed++
  }

  // once runs act (an async function) unless it's already running: a
  // button clicked twice, or Ctrl+Enter held, does it once.
  function once(act) {
    let running = false
    return async (...args) => {
      if (running) return
      running = true
      try { return await act(...args) } finally { running = false }
    }
  }

  // redrawReview draws the review again even though nothing in it changed:
  // something the page holds (a comment being written) did.
  function redrawReview() {
    drawReview(true)
  }

  // toggle puts value in set, or takes it out.
  function toggle(set, value) {
    if (set.has(value)) set.delete(value)
    else set.add(value)
  }

  // openComposer opens a comment on line of path's side: one already being
  // written moves there, keeping what's written.
  // row is the line numbers of the row clicked, on each side ({ old, new },
  // null where it has none): a Shift-click in the other column ranges from
  // it on a side they share.
  function openComposer(path, side, line, code, row) {
    const was = reviewView.composer
    reviewView.composer = { path, side, line, endLine: line, code, anchor: row || { [side]: line },
      text: was ? was.text : '', mode: was ? was.mode : 'write' }
    redrawReview()
    focusComposer()
  }

  // shownLine notes, while drawing, that line n of path's side is on the
  // page reading text: a range of lines takes its code from them, and can
  // only be made of them.
  function shownLine(path, side, n, text) {
    const key = path + '|' + side
    if (!reviewView.shown.has(key)) reviewView.shown.set(key, new Map())
    reviewView.shown.get(key).set(n, text)
  }

  // inComposer says line n of path's side is one the comment being written
  // is on (to mark it).
  function inComposer(path, side, n) {
    const c = reviewView.composer
    return !!c && c.path === path && c.side === side && n >= c.line && n <= c.endLine
  }

  // composerAfter says the comment being written goes after line n of
  // path's side: its last line.
  function composerAfter(path, side, n) {
    const c = reviewView.composer
    return !!c && c.path === path && c.side === side && n === c.endLine
  }

  // commentOnLines opens a comment on lines a to b (either way round) of
  // path's side, keeping what's been written; refused, saying why, when
  // some of them are still hidden.
  function commentOnLines(path, side, a, b) {
    const lo = Math.min(a, b)
    const hi = Math.max(a, b)
    const shown = reviewView.shown.get(path + '|' + side) || new Map()
    const code = []
    for (let n = lo; n <= hi; n++) {
      if (!shown.has(n)) { toast("A comment's lines can't take in lines still hidden: open them first."); return }
      code.push(shown.get(n))
    }
    const was = reviewView.composer
    reviewView.composer = { path, side, line: lo, endLine: hi, code: code.join('\n'), anchor: was ? was.anchor : { [side]: lo },
      text: was ? was.text : '', mode: was ? was.mode : 'write' }
    redrawReview()
    focusComposer()
  }

  function focusComposer() {
    const box = document.querySelector('textarea[aria-label="Your comment"]')
    if (box) box.focus()
  }

  // FORMATS are the formatting toolbar's buttons: what the page's Markdown
  // shows (see markdown.js), each with its shortcut, if it has one.
  const FORMATS = [
    { kind: 'bold', text: 'B', label: 'Bold (Ctrl+B)' },
    { kind: 'italic', text: 'I', label: 'Italic (Ctrl+I)' },
    { kind: 'code', text: '</>', label: 'Code' },
    { kind: 'codeblock', text: '{ }', label: 'Code block' },
    { kind: 'link', text: 'Link', label: 'Link (Ctrl+K)' },
    { kind: 'quote', text: '❝', label: 'Quote' },
    { kind: 'bullet', text: '•', label: 'Bulleted list' },
    { kind: 'numbered', text: '1.', label: 'Numbered list' },
  ]
  const FORMAT_KEYS = { b: 'bold', i: 'italic', k: 'link' }

  // formatBox applies a formatting button to box's selection: as typing
  // would where the browser can, so Ctrl+Z takes it back.
  function formatBox(box, kind) {
    const e = R.formatText(kind, box.value, box.selectionStart, box.selectionEnd)
    const want = box.value.slice(0, e.from) + e.insert + box.value.slice(e.to)
    box.focus()
    box.setSelectionRange(e.from, e.to)
    let done = false
    try { done = document.execCommand('insertText', false, e.insert) } catch (err) { /* not supported: set it below */ }
    if (!done || box.value !== want) {
      box.value = want
      box.dispatchEvent(new Event('input', { bubbles: true }))
    }
    box.setSelectionRange(e.selStart, e.selEnd)
  }

  // formattable gives comment box box its formatting shortcuts, and is
  // its toolbar. A button keeps the box's focus and selection when pressed.
  function formattable(box) {
    box.addEventListener('keydown', (ev) => {
      const kind = (ev.ctrlKey || ev.metaKey) && !ev.altKey && !ev.shiftKey && FORMAT_KEYS[ev.key.toLowerCase()]
      if (kind) { ev.preventDefault(); formatBox(box, kind) }
    })
    return el('div', { class: 'md-toolbar', role: 'toolbar', 'aria-label': 'Formatting' },
      ...FORMATS.map((f) => el('button', { type: 'button', class: 'ghost', text: f.text, title: f.label, 'aria-label': f.label,
        onmousedown: (ev) => ev.preventDefault(), onclick: () => formatBox(box, f.kind) })))
  }

  // lineButton is the + over a line's number (in a row with line numbers
  // row, { old, new }), opening a comment on it, or moving the one being
  // written there; a Shift-click makes the comment being written a range
  // to it. (Dragging one + onto another moves it too: see setUpLineDrag.)
  function lineButton(path, side, line, code, row) {
    const label = 'Comment on line ' + (side === 'old' ? 'L' : 'R') + line
    return el('button', { type: 'button', class: 'line-comment', text: '+', 'aria-label': label,
      title: label + ' (Shift-click another for several)',
      'data-path': path, 'data-side': side, 'data-line': String(line),
      onclick: (ev) => {
        const c = reviewView.composer
        if (ev.shiftKey && c && c.path === path) commentToRow(path, side, line, row || { [side]: line })
        else openComposer(path, side, line, code, row)
      } })
  }

  // commentToRow makes the comment being written a range from the row it
  // was begun on to row (Shift-clicked on its side's +): on that side when
  // it's the comment's, else on a side both rows have a line on, the new
  // file's first; refused, saying why, when they have none in common.
  function commentToRow(path, side, line, row) {
    const c = reviewView.composer
    const anchor = c.anchor || { [c.side]: c.line }
    const common = c.side === side ? side : ['new', 'old'].find((s) => anchor[s] && row[s])
    if (!common) { toast("A comment's lines are all on one side: Shift-click a line on the same side as the first."); return }
    commentOnLines(path, common, anchor[common] || c.line, row[common] || line)
  }

  // setUpLineDrag makes dragging one line's + onto another's move the
  // comment being written there, as clicking it does: a browser gives no
  // click when a press and its release are on different buttons.
  function setUpLineDrag() {
    let from = null
    const plusOf = (ev) => ev.target.closest && ev.target.closest('.line-comment')
    $('review-files').addEventListener('pointerdown', (ev) => { from = plusOf(ev) || null })
    document.addEventListener('pointerup', (ev) => {
      const to = plusOf(ev)
      const start = from
      from = null
      if (start && to && to !== start) to.click()
    })
  }

  // composerRow is the comment being written, as a row of a file's table:
  // Write (Markdown, as typed) and Preview (as it will show).
  function composerRow(columns) {
    const c = reviewView.composer
    const send = el('button', { type: 'button', text: 'Comment', disabled: !c.text.trim(), onclick: submitComposer })
    let body
    if (c.mode === 'write') {
      body = el('textarea', { class: 'review-compose-text', placeholder: 'Leave a comment (Markdown). Ctrl+Enter comments.', 'aria-label': 'Your comment' })
      body.value = c.text
      body.addEventListener('input', () => { c.text = body.value; send.disabled = !c.text.trim() })
      body.addEventListener('keydown', (ev) => {
        if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); submitComposer() }
      })
    } else {
      body = el('div', { class: 'review-compose-preview' }, c.text.trim() ? markdown(c.text) : el('p', { class: 'muted', text: 'Nothing to preview.' }))
    }
    const tab = (mode, label) => el('button', { type: 'button', role: 'tab', 'aria-selected': String(c.mode === mode),
      class: 'review-compose-tab' + (c.mode === mode ? ' on' : ''), text: label,
      onclick: () => { c.mode = mode; redrawReview(); if (mode === 'write') focusComposer() } })
    const box = el('div', { class: 'review-composer' },
      el('div', { class: 'review-compose-head', text: 'Add a comment on ' + (c.endLine > c.line
        ? 'lines ' + (c.side === 'old' ? 'L' : 'R') + c.line + '–' + (c.side === 'old' ? 'L' : 'R') + c.endLine
        : 'line ' + (c.side === 'old' ? 'L' : 'R') + c.line) }),
      el('div', { class: 'review-compose-tabs' }, el('span', { role: 'tablist' }, tab('write', 'Write'), tab('preview', 'Preview')),
        c.mode === 'write' ? formattable(body) : null),
      body,
      el('div', { class: 'bar review-compose-actions' }, el('span', { class: 'spacer' }),
        el('button', { type: 'button', class: 'ghost', text: 'Cancel', onclick: cancelComposer }), send))
    return sideRow(columns, c.side === 'old' ? [box] : [], c.side === 'old' ? [] : [box])
  }

  // sideRow is a row of things under a line: side by side (columns '6'),
  // left ones in the old file's pane and right ones in the new file's, as
  // GitHub has them; in one column, all of them across it.
  function sideRow(columns, left, right) {
    if (columns !== '6') return el('tr', { class: 'threads' }, el('td', { colspan: columns }, ...left, ...right))
    return el('tr', { class: 'threads' },
      el('td', { colspan: '3', class: 'side' + (left.length ? '' : ' empty') }, ...left),
      el('td', { colspan: '3', class: 'side start' + (right.length ? '' : ' empty') }, ...right))
  }

  const submitComposer = once(async () => {
    const c = reviewView.composer
    if (!c || !c.text.trim()) return
    try {
      const req = { op: 'comment', file: c.path, side: c.side, line: c.line, code: c.code, body: c.text }
      if (c.endLine > c.line) req.endLine = c.endLine
      const res = await doReview(req)
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); return }
      reviewView.composer = null
      document.activeElement && document.activeElement.blur()
      redrawReview()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  })

  // cancelComposer closes the comment being written, asking first when
  // there's something written to lose.
  function cancelComposer() {
    const close = () => { reviewView.composer = null; redrawReview() }
    if (!reviewView.composer || !reviewView.composer.text.trim()) { close(); return }
    confirmThen({ title: 'Discard this comment?', text: "What you've written goes; it was never sent.", commandNote: '', command: '',
      danger: true, runLabel: 'Discard', started: 'Discarded.',
      run: async () => { close(); return { ok: true, body: {} } } })
  }

  // unsentCount is how many of the user's comments the agent hasn't seen.
  function unsentCount(rv) {
    return rv.threads.reduce((n, t) => n + t.comments.filter((c) => c.author === 'user' && !c.sent).length, 0)
  }

  // drawTree draws the file tree from the files the filter keeps, and keeps
  // the diff to the same files. A folder the user closed stays closed,
  // unless the filter is in use: then every folder with a match is open.
  function drawTree() {
    const files = reviewView.files || []
    const query = reviewView.filter
    const filtering = !!query.trim()
    const shown = files.filter((f) => R.matchesFilter(f.path, query))
    const index = new Map(files.map((f, i) => [f.path, i]))
    const byPath = new Map(files.map((f) => [f.path, f]))
    const commentsOn = R.threadsByFile(reviewView.review ? reviewView.review.threads : [])
    const steps = new Map(R.inTreeOrder(files, reviewView.review ? reviewView.review.order : []).filter((o) => o.step).map((o) => [o.file.path, o]))
    const rows = []
    // Indented through element.style: the page's policy (style-src 'self')
    // drops a style attribute.
    const indented = (row, depth) => { row.style.paddingLeft = (4 + depth * 14) + 'px'; return row }
    const walk = (node, depth) => {
      for (const d of node.dirs) {
        const open = filtering || !reviewView.collapsed.has(d.path)
        rows.push(indented(el('button', { type: 'button', role: 'treeitem', 'aria-expanded': String(open), title: d.path, 'data-path': d.path,
          onclick: () => {
            if (filtering) return
            if (open) reviewView.collapsed.add(d.path)
            else reviewView.collapsed.delete(d.path)
            drawTree()
          } },
        el('span', { class: 'twisty', text: open ? '▾' : '▸' }), el('span', { class: 'name', text: d.name + '/' })), depth))
        if (open) walk(d, depth + 1)
      }
      for (const f of node.files) {
        const file = byPath.get(f.path)
        rows.push(indented(el('button', { type: 'button', role: 'treeitem', class: viewedState(file) === 'viewed' ? 'viewed' : '', title: f.path + ' (' + file.status + ')', 'data-path': f.path,
          onclick: () => {
            scrollToFile(index.get(f.path))
            reviewView.current = f.path
            reviewView.fileSteering = true
            markCurrentFile()
          } },
        el('span', { class: 'twisty' }), fileIcon(file.status),
        el('span', { class: 'name', text: f.name }), stepBadge((steps.get(f.path) || {}).step, (steps.get(f.path) || {}).stop),
        commentCount(commentsOn.get(f.path) || 0)), depth))
      }
    }
    walk(R.fileTree(shown.map((f) => f.path)), 0)
    if (filtering && !shown.length) rows.push(el('p', { class: 'muted', text: 'No file matches.' }))
    $('review-tree').replaceChildren(...rows)
    files.forEach((f, i) => {
      const card = $('review-file-' + i)
      if (card) card.hidden = !R.matchesFilter(f.path, query)
    })
    countComments()
    if (reviewView.fileSteering) markCurrentFile()
    else measureCurrentFile()
  }

  // fileCards are the files' sections the page shows, in its order.
  function fileCards() {
    return Array.from($('review-files').querySelectorAll('.review-file')).filter((card) => !card.hidden)
  }

  // measureCurrentFile works out which file the page is in, from where the
  // files' sections are: the last reaching up to just under the bar.
  function measureCurrentFile() {
    const cards = fileCards()
    const i = R.currentOf(cards.map((c) => c.getBoundingClientRect().top), threadLine())
    reviewView.current = i < 0 ? null : cards[i].dataset.path
    markCurrentFile()
  }

  // markCurrentFile marks the file the page is in, in the tree: its row, or
  // the collapsed folder it's hidden in; keeps that row in view in the
  // tree; and counts how far through the files it is.
  function markCurrentFile() {
    const tree = $('review-tree')
    for (const row of tree.querySelectorAll('.current')) row.classList.remove('current')
    const cards = fileCards()
    const total = (reviewView.files || []).length
    const at = cards.findIndex((c) => c.dataset.path === reviewView.current)
    const filtering = !!reviewView.filter.trim()
    const viewed = (reviewView.files || []).filter((f) => viewedState(f) === 'viewed').length
    $('review-tree-count').textContent = !total ? ''
      : (at >= 0 ? (at + 1) + '/' : '') + (filtering ? cards.length + ' of ' + total : total) + (total === 1 ? ' file' : ' files') +
        (viewed ? ' · ' + viewed + '/' + total + ' viewed' : '')
    const path = reviewView.current
    if (!path) return
    const rows = Array.from(tree.querySelectorAll('[data-path]'))
    const row = rows.find((r) => r.dataset.path === path) ||
      rows.filter((r) => path.startsWith(r.dataset.path + '/')).sort((a, b) => b.dataset.path.length - a.dataset.path.length)[0]
    if (!row) return
    row.classList.add('current')
    // In view in the tree, which scrolls on its own: just far enough.
    const pane = tree.closest('.review-tree-pane')
    const r = row.getBoundingClientRect()
    const p = pane.getBoundingClientRect()
    if (r.bottom > p.bottom) pane.scrollTop += r.bottom - p.bottom + 4
    else if (r.top < p.top) pane.scrollTop -= p.top - r.top + 4
  }

  // commentCount is a file's comment threads, for its row in the tree.
  function commentCount(n) {
    if (!n) return null
    return el('span', { class: 'review-tree-comments', title: plural(n, 'comment') }, commentIcon(), String(n))
  }

  function commentIcon() {
    const ns = 'http://www.w3.org/2000/svg'
    const svg = document.createElementNS(ns, 'svg')
    svg.setAttribute('viewBox', '0 0 16 16')
    svg.setAttribute('aria-hidden', 'true')
    const path = document.createElementNS(ns, 'path')
    path.setAttribute('d', 'M2.5 2.5h11v8h-5l-3 3v-3h-3z')
    svg.append(path)
    return svg
  }

  // threadLine is where a thread is scrolled to, on the screen: just under
  // the bar and the file header pinned below it, by its scroll-margin-top
  // (8px more, in app.css).
  function threadLine() {
    return $('panel-review').querySelector(':scope > .bar').getBoundingClientRect().bottom + fileHeadHeight() + 8
  }

  // fileHeadHeight is how tall a file's header is, pinned under the bar
  // while the page is in its file; 0 with no files.
  function fileHeadHeight() {
    const head = $('review-files').querySelector('.review-file > header')
    return head ? head.getBoundingClientRect().height : 0
  }

  // setCommentOrder sets the order the comment arrows go in, 'path' or
  // 'reading', for this browser: chosen under the comment count, or in the
  // comments pane, which go in step.
  function setCommentOrder(order) {
    reviewView.order = order
    store.set(REVIEW_ORDER, order)
    reviewView.steering = false
    countComments()
    drawCommentsPane()
  }

  // setUpOrderMenu makes the comment count a button choosing the order the
  // comment arrows go in. The reading order can only be chosen when the
  // agent set one.
  function setUpOrderMenu() {
    const button = $('review-order')
    const menu = $('review-order-menu')
    const close = () => { menu.hidden = true; button.setAttribute('aria-expanded', 'false') }
    button.addEventListener('click', () => {
      if (!menu.hidden) { close(); return }
      const none = reviewSteps().size === 0
      for (const item of menu.querySelectorAll('[role=menuitemradio]')) {
        item.setAttribute('aria-checked', String(item.dataset.order === reviewView.order))
        if (item.dataset.order === 'reading') {
          item.disabled = none
          item.textContent = 'Order by agent reading order' + (none ? ' (the agent set no reading order)' : '')
        }
      }
      menu.hidden = false
      button.setAttribute('aria-expanded', 'true')
      const checked = menu.querySelector('[aria-checked=true]')
      if (checked) checked.focus()
    })
    menu.addEventListener('click', (ev) => {
      const item = ev.target.closest('[role=menuitemradio]')
      if (!item || item.disabled) return
      close()
      setCommentOrder(item.dataset.order)
    })
    menu.addEventListener('keydown', (ev) => { if (ev.key === 'Escape') { ev.stopPropagation(); close(); button.focus() } })
    document.addEventListener('pointerdown', (ev) => { if (!menu.hidden && !ev.target.closest('.review-order-wrap')) close() })
  }

  // reviewSteps is each file's step in the reading order (path to number).
  function reviewSteps() {
    const rv = reviewView.review
    return new Map(R.inTreeOrder(reviewView.files || [], rv ? rv.order : []).filter((o) => o.step).map((o) => [o.file.path, o.step]))
  }

  // arrowCards are the shown threads' cards in the order the comment
  // arrows go in: the page's, or the reading order's.
  function arrowCards() {
    const cards = threadCards()
    if (reviewView.order !== 'reading') return cards
    const entries = cards.map((card) => { const file = card.closest('.review-file'); return { file: file ? file.dataset.path : null, card } })
    return R.inStepOrder(entries, reviewSteps()).map((e) => e.card)
  }

  // threadCards are the threads' cards the page shows, in its order: the
  // filter hides the rest.
  function threadCards() {
    return Array.from($('panel-review').querySelectorAll('.review-thread')).filter((card) => !card.closest('[hidden]'))
  }

  // countComments says, in the bar, how many comment threads there are (and
  // how many the filter hides), and lets the arrows go to them.
  function countComments() {
    const rv = reviewView.review
    const all = rv ? rv.threads.length : 0
    const open = rv ? rv.threads.filter((t) => t.status === 'open').length : 0
    const page = threadCards()
    const cards = arrowCards()
    const shown = cards.length
    const filtered = $('review-files').querySelectorAll('.review-file[hidden] .review-thread').length
    const inViewed = Math.max(0, all - shown - filtered)
    const hiddenNote = [filtered ? filtered + ' hidden by the filter' : '', inViewed ? inViewed + ' in viewed files' : ''].filter(Boolean).join(', ')
    if (!reviewView.steering) {
      // Where the page is: in the page's order, then counted in the arrows'.
      const tops = page.map((c) => c.getBoundingClientRect().top)
      if (reviewView.order === 'reading') {
        const i = R.currentOf(tops, threadLine())
        reviewView.at = i < 0 ? 0 : cards.indexOf(page[i])
      } else reviewView.at = Math.max(0, R.positionOf(tops, threadLine()))
    }
    reviewView.at = Math.min(reviewView.at, Math.max(0, shown - 1))
    const where = shown ? (reviewView.at + 1) + '/' + plural(shown, 'comment') : plural(all, 'comment')
    $('review-count').textContent = !all ? 'no comments yet' : where + (hiddenNote ? ' (' + hiddenNote + ')' : '')
    $('review-order').title = (all ? open + ' open of ' + all + ' comment threads. ' : '') +
      (reviewView.order === 'reading' ? "The arrows go in the agent's reading order" : 'The arrows go in path order, as the tree') + ': click to change.'
    $('review-prev-comment').disabled = !shown
    $('review-next-comment').disabled = !shown
  }

  // goToComment scrolls to the next thread past the one the page is at, or
  // the one before it, and puts focus in its reply box.
  function goToComment(dir) {
    const cards = arrowCards()
    if (!cards.length) return
    const n = cards.length
    let i
    if (reviewView.steering) i = (reviewView.at + (dir === 'next' ? 1 : -1) + n) % n
    else if (reviewView.order === 'reading') {
      // From the thread the page is at, on in the reading order.
      const page = threadCards()
      const at = R.currentOf(page.map((c) => c.getBoundingClientRect().top), threadLine())
      const from = at < 0 ? -1 : cards.indexOf(page[at])
      i = dir === 'next' ? (from + 1) % n : from <= 0 ? n - 1 : from - 1
    } else i = R.stepTo(cards.map((c) => c.getBoundingClientRect().top), threadLine(), dir)
    if (i < 0) return
    showComment(cards, i)
  }

  // goToCommentAfter goes to the comment after thread id's, in the order the
  // arrows go, round at the end.
  function goToCommentAfter(id) {
    const cards = arrowCards()
    const i = cards.findIndex((card) => card.id === 'review-thread-' + id)
    if (i < 0 || cards.length < 2) return
    showComment(cards, (i + 1) % cards.length)
  }

  // showComment scrolls to cards[i], its reply box ready to type in, and
  // makes it where the arrows go on from.
  function showComment(cards, i) {
    reviewView.at = i
    reviewView.steering = true
    countComments()
    cards[i].scrollIntoView({ behavior: 'smooth', block: 'start' })
    // Its reply box, to type in at once.
    ;(cards[i].querySelector('textarea[data-thread]') || cards[i]).focus({ preventScroll: true })
  }

  // What each file's mark in the tree says about it.
  // FILE_SIGNS are the sign on each file's icon in the tree, by what
  // happened to it, as paths in the icon's 16x16 box (and, for tests and
  // screen readers, as the character they draw).
  const FILE_SIGNS = {
    added: { sign: '+', d: 'M8 6.5v5M5.5 9h5' },
    modified: { sign: '±', d: 'M8 5.5v4M6 7.5h4M6 11.5h4' },
    deleted: { sign: '−', d: 'M5.5 9h5' },
    renamed: { sign: '→', d: 'M5.5 9h5M8.5 7l2 2-2 2' },
  }

  // fileIcon is a file's icon in the tree: a page with a folded corner,
  // coloured and signed by what happened to it, as GitHub has them.
  function fileIcon(status) {
    const s = FILE_SIGNS[status] || FILE_SIGNS.modified
    const ns = 'http://www.w3.org/2000/svg'
    const svg = document.createElementNS(ns, 'svg')
    svg.setAttribute('viewBox', '0 0 16 16')
    svg.setAttribute('class', 'file-icon ' + status)
    svg.setAttribute('data-sign', s.sign)
    svg.setAttribute('aria-hidden', 'true')
    for (const d of ['M3.5 1.5h6l3 3v10h-9z M9.5 1.5v3h3', s.d]) {
      const path = document.createElementNS(ns, 'path')
      path.setAttribute('d', d)
      svg.append(path)
    }
    return svg
  }

  // scrollToFile scrolls to a file's diff: never a link to it, as the URL's
  // fragment is the page's token.
  function scrollToFile(index) {
    const card = $('review-file-' + index)
    if (card) card.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  // The tree's width, kept for this browser: dragged, or nudged with the
  // arrow keys on the line between it and the diff.
  const REVIEW_TREE_WIDTH = 'vibe-review-tree-width'

  function setTreeWidth(px) {
    const body = $('review-body')
    const max = Math.max(200, body.clientWidth * 0.6)
    const width = Math.round(Math.min(max, Math.max(140, px)))
    body.style.setProperty('--review-tree-width', width + 'px')
    return width
  }

  function setUpReviewTree() {
    const saved = Number(store.get(REVIEW_TREE_WIDTH))
    if (saved > 0) $('review-body').style.setProperty('--review-tree-width', saved + 'px')
    const splitter = $('review-splitter')
    splitter.addEventListener('pointerdown', (ev) => {
      ev.preventDefault()
      splitter.setPointerCapture(ev.pointerId)
      splitter.classList.add('dragging')
      const left = $('review-body').getBoundingClientRect().left
      const move = (e) => setTreeWidth(e.clientX - left)
      const up = (e) => {
        splitter.classList.remove('dragging')
        splitter.removeEventListener('pointermove', move)
        splitter.removeEventListener('pointerup', up)
        store.set(REVIEW_TREE_WIDTH, String(setTreeWidth(e.clientX - left)))
      }
      splitter.addEventListener('pointermove', move)
      splitter.addEventListener('pointerup', up)
    })
    splitter.addEventListener('keydown', (ev) => {
      if (ev.key !== 'ArrowLeft' && ev.key !== 'ArrowRight') return
      ev.preventDefault()
      const now = splitter.previousElementSibling.getBoundingClientRect().width
      store.set(REVIEW_TREE_WIDTH, String(setTreeWidth(now + (ev.key === 'ArrowLeft' ? -20 : 20))))
    })
    const filter = $('review-filter')
    filter.value = reviewView.filter
    filter.addEventListener('input', () => {
      reviewView.filter = filter.value
      sessionSet(REVIEW_FILTER, filter.value)
      drawTree()
    })
    filter.addEventListener('keydown', (ev) => {
      if (ev.key === 'Escape' && filter.value) {
        ev.preventDefault()
        filter.value = ''
        filter.dispatchEvent(new Event('input'))
      }
    })
    // / finds, as on the Profiles tab.
    document.addEventListener('keydown', (ev) => {
      if (ev.key !== '/' || ev.ctrlKey || ev.metaKey || ev.altKey || $('panel-review').hidden || typingInto(document.activeElement)) return
      ev.preventDefault()
      filter.focus()
      filter.select()
    })
  }

  // openGap opens a file's gap by direction ('down', 'up' or 'all'),
  // fetching the file's lines first if they aren't to hand.
  // showPastes says, in the review's bar, how many pastes will go to the
  // agent with the next message: the clipboard page can't be seen here.
  function showPastes(n) {
    $('review-pastes').hidden = !n
    $('review-pastes').textContent = plural(n, 'paste') + ' to send'
  }

  // reviewPaste sends what's pasted on the Review tab that isn't text (an
  // image, a file) to the clipboard page, to go to the agent with the next
  // message, saying which; in a text box, text pastes into it as ever.
  async function reviewPaste(ev) {
    const data = ev.clipboardData
    const files = data ? Array.from(data.files || []) : []
    // In a text box, text goes in it as ever; elsewhere text has nowhere to
    // go, and a file copied in a file manager carries its path as text too.
    if (!files.length || (typingInto(ev.target) && data.getData('text/plain'))) return
    ev.preventDefault()
    const said = { image: 0, file: 0, already: 0 }
    for (const f of files) {
      try {
        const res = await fetch('/api/clipboard/paste', { method: 'POST', body: f, headers: {
          'X-Vibe-Token': token, 'Content-Type': f.type || 'application/octet-stream', 'X-Filename': encodeURIComponent(f.name || '') } })
        reached(true)
        let body = {}
        try { body = await res.json() } catch (e) { /* not JSON: said by its status */ }
        if (!res.ok) { toast(body.error || "That couldn't be pasted.", true); return }
        if (body.duplicate) said.already++
        else said[(f.type || '').startsWith('image/') ? 'image' : 'file']++
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
        return
      }
    }
    const many = (n, one, more) => (n === 1 ? one : n + ' ' + more) + ' pasted'
    const parts = [said.image && many(said.image, 'Image', 'images'), said.file && many(said.file, 'File', 'files')].filter(Boolean)
    if (said.already) parts.push(parts.length ? '(' + said.already + ' already pasted)' : 'Already pasted')
    toast(parts.join(' '))
    loadReview(false)
  }

  // loadFileLines gets path's lines, for its gaps: true once they're there.
  async function loadFileLines(path) {
    if (reviewView.fileLines.has(path)) return true
    try {
      const res = await api('/api/review/file?path=' + encodeURIComponent(path))
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || "The file couldn't be read.", true); return false }
      reviewView.fileLines.set(path, res.body.lines || [])
      return true
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
      return false
    }
  }

  // wantLines gets path's lines, once, to place the threads on lines its
  // diff leaves out, and draws them there; not while a reply's being typed.
  function wantLines(path) {
    if (reviewView.linesAsked.has(path)) return
    reviewView.linesAsked.add(path)
    loadFileLines(path).then((ok) => { if (ok) { reviewView.key = null; drawReview() } })
  }

  async function openGap(path, index, direction) {
    if (!(await loadFileLines(path))) return
    const byGap = reviewView.opened.get(path) || {}
    const g = byGap[index] || { down: 0, up: 0 }
    if (direction === 'down') g.down += R.GAP_STEP
    else if (direction === 'up') g.up += R.GAP_STEP
    else g.down = Number.MAX_SAFE_INTEGER
    byGap[index] = g
    reviewView.opened.set(path, byGap)
    redrawReview()
  }

  function showReviewLayout() {
    $('review-layout').textContent = reviewView.layout === 'split' ? 'One column' : 'Side by side'
    $('review-layout').title = reviewView.layout === 'split' ? 'Show each file as one column, old and new lines in turn' : 'Show the old and new lines side by side'
  }
  const R = window.vibeReview
  // A file this long is shown folded until asked for: a big generated file
  // shouldn't bury the rest.
  const REVIEW_FOLD_LINES = 600

  // loadReview loads the review, and with withDiff its diff, worked out
  // afresh. A load asked for the diff while one's on its way follows it.
  async function loadReview(withDiff) {
    if (reviewView.loading) {
      if (withDiff && (!reviewView.loadingDiff || reviewView.loadingEpoch !== reviewView.epoch)) reviewView.diffWanted = true
      return
    }
    reviewView.loading = true
    reviewView.loadingDiff = withDiff
    reviewView.loadingEpoch = reviewView.epoch
    const changed = reviewView.changed
    try {
      const res = await api('/api/review' + (withDiff ? '?diff=1' + (reviewView.ignoreSpace ? '&whitespace=ignore' : '') : ''), { timeout: 40000 })
      reached(true)
      if (!res.ok) {
        reviewView.goTo = null
        // A review that can't be read is the tab's to say, with a way out.
        if (res.body && res.body.code === 'review-unreadable') showBroken(res.body.error)
        else problem('review', res)
        return
      }
      banner('review', '')
      showBroken('')
      // Something done on the page since this set out gave a newer review.
      if (reviewView.changed === changed) {
        const wasOn = reviewView.review && reviewIsOn(reviewView.review)
        reviewView.review = res.body.review
        reviewView.ask = res.body.ask || null
        reviewView.agentAtWork = !!res.body.agentAtWork
        // What can be reviewed is asked when the start screen shows, not at
        // every poll: it's git, which polls keep clear of.
        if (!reviewIsOn(reviewView.review) && (wasOn || (!reviewView.options && !reviewView.optionsError))) loadOptions()
      }
      showPastes(res.body.pastes)
      // A diff that set out before the review's files went stale (Refresh,
      // a new review) isn't drawn: the one asked for since is on its way.
      if (withDiff && reviewView.loadingEpoch === reviewView.epoch) {
        reviewView.files = res.body.diff || []
        reviewView.diffError = res.body.diffError || ''
        // The code may have changed with the same files: drawn afresh.
        reviewView.key = null
      }
      drawReview()
    } catch (e) {
      reached(false)
      // Not to be gone to out of the blue, once vibe answers again.
      reviewView.goTo = null
    } finally {
      reviewView.loading = false
      if (reviewView.diffWanted) {
        reviewView.diffWanted = false
        loadReview(true)
      }
    }
  }

  // typingInABox says a box on the tab is being typed in: redrawing now
  // would take it away mid-word, so the redraw waits for the next poll. An
  // empty box with focus (the comment arrows put it in a reply box; a +
  // puts it in a comment box) isn't: holding back would hide what the
  // agent says until it lost focus. Drawn again, it keeps the focus.
  function typingInABox() {
    const a = document.activeElement
    if (!a || a.tagName !== 'TEXTAREA' || !$('panel-review').contains(a)) return false
    return !!a.value.trim()
  }

  // focusedBox is which box on the tab has focus, to give it back once the
  // tab is drawn afresh: a reply box by its thread, the comment box, the
  // edit box; null for none.
  function focusedBox() {
    const a = document.activeElement
    if (!a || a.tagName !== 'TEXTAREA' || !$('panel-review').contains(a)) return null
    if (a.dataset.thread) return 'textarea[data-thread="' + a.dataset.thread + '"]'
    if (a.getAttribute('aria-label') === 'Edit your comment') return 'textarea[aria-label="Edit your comment"]'
    if (a.getAttribute('aria-label') === 'Your comment') return 'textarea[aria-label="Your comment"]'
    return null
  }

  // drawReview draws the review when it has changed; a poll's redraw waits
  // while a box on the tab is being typed in (it would take the focus away),
  // one the user asked for (force) doesn't.
  // reviewIsOn says rv is a review to show: started (or commented on) and
  // not closed. Else the tab offers to start one.
  function reviewIsOn(rv) {
    const started = !!rv.started && !String(rv.started).startsWith('0001-')
    return !rv.closed && (started || rv.threads.length > 0)
  }

  // drawStart draws the ways to start a review: each kind there is to
  // review, by the agent (when the page can talk to it), or just its diff.
  function drawStart(rv) {
    $('review-closed').hidden = !rv.closed
    $('review-closed').textContent = rv.closed ? 'The last review was closed. ' + rv.closed : ''
    // A dropped review left nothing behind to reopen.
    $('review-reopen').hidden = !rv.closed || (!rv.threads.length && (!rv.started || String(rv.started).startsWith('0001-')))
    const o = reviewView.options
    if (!o) {
      $('review-start-options').replaceChildren(el('p', { class: reviewView.optionsError ? 'bad-text' : 'muted',
        text: reviewView.optionsError ? "What there is to review couldn't be told: " + reviewView.optionsError : 'Seeing what there is to review…' }))
      return
    }
    const kinds = []
    if (o.uncommitted) kinds.push({ kind: 'uncommitted', what: 'the uncommitted changes', review: 'Review uncommitted changes', show: 'Show uncommitted changes' })
    const against = o.defaultBranch || o.emptyDefaultBranch
    if (against) kinds.push({ kind: 'branch', what: 'this branch against ' + against, review: 'Review against ' + against, show: 'Diff against ' + against, empty: !o.defaultBranch })
    if (o.upstream) {
      const n = plural(o.unpushed, 'unpushed commit')
      kinds.push({ kind: 'unpushed', what: 'the commits not yet pushed to ' + o.upstream, review: 'Review ' + n, show: 'Show ' + n })
    }
    // Why there's no reviewing against the default branch: it can't be told
    // (it's never guessed, but it can be set), or it has no commits yet. The
    // setter is kept across redraws, with what's typed in it.
    let noDefault = null
    if (o.noDefaultBranch) {
      if (!reviewView.setter || reviewView.setter.why !== o.noDefaultBranch) {
        reviewView.setter = { why: o.noDefaultBranch, node: defaultBranchSetter(o.noDefaultBranch, () => { reviewView.setter = null; loadOptions() }) }
      }
      noDefault = reviewView.setter.node
    } else if (o.emptyDefaultBranch) {
      noDefault = el('p', { class: 'muted note', text: o.emptyDefaultBranch + " has no commits yet, so there's nothing to review against it." })
    }
    if (!kinds.length) {
      $('review-start-options').replaceChildren(el('p', { class: 'muted',
        text: 'Nothing to review: no uncommitted changes, no commits waiting to be pushed, and no other branch to compare this one with.' }),
      ...(noDefault ? [noDefault] : []))
      return
    }
    const button = (k, agent) => el('button', { type: 'button', class: agent ? '' : 'ghost', text: agent ? k.review : k.show, disabled: !!k.empty,
      title: agent ? 'Ask the agent to review ' + k.what + ': the diff shows at once, its comments as it makes them' : 'Show the diff of ' + k.what + ', to review yourself',
      onclick: () => startReview(k.kind, agent) })
    $('review-start-options').replaceChildren(
      ...(state.conversation ? [...kinds.map((k) => button(k, true)), el('div', { class: 'gap' })] : []),
      ...kinds.map((k) => button(k, false)),
      ...(noDefault ? [el('div', { class: 'gap' }), noDefault] : []))
  }

  async function loadOptions() {
    if (reviewView.optionsLoading) return
    reviewView.optionsLoading = true
    try {
      const res = await api('/api/review/options')
      reached(true)
      reviewView.options = res.ok ? res.body : null
      reviewView.optionsError = res.ok ? '' : (res.body && res.body.error) || 'vibe said no'
      drawReview()
    } catch (e) {
      reached(false)
    } finally {
      reviewView.optionsLoading = false
    }
  }

  // showReview shows review rv, as vibe gave it, with its diff worked out
  // afresh: one just started, or reopened.
  function showReview(rv) {
    tookReview(rv)
    reviewView.files = null
    // A comment being written, or edited, was on the review before.
    reviewView.composer = null
    reviewView.editing = null
    forgetFiles()
    drawReview()
    loadReview(true)
  }

  // showBroken says, on the tab, why the review can't be read, offering to
  // start afresh; "" hides it.
  function showBroken(why) {
    $('review-broken').hidden = !why
    $('review-broken-why').textContent = why
    if (why) $('review-wait').hidden = true
  }

  // startAfresh sets aside the review that can't be read (vibe keeps it in
  // history/) and begins an empty one, asking first.
  function startAfresh() {
    confirmThen({ title: 'Start afresh?', text: "The review that can't be read is set aside in history/ (nothing is deleted), and an empty one is begun. What the agent sends from now on lands in it.",
      commandNote: '', command: '', runLabel: 'Start afresh', started: 'Started afresh.',
      run: async () => {
        const res = await changeReview('/api/review/afresh', {})
        if (res.ok) { showBroken(''); showReview(res.body.review) }
        return res
      } })
  }

  // forgetFiles forgets what the page has of the diff's files (their
  // lines, the gaps opened in them, their colours, which long ones are
  // shown): the code may have changed under it.
  function forgetFiles() {
    reviewView.epoch++
    reviewView.fileLines.clear()
    reviewView.linesAsked.clear()
    reviewView.opened.clear()
    reviewView.colours.clear()
    reviewView.unfolded.clear()
    reviewView.key = null
  }

  // refreshReview loads the review and its diff afresh, and what can be
  // reviewed, when there's no review to show.
  function refreshReview() {
    forgetFiles()
    reviewView.options = null
    reviewView.optionsError = ''
    if (reviewView.review && !reviewIsOn(reviewView.review)) loadOptions()
    loadReview(true)
  }

  async function startReview(kind, agent) {
    try {
      const res = await changeReview('/api/review/start', { kind, agent })
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); return }
      showReview(res.body.review)
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  // closeReview closes the review, asking first when that stops the agent
  // at work on it, or comments on it are still open: the review the agent is
  // still writing is dropped (vibe would only be asked to write it again);
  // any other is kept, to reopen. With nothing open, the agent is left to
  // finish. Cancelled with comments open, the first of them is shown.
  function closeReview(again = false) {
    const rv = reviewView.review
    const open = rv.threads.filter((t) => t.status === 'open').length
    const writing = reviewView.agentAtWork && rv.state === 'in progress'
    const stop = writing || (reviewView.agentAtWork && open > 0)
    const close = async () => {
      const res = await post('/api/review/close', { stop })
      if (res.status === 409 && !again) {
        // The agent began work on the review since the page last looked:
        // asked again, saying what closing now does.
        await loadReview(false)
        reviewView.agentAtWork = true
        closeReview(true)
        return { asked: true }
      }
      if (res.ok) { tookReview(res.body.review); reviewView.key = null; drawReview(); loadOptions() }
      return res
    }
    if (!writing && !open) {
      close().then((res) => { reached(true); if (!res.ok && !res.asked) toast((res.body && res.body.error) || 'That failed.', true) },
        () => { reached(false); toast('Could not reach vibe.', true) })
      return
    }
    const opens = open === 1 ? '1 comment is' : open + ' comments are'
    const text = writing
      ? 'The agent is still writing this review. Closing it stops the agent, and the review is dropped: you can start another at any time.'
      : 'This review is incomplete: ' + opens + ' still open. ' +
        (stop ? "Closing it stops the agent's work on it: changes it has finished are kept, but one it's partway through may be left half-done. " : '') +
        'The review is kept: you can reopen it at any time.'
    confirmThen({ title: 'Close this review?', text: text + ' Are you sure you want to close it?', safe: true,
      commandNote: '', command: '', runLabel: 'Close review', started: 'Closed.',
      run: close,
      cancelled: writing ? null : showFirstOpenComment })
  }

  // showFirstOpenComment opens the comments pane at the first comment still
  // open, as high in it as it goes, focused: a filter that hid it is set to
  // show the open ones.
  function showFirstOpenComment() {
    const firstOpen = () => paneThreads().find((t) => t.status === 'open')
    if (!firstOpen()) reviewView.pane.filter = 'open'
    openCommentsPane(true)
    const t = firstOpen()
    const card = t && $('review-comments-list').querySelector(`[data-thread="${t.id}"]`)
    if (!card) return
    card.scrollIntoView({ block: 'start' })
    card.focus({ preventScroll: true })
  }

  async function reopenReview() {
    try {
      const res = await doReview({ op: 'reopen-review' })
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); return }
      showReview(res.body.review)
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  function drawReview(force) {
    const rv = reviewView.review
    if (!rv) return
    const key = JSON.stringify(rv) + '|' + JSON.stringify(reviewView.ask) + '|' + JSON.stringify(reviewView.options) + reviewView.optionsError + '|' + (reviewView.files ? reviewView.files.length : -1) + reviewView.diffError
    if (!force && (key === reviewView.key || typingInABox())) return
    reviewView.key = key
    for (const box of $('panel-review').querySelectorAll('textarea[data-thread]')) {
      if (box.value.trim()) reviewView.drafts.set(box.dataset.thread, box.value)
    }
    const focused = focusedBox()
    // The cards drawn last go: so does watching them.
    if (colourWatch) colourWatch.disconnect()
    const on = reviewIsOn(rv)
    $('review-start').hidden = on
    $('review-body').hidden = !on
    $('review-actions').hidden = !on
    $('review-wait').hidden = reviewView.files !== null || !on
    if (!on) drawStart(rv)
    $('review-title').textContent = on && rv.note ? 'Review: ' + rv.note : 'Review'
    $('review-state').hidden = !on || !rv.state
    $('review-state').textContent = rv.state === 'in progress' ? 'the agent is still reviewing: wait for it to finish' : rv.state || ''
    $('review-state').classList.toggle('reviewing', on && rv.state === 'in progress')
    if (on) announceReview(rv)
    $('review-of').textContent = !on ? '' : rv.commit ? 'of commit ' + rv.commit : rv.unpushed ? 'of the commits not pushed to ' + rv.ref : rv.ref ? 'against ' + rv.ref : 'of the uncommitted changes'
    const unsent = unsentCount(rv)
    const stillWriting = rv.state === 'in progress' && reviewView.agentAtWork
    $('review-submit').disabled = !unsent || !state.conversation || stillWriting
    $('review-submit').textContent = unsent ? 'Submit ' + unsent + ' to agent' : 'Submit to agent'
    $('review-submit').title = !state.conversation ? "This sandbox's agent can't be talked to from the page"
      : stillWriting ? 'The agent is still writing this review: submit your comments once it has finished'
      : unsent ? 'Send the agent your ' + plural(unsent, 'comment') + " it hasn't seen: it changes code only where you ask it to"
        : "Nothing new to send: comment on a line, or reply, first"
    $('review-error').hidden = !reviewView.diffError
    $('review-error').textContent = reviewView.diffError
    $('review-problems').hidden = !(rv.problems || []).length
    $('review-problems').replaceChildren(...(rv.problems || []).map((p) => el('li', { text: "The agent's request failed: " + p })))

    const files = reviewView.files || []
    reviewView.shown = new Map()
    const threads = new Map(rv.threads.map((t) => [t.id, t]))
    const placed = R.placeThreads(files, rv.threads)
    reviewView.outdated = placed.outdated
    const cards = (ids) => ids.map((id) => threadCard(threads.get(id)))
    $('review-general').replaceChildren(...cards(placed.general),
      ...(placed.elsewhere.length ? [el('p', { class: 'muted', text: 'On files not in this diff:' }), ...cards(placed.elsewhere)] : []))

    // The files as the tree lists them, so the tree, the files and the
    // comment arrows all go one way; the reading order is numbered on them.
    const ordered = R.inTreeOrder(files, rv.order)
    reviewView.drawn = { files, ordered, placed, threads }

    $('review-files').replaceChildren(...(files.length || reviewView.files === null ? [] : [el('p', { class: 'muted', text: 'Nothing to review: no changes.' })]),
      ...ordered.map((o) => foldIfViewed(o.file, fileCard(o.file, o.stop, o.step, files.indexOf(o.file), placed, threads))))
    drawTree()
    drawCommentsPane()
    // The box that had focus, drawn afresh, keeps it.
    const again = focused && $('panel-review').querySelector(focused)
    if (again) again.focus({ preventScroll: true })
    goToWanted()
  }

  // viewedState is whether file f is ticked as viewed: 'viewed', 'changed'
  // (ticked, but its change has changed since), or ''.
  function viewedState(f) {
    const seen = reviewView.review && reviewView.review.viewed ? reviewView.review.viewed[f.path] : undefined
    return !seen ? '' : seen === f.fingerprint ? 'viewed' : 'changed'
  }

  // viewedTick is the Viewed tick in file f's header, saying when its
  // change has changed since it was ticked.
  function viewedTick(f) {
    const was = viewedState(f)
    const box = el('input', { type: 'checkbox' })
    box.checked = was === 'viewed'
    box.addEventListener('change', () => tickViewed(f, box.checked))
    return el('label', { class: 'review-viewed', title: 'Ticked, its diff is folded away until you untick it' },
      was === 'changed' ? el('span', { class: 'review-changed', text: 'changed since you viewed it' }) : null, box, ' Viewed')
  }

  async function tickViewed(f, on) {
    try {
      const res = await doReview(on ? { op: 'view', file: f.path, code: f.fingerprint } : { op: 'unview', file: f.path })
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); redrawReview(); return }
      redrawReview()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
      // The tick shows what vibe has, not what was asked.
      redrawReview()
    }
  }

  // foldIfViewed folds file f's card, ticked as viewed, to its header.
  function foldIfViewed(f, card) {
    if (viewedState(f) !== 'viewed') return card
    card.classList.add('viewed')
    for (const child of card.children) if (child.tagName !== 'HEADER') child.hidden = true
    return card
  }

  // colourSides are the sides of file f there are to colour: an added file
  // has only its new one, a deleted file its old; a binary one none.
  function colourSides(f) {
    if (f.binary || f.tooLarge) return []
    if (f.status === 'added') return [{ side: 'new', path: f.path }]
    if (f.status === 'deleted') return [{ side: 'old', path: f.path }]
    return [{ side: 'old', path: f.oldPath || f.path }, { side: 'new', path: f.path }]
  }

  // fetchColours asks vibe to colour the sides of file f it hasn't yet,
  // drawing them as they come (not while a reply's being typed).
  function fetchColours(f) {
    for (const { side, path } of colourSides(f)) {
      const key = f.path + '|' + side
      if (reviewView.colours.has(key)) continue
      const cancel = new AbortController()
      reviewView.colours.set(key, { loading: true, cancel })
      api('/api/review/highlight?path=' + encodeURIComponent(path) + '&side=' + side, { cancel })
        .then((res) => { reached(true); reviewView.colours.set(key, res.ok ? res.body : { error: (res.body && res.body.error) || 'vibe said no' }) },
          () => {
            if (!cancel.signal.aborted) reached(false)
            // One given up on may be asked for again before this lands.
            if ((reviewView.colours.get(key) || {}).cancel === cancel) reviewView.colours.delete(key)
          })
        .then(() => { if (!cancel.signal.aborted) redrawCard(f) })
    }
  }

  // abandonColours gives up on file f's colours still on their way, as it's
  // scrolled past: vibe stops colouring what nobody's looking at, and they're
  // asked for again if it comes back.
  function abandonColours(f) {
    for (const { side } of colourSides(f)) {
      const key = f.path + '|' + side
      const c = reviewView.colours.get(key)
      if (c && c.loading) { c.cancel.abort(); reviewView.colours.delete(key) }
    }
  }

  // redrawCard draws file f's card afresh, alone, as its colours come: a
  // whole redraw for each would take time growing with the square of the
  // review's size. Where the card can't be (a reply being written in it, the
  // files drawn since from another diff), the whole review is, as before.
  function redrawCard(f) {
    const d = reviewView.drawn
    const o = d && d.files === reviewView.files ? d.ordered.find((x) => x.file === f) : null
    const old = o && Array.from($('review-files').querySelectorAll('.review-file')).find((c) => c.dataset.path === f.path)
    if (!old || old.querySelector('textarea')) { reviewView.key = null; drawReview(); return }
    if (colourWatch) colourWatch.unobserve(old)
    old.replaceWith(foldIfViewed(f, fileCard(f, o.stop, o.step, d.files.indexOf(f), d.placed, d.threads)))
  }

  // colourWhenNear asks for file f's colours once its card is near the
  // screen (all at once where the browser can't tell): a big review's
  // files aren't all coloured up front.
  let colourWatch = null
  function colourWhenNear(f, card) {
    if (colourSides(f).every(({ side }) => reviewView.colours.has(f.path + '|' + side))) return
    if (!window.IntersectionObserver) { fetchColours(f); return }
    if (!colourWatch) {
      colourWatch = new IntersectionObserver((entries) => {
        for (const e of entries) {
          const file = (reviewView.files || []).find((x) => x.path === e.target.dataset.path)
          if (!file) continue
          if (e.isIntersecting) fetchColours(file)
          else abandonColours(file)
        }
      }, { rootMargin: '800px 0px' })
    }
    colourWatch.observe(card)
  }

  // codeCell is a line's code, coloured when its side's colouring has the
  // line as it reads (lineRuns), else plain.
  function codeCell(classes, f, side, n, text) {
    const coloured = reviewView.colours.get(f.path + '|' + side)
    const runs = coloured && coloured.lines ? R.lineRuns(coloured.lines[n - 1], text) : null
    if (!runs) return el('td', { class: classes, text })
    return el('td', { class: classes }, ...runs.map((r) => r.kind ? el('span', { class: 'hl-' + r.kind, text: r.text }) : r.text))
  }

  // uncoloured says, in a file's header, why it isn't coloured, when vibe
  // said why.
  function uncoloured(f) {
    const why = colourSides(f).map(({ side }) => reviewView.colours.get(f.path + '|' + side)).find((c) => c && (c.skipped || c.error))
    return why ? el('span', { class: 'muted review-uncoloured', text: 'not coloured: ' + (why.skipped || why.error) }) : null
  }

  // diffStat is a file's figures, on the right of its header, as GitHub
  // shows them: lines added and deleted, five squares of their share, and
  // how long the file is (or was, deleted). Nothing for a binary file.
  function diffStat(f) {
    if (f.binary) return null
    let adds = 0
    let dels = 0
    for (const h of f.hunks || []) for (const l of h.lines) { if (l.kind === 'add') adds++; else if (l.kind === 'del') dels++ }
    const length = f.lines ? (f.status === 'deleted' ? 'was ' : '') + plural(f.lines, 'line') : ''
    return el('span', { class: 'diffstat', title: plural(adds, 'line') + ' added, ' + dels + ' deleted' + (f.lines ? ', of ' + f.lines : '') },
      el('span', { class: 'adds', text: '+' + adds }), el('span', { class: 'dels', text: '−' + dels }),
      el('span', { class: 'squares' }, ...R.diffSquares(adds, dels).map((k) => el('span', { class: 'square ' + k }))),
      length ? el('span', { class: 'length muted', text: length }) : null)
  }

  // stepBadge is a file's number in the reading order, if it has one.
  function stepBadge(step, stop) {
    if (!step) return null
    return el('span', { class: 'review-step', text: String(step), title: 'Step ' + step + ' of the reading order' + (stop.label ? ': ' + stop.label : '') })
  }

  function fileCard(f, stop, step, index, placed, threads) {
    const after = placed.after[f.path] || {}
    const marked = placed.lines[f.path] || {}
    const total = (f.hunks || []).reduce((n, h) => n + h.lines.length, 0)
    const head = el('header', {}, el('code', { text: f.status === 'renamed' ? f.oldPath + ' → ' + f.path : f.path }),
      el('span', { class: 'muted', text: f.status }),
      stepBadge(step, stop), uncoloured(f),
      stop ? el('span', { class: 'review-stop', text: stop.label || 'in the reading order' }) : null,
      diffStat(f), viewedTick(f))
    const card = el('section', { class: 'review-file', id: 'review-file-' + index, 'data-path': f.path }, head)
    colourWhenNear(f, card)
    if (stop && stop.body) card.append(el('div', { class: 'note' }, markdown(stop.body)))
    // The lines the diff leaves out can be opened, as on GitHub, for a file
    // that's in it on both sides: an added or deleted one is shown whole.
    const expandable = f.status === 'modified' || f.status === 'renamed'
    const lines = reviewView.fileLines.get(f.path)
    const fileGaps = expandable ? R.gaps(f.hunks || [], lines ? lines.length : null) : []
    // Threads on lines the diff leaves out go under their lines, opened for
    // them, once the file's lines are here; until then, and those on no
    // line of it, above it.
    let gapped = { after: {}, marked: new Set(), rest: placed.offDiff[f.path] || [] }
    if (expandable && gapped.rest.length && f.hunks && f.hunks.length) {
      if (!lines) wantLines(f.path)
      else {
        gapped = R.placeInGaps(fileGaps, gapped.rest.map((id) => threads.get(id)), lines, f.hunks.length)
        // Opened at least that far: opening more goes on from there.
        const byGap = reviewView.opened.get(f.path) || {}
        for (const [i, r] of Object.entries(gapped.reveal)) {
          const g = byGap[i] || { down: 0, up: 0 }
          byGap[i] = { down: Math.max(g.down, r.down), up: Math.max(g.up, r.up) }
        }
        reviewView.opened.set(f.path, byGap)
        for (const id of gapped.outdated) reviewView.outdated.add(id)
      }
    }
    const off = gapped.rest
    if (off.length) card.append(el('div', { class: 'note' }, el('p', { class: 'muted', text: 'On lines this diff doesn\'t show:' }),
      ...off.map((id) => threadCard(threads.get(id)))))
    if (f.binary) { card.append(el('p', { class: 'note muted', text: 'A binary file: no lines to show.' })); return card }
    if (f.tooLarge) { card.append(el('p', { class: 'note muted', text: 'Too large to show.' })); return card }
    if (f.unreadable) { card.append(el('p', { class: 'note bad-text', text: "It couldn't be read: " + f.unreadable })); return card }
    const table = el('table', { class: 'review-lines' })
    // Fixed column widths, set by a colgroup: a fixed table takes its
    // widths from its first row otherwise, which is a hunk header spanning
    // them all, so every column came out the same width.
    const split = reviewView.layout === 'split'
    const cols = split ? ['num', 'sign', 'code', 'num', 'sign', 'code'] : ['num', 'num', 'sign', 'code']
    if (split) table.classList.add('split')
    const columns = String(cols.length)
    // A thread on an old line goes in the left pane, any other in the right.
    const threadRow = (ids) => {
      const old = ids.filter((id) => threads.get(id).side === 'old')
      const rest = ids.filter((id) => threads.get(id).side !== 'old')
      return sideRow(columns, old.map((id) => threadCard(threads.get(id))), rest.map((id) => threadCard(threads.get(id))))
    }
    const signOf = (kind) => kind === 'add' ? '+' : kind === 'del' ? '-' : ''
    const opened = reviewView.opened.get(f.path) || {}
    // contextRows is unchanged line n of the new file (old number n -
    // offset), opened from a gap, and the threads that end on it.
    const contextRows = (n, offset) => {
      const text = lines && n <= lines.length ? lines[n - 1] : ''
      const old = n - offset
      shownLine(f.path, 'old', old, text)
      shownLine(f.path, 'new', n, text)
      const half = (number, side) => {
        const sel = inComposer(f.path, side, number) ? ' sel' : ''
        return [el('td', { class: 'num context' + sel }, String(number), lineButton(f.path, side, number, text, { old, new: n })),
          el('td', { class: 'sign' + sel }), codeCell('code context' + sel, f, 'new', n, text)]
      }
      const classes = [gapped.marked.has(n) ? 'marked' : '', inComposer(f.path, 'old', old) || inComposer(f.path, 'new', n) ? 'selected' : ''].filter(Boolean).join(' ')
      const row = split
        ? el('tr', { class: classes }, ...half(old, 'old'), ...half(n, 'new').map((td, i) => { if (i === 0) td.classList.add('start'); return td }))
        : el('tr', { class: classes }, ...(() => {
          const sel = inComposer(f.path, 'old', old) || inComposer(f.path, 'new', n) ? ' sel' : ''
          return [el('td', { class: 'num context' + sel }, String(old), lineButton(f.path, 'new', n, text, { old, new: n })), el('td', { class: 'num context' + sel, text: String(n) }),
            el('td', { class: 'sign' + sel }), codeCell('code context' + sel, f, 'new', n, text)]
        })())
      const out = gapped.after[n] ? [row, threadRow(gapped.after[n])] : [row]
      if (composerAfter(f.path, 'old', old) || composerAfter(f.path, 'new', n)) out.push(composerRow(columns))
      return out
    }
    const GAP_BUTTONS = {
      down: { text: '↓', title: 'Show ' + R.GAP_STEP + ' more lines, carrying on from the lines above' },
      up: { text: '↑', title: 'Show ' + R.GAP_STEP + ' more lines, leading into the lines below' },
      all: { text: '↕', title: 'Show the hidden lines' },
    }
    // gapRows are a gap's rows: what's been opened at its top, its header
    // (with the hunk's own, header) while anything's still hidden, and what's
    // been opened at its foot.
    const gapRows = (g, end, header) => {
      if (!expandable) return header === null ? [] : [el('tr', { class: 'hunk' }, el('td', { colspan: columns, text: header }))]
      const v = R.gapView(g, opened[g.index] || { down: 0, up: 0 }, end)
      const rows = v.down.filter((n) => !lines || n <= lines.length).flatMap((n) => contextRows(n, g.offset))
      if (v.hidden !== 0 && !(end === 'last' && lines && v.hidden === null)) {
        const buttons = v.buttons.map((b) => el('button', { type: 'button', class: 'gap-open', text: GAP_BUTTONS[b].text,
          title: GAP_BUTTONS[b].title + (v.hidden ? ' (' + v.hidden + ' hidden)' : ''), 'aria-label': GAP_BUTTONS[b].title,
          onclick: () => openGap(f.path, g.index, b) }))
        rows.push(el('tr', { class: 'hunk' }, el('td', { class: 'gap-buttons' }, ...buttons),
          el('td', { colspan: String(cols.length - 1), text: header || '' })))
      }
      rows.push(...v.up.flatMap((n) => contextRows(n, g.offset)))
      return rows
    }
    const fill = () => {
      const rows = []
      ;(f.hunks || []).forEach((h, hi) => {
        const header = '@@ -' + h.oldStart + ' +' + h.newStart + ' @@ ' + (h.section || '')
        rows.push(...gapRows(fileGaps[hi], hi === 0 ? 'first' : '', header))
        if (split) {
          // Each side's line, or a blank where that side has none.
          const half = (index, number, start, row) => {
            if (index === null) return ['num', 'sign', 'code'].map((c) => el('td', { class: c + ' blank' + (start ? ' start' : '') }))
            const l = h.lines[index]
            const side = number === 'old' ? 'old' : 'new'
            shownLine(f.path, side, l[number], l.text)
            const sel = inComposer(f.path, side, l[number]) ? ' sel' : ''
            return [el('td', { class: 'num ' + l.kind + (start ? ' start' : '') + sel }, String(l[number] || ''), lineButton(f.path, side, l[number], l.text, row)),
              el('td', { class: 'sign ' + l.kind + sel, text: signOf(l.kind) }),
              codeCell('code ' + l.kind + sel, f, side, l[number], l.text)]
          }
          for (const row of R.sideBySide(h)) {
            const keys = [...new Set([row.left, row.right].filter((i) => i !== null).map((i) => hi + ':' + i))]
            const left = row.left === null ? null : h.lines[row.left].old
            const right = row.right === null ? null : h.lines[row.right].new
            const selected = inComposer(f.path, 'old', left) || inComposer(f.path, 'new', right)
            rows.push(el('tr', { class: [keys.some((k) => marked[k]) ? 'marked' : '', selected ? 'selected' : ''].filter(Boolean).join(' ') },
              ...half(row.left, 'old', false, { old: left, new: right }), ...half(row.right, 'new', true, { old: left, new: right })))
            const ids = [...new Set(keys.flatMap((k) => after[k] || []))]
            if (ids.length) rows.push(threadRow(ids))
            if (composerAfter(f.path, 'old', left) || composerAfter(f.path, 'new', right)) rows.push(composerRow(columns))
          }
          return
        }
        h.lines.forEach((l, li) => {
          const key = hi + ':' + li
          // One + per line: on the old file's line for a deleted one, else
          // the new file's.
          const side = l.kind === 'del' ? 'old' : 'new'
          if (l.old) shownLine(f.path, 'old', l.old, l.text)
          if (l.new) shownLine(f.path, 'new', l.new, l.text)
          const selected = inComposer(f.path, 'old', l.old) || inComposer(f.path, 'new', l.new)
          rows.push(el('tr', { class: [marked[key] ? 'marked' : '', selected ? 'selected' : ''].filter(Boolean).join(' ') },
            el('td', { class: 'num ' + l.kind + (selected ? ' sel' : '') }, l.old ? String(l.old) : '', lineButton(f.path, side, l[side], l.text, { old: l.old || null, new: l.new || null })),
            el('td', { class: 'num ' + l.kind + (selected ? ' sel' : ''), text: l.new ? String(l.new) : '' }),
            el('td', { class: 'sign ' + l.kind + (selected ? ' sel' : ''), text: signOf(l.kind) }),
            codeCell('code ' + l.kind + (selected ? ' sel' : ''), f, side, l[side], l.text)))
          if (after[key]) rows.push(threadRow(after[key]))
          if (composerAfter(f.path, 'old', l.old) || composerAfter(f.path, 'new', l.new)) rows.push(composerRow(columns))
        })
      })
      if (expandable && f.hunks && f.hunks.length) rows.push(...gapRows(fileGaps[f.hunks.length], 'last', null))
      table.replaceChildren(el('colgroup', {}, ...cols.map((c) => el('col', { class: c }))), ...rows)
    }
    // A file with comments is never folded: they'd be hidden; nor one with
    // a comment being written on it, nor one the user asked to see.
    const writingHere = reviewView.composer && reviewView.composer.path === f.path
    if (total > REVIEW_FOLD_LINES && !Object.keys(after).length && !Object.keys(gapped.after).length && !writingHere && !reviewView.unfolded.has(f.path)) {
      card.append(el('p', { class: 'note' }, el('button', { type: 'button', class: 'ghost',
        text: 'Show ' + total + ' lines', onclick: (ev) => { reviewView.unfolded.add(f.path); ev.target.parentNode.replaceWith(table); fill() } })))
    } else {
      fill()
      card.append(table)
    }
    return card
  }

  const saveEdit = once(async (t, c) => {
    const text = reviewView.editing ? reviewView.editing.text : ''
    try {
      const res = await doReview({ op: 'edit', thread: t.id, at: c.at, body: text })
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); return }
      reviewView.editing = null
      document.activeElement && document.activeElement.blur()
      redrawReview()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  })

  // editComment opens comment c of thread t to edit: asking first when
  // another edit has changes that would be lost.
  function editComment(t, c) {
    const start = () => { reviewView.editing = { thread: t.id, at: c.at, text: c.body }; redrawReview() }
    const was = reviewView.editing
    const lost = was && was.text.trim() !== originalOf(was).trim()
    if (!lost) { start(); return }
    confirmThen({ title: 'Discard your edit?', text: "The comment you're editing has changes that haven't been saved: they go.", commandNote: '', command: '',
      danger: true, runLabel: 'Discard', started: 'Discarded.',
      run: async () => { start(); return { ok: true, body: {} } } })
  }

  // originalOf is what the comment being edited says, as last saved.
  function originalOf(editing) {
    const t = reviewView.review && reviewView.review.threads.find((x) => x.id === editing.thread)
    const c = t && t.comments.find((x) => x.at === editing.at)
    return c ? c.body : ''
  }

  function deleteComment(t, i, c) {
    confirmThen({ title: 'Delete your comment?', text: i === 0 && t.comments.length === 1 ? 'It goes, with its thread. It was never sent, so the agent never saw it.' : 'It goes. It was never sent, so the agent never saw it.',
      commandNote: '', command: '', danger: true, runLabel: 'Delete', started: 'Deleted.',
      run: async () => {
        const res = await doReview({ op: 'delete', thread: t.id, at: c.at })
        if (res.ok) redrawReview()
        return res
      } })
  }

  // askFeedback is, on the thread last asked about while it's unanswered,
  // the agent at work on its answer, or that its turn ended without one.
  function askFeedback(t) {
    const ask = reviewView.ask
    const last = t.comments[t.comments.length - 1]
    if (!ask || ask.thread !== t.id || !last || last.author !== 'user') return null
    if (ask.answering) return el('div', { class: 'typing', role: 'status', 'aria-label': 'The agent is answering' }, el('span'), el('span'), el('span'))
    return el('p', { class: 'muted review-unanswered', text: "The agent's turn ended without answering here: the Conversation tab shows what it said." })
  }

  function threadCard(t) {
    const comments = t.comments.map((c, i) => {
      // Your comments can be edited; deleted only while the agent has
      // seen no version of them. One edited since the agent saw it says
      // so, and shows what it said, on asking.
      const yours = c.author === 'user'
      const edited = !!(c.was && c.was.length)
      const unsent = yours && !c.sent
      const editing = yours && reviewView.editing && reviewView.editing.thread === t.id && reviewView.editing.at === c.at
      const head = el('div', {}, el('span', { class: 'who', text: c.author === 'agent' ? 'Agent' : 'You' }), stamp(c.at),
        edited ? el('button', { type: 'button', class: 'ghost review-edited', text: 'edited', 'aria-expanded': String(reviewView.earlier.has(c.at)),
          title: 'Show what it said before', onclick: () => { toggle(reviewView.earlier, c.at); redrawReview() } }) : null,
        unsent ? el('span', { class: 'review-unsent', text: edited ? 'edit not sent yet' : 'not sent yet' }) : null,
        yours && !editing ? el('span', { class: 'comment-actions' },
          el('button', { type: 'button', class: 'ghost', text: 'Edit', onclick: () => editComment(t, c) }),
          // A thread's first comment goes only once its replies have.
          unsent && !edited && !(i === 0 && t.comments.length > 1) ? el('button', { type: 'button', class: 'ghost', text: 'Delete', onclick: () => deleteComment(t, i, c) }) : null) : null)
      const earlier = edited && reviewView.earlier.has(c.at)
        ? el('div', { class: 'review-earlier' }, ...c.was.slice().reverse().map((w, k) => el('div', {},
          el('div', { class: 'muted', text: k === 0 ? 'It said, before you edited it:' : 'And before that:' }), markdown(w))))
        : null
      if (!editing) return el('div', { class: 'comment' }, head, markdown(c.body), earlier)
      const box = el('textarea', { class: 'review-compose-text', 'aria-label': 'Edit your comment' })
      box.value = reviewView.editing.text
      box.addEventListener('input', () => { reviewView.editing.text = box.value })
      box.addEventListener('keydown', (ev) => {
        if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); saveEdit(t, c) }
      })
      return el('div', { class: 'comment' }, head, formattable(box), box, el('div', { class: 'bar' },
        el('button', { type: 'button', text: 'Save', title: 'Save (Ctrl+Enter)', onclick: () => saveEdit(t, c) }),
        el('button', { type: 'button', class: 'ghost', text: 'Cancel', onclick: () => { reviewView.editing = null; redrawReview() } })))
    })
    const where = t.file ? t.file + ':' + t.line + (t.endLine > t.line ? '–' + t.endLine : '') + (t.side === 'old' ? ' (old)' : '') : 'on the whole review'
    const box = el('textarea', { 'data-thread': String(t.id), placeholder: 'Reply… (Ctrl+Enter replies)', 'aria-label': 'Reply to comment ' + t.id })
    box.value = reviewView.drafts.get(String(t.id)) || ''
    box.addEventListener('input', () => reviewView.drafts.set(String(t.id), box.value))
    box.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); act('reply', true)() }
    })
    // byKeys: replied with Ctrl+Enter, which can go on to the next comment.
    // One at a time, whichever button (or Ctrl+Enter, held) asks.
    const act = (op, byKeys) => () => send(op, byKeys)
    const send = once(async (op, byKeys) => {
      const body = box.value.trim()
      if (op === 'reply' && !body) { box.focus(); return }
      let res
      try {
        res = await doReview({ op, thread: t.id, body: op === 'reopen' ? '' : body })
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
        return
      }
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); return }
      reviewView.drafts.delete(String(t.id))
      box.value = ''
      box.blur()
      drawReview()
      if (byKeys && reviewView.replyNext) goToCommentAfter(t.id)
    })
    // A turn: the answer comes back on this comment, from the agent's
    // vibe-review reply, and the turn shows on the Conversation tab too.
    const ask = async () => {
      const body = box.value.trim()
      if (!body) { box.focus(); toast('Type your question first.'); return }
      try {
        const res = await changeReview('/api/review/ask', { thread: t.id, body })
        reached(true)
        if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); return }
        reviewView.drafts.delete(String(t.id))
        box.value = ''
        box.blur()
        toast('Asked — the answer comes here, on comment #' + t.id + '.')
        loadReview(false)
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
      }
    }
    const open = t.status === 'open'
    return el('div', { class: 'review-thread ' + t.status, id: 'review-thread-' + t.id, tabindex: '-1' },
      el('div', { class: 'muted' }, '#' + t.id + ' · ' + where + (t.movedFrom ? ' · moved from line ' + t.movedFrom : '') + (open ? '' : ' · '),
        open ? null : el('span', { class: 'thread-state', text: t.status }),
        reviewView.outdated && reviewView.outdated.has(t.id) ? el('span', { class: 'review-outdated', text: ' · the code has changed since' }) : null),
      ...comments, askFeedback(t), formattable(box), box,
      el('div', { class: 'bar' },
        el('button', { type: 'button', text: 'Reply', onclick: act('reply') }),
        state.conversation ? el('button', { type: 'button', class: 'ghost', text: 'Ask the agent', title: 'Send your reply to the agent as a question: it answers here', onclick: ask }) : null,
        open ? el('button', { type: 'button', class: 'ghost', text: 'Resolve', title: 'With your reply, if you typed one', onclick: act('resolve') }) : null,
        open ? el('button', { type: 'button', class: 'ghost', text: 'Dismiss', title: 'With your reply, if you typed one', onclick: act('dismiss') }) : null,
        open ? null : el('button', { type: 'button', class: 'ghost', text: 'Reopen', onclick: act('reopen') })))
  }

  // --- the CPU and memory bars in the tab bar -------------------------------

  // A reading older than this is greyed: the sandbox is too busy to answer, or
  // gone.
  const USAGE_STALE_MS = 15000
  const GIB = 1024 * 1024 * 1024

  async function loadUsage() {
    try {
      const res = await api('/api/usage')
      reached(true)
      if (res.ok) renderUsage(res.body)
    } catch (e) {
      reached(false)
    }
  }

  const gib = (bytes) => (bytes / GIB).toFixed(1)
  const hotness = (pct) => (pct >= 90 ? 'hot' : pct >= 70 ? 'warm' : 'ok')
  const agoText = (ms) => (ms >= 90000 ? Math.round(ms / 60000) + 'm ago' : Math.round(ms / 1000) + 's ago')

  // meter draws one bar at pct (null: none); it's also what a screen reader is told.
  function meter(which, pct, name) {
    const m = $('usage-' + which)
    $('usage-' + which + '-fill').style.width = (pct === null ? 0 : pct) + '%'
    m.className = 'meter' + (pct === null ? '' : ' ' + hotness(pct))
    if (pct === null) {
      m.removeAttribute('aria-valuenow')
      m.setAttribute('aria-valuetext', 'not known')
    } else {
      m.setAttribute('aria-valuenow', String(Math.round(pct)))
      m.setAttribute('aria-valuetext', Math.round(pct) + '% ' + name)
    }
  }

  function renderUsage(b) {
    const box = $('usage')
    box.classList.remove('stale', 'off')
    if (b.state !== 'ok') {
      $('usage-cpu-label').textContent = 'CPU'
      $('usage-mem-label').textContent = 'MEM'
      meter('cpu', null, '')
      meter('mem', null, '')
      if (b.state === 'stopped') { box.classList.add('off'); box.title = 'The sandbox is stopped: there is nothing to measure.' }
      else if (b.state === 'unreachable') { box.classList.add('stale'); box.title = "The sandbox can't be read: " + (b.error || 'it did not answer') }
      else { box.classList.add('off'); box.title = 'Reading the sandbox…' }
      return
    }
    const cores = b.coresSetting || b.cores
    const coresText = cores + (cores === 1 ? ' core' : ' cores')
    $('usage-cpu-label').textContent = 'CPU (' + coresText + ')'
    $('usage-mem-label').textContent = 'MEM (' + (b.memSetting || gib(b.memTotal) + ' GiB') + ')'
    const memPct = b.memTotal ? (b.memUsed / b.memTotal) * 100 : null
    const cpuPct = b.cpu === null || b.cpu === undefined ? null : b.cpu
    meter('cpu', cpuPct, 'of the CPU in use')
    meter('mem', memPct, 'of the memory in use')
    const parts = [
      cpuPct === null ? 'CPU: measuring…' : 'CPU ' + Math.round(cpuPct) + '% of ' + coresText,
      'MEM ' + gib(b.memUsed) + ' of ' + gib(b.memTotal) + ' GiB (' + Math.round(memPct) + '%)',
      'read ' + agoText(b.ageMs),
    ]
    box.title = parts.join(' · ')
    if (b.ageMs > USAGE_STALE_MS || b.error) {
      box.classList.add('stale')
      box.title = 'Last read ' + agoText(b.ageMs) + ": the sandbox can't be read now (it may be too busy to answer)" +
        (b.error ? ': ' + b.error : '') + '. ' + parts.slice(0, 2).join(' · ')
    }
  }

  // --- the Admin tab ------------------------------------------------------

  // What the tab last showed: the server's answer, and the features as the
  // profile has them (to tell what the user has ticked since).
  const adminView = { data: null, features: '', jobLines: -1 }

  // --- signing in -------------------------------------------------------------

  // What the Sign-in card shows: whether sbx, and the agent, can be used, read by the host when the Admin tab is
  // opened and when Check now is pressed (each asks sbx, which can be slow, so it isn't polled).
  const signInShown = { asked: false, checking: false, watching: false, lastJob: '', agent: 'the agent', job: null, codeSent: false, sentPolls: 0 }
  // How many looks (1.5 s apart) at a code being checked before it can be entered again: a code that wasn't taken leaves the command waiting.
  const CODE_PATIENCE_POLLS = 15
  const SIGNIN_WORDS = { 'signed-in': ['Signed in', 'ok'], 'signed-out': ['Signed out', 'bad'], 'needs-policy': ['Needs a network policy', 'bad'], unknown: ['Unknown', ''], unsupported: ['Not checked', ''] }

  function renderSignInPart(id, name, part, kind) {
    const [word, cls] = SIGNIN_WORDS[part.state] || SIGNIN_WORDS.unknown
    const box = $(id)
    const steps = (part.manual || []).map((line) => el('li', { text: line }))
    const go = state.signinStart && part.state === 'signed-out'
      ? el('button', { type: 'button', class: 'signin-go', 'data-kind': kind, text: 'Sign in', disabled: signInShown.watching, onclick: () => startSignIn(kind) })
      : null
    box.replaceChildren(...[
      el('div', { class: 'signin-head' }, el('strong', { text: name }), el('span', { class: 'pill ' + cls, text: word }), go),
      part.detail ? el('p', { class: 'note muted', text: part.detail }) : null,
      steps.length ? el('details', {}, el('summary', { text: 'Do it yourself' }), el('ol', { class: 'steps' }, ...steps)) : null,
    ].filter(Boolean))
  }

  function signInProblem(text) {
    const status = $('signin-status')
    status.textContent = text
    status.hidden = !text
  }

  // The conversation says when sign-in is needed, as it is there that a prompt would fail: sbx's own first (nothing starts
  // without it), then the agent's. What could not be told is not said: only what is known to be signed out.
  function renderSignInNotice(parts) {
    const docker = (parts.docker || {}).state === 'signed-out'
    const agent = (parts.agentSignIn || {}).state === 'signed-out'
    const name = parts.agent || 'The agent'
    $('convo-signin').hidden = !(docker || agent)
    $('convo-signin-text').textContent = docker
      ? 'sbx is not signed in to Docker, so nothing can start. Sign in from the Admin tab.'
      : name + ' is not signed in, so it cannot answer yet. Sign in from the Admin tab.'
  }

  async function loadSignIn() {
    if (signInShown.checking) return
    signInShown.checking = true
    signInShown.asked = true
    $('signin-check').disabled = true
    try {
      const res = await api('/api/signin', { timeout: 30000 })
      reached(true)
      if (!res.ok) {
        signInProblem("Couldn't check: " + (res.body.error || 'HTTP ' + res.status))
        return
      }
      signInProblem('')
      signInShown.agent = res.body.agent || 'the agent'
      signInShown.parts = res.body
      renderSignInPart('signin-docker', 'Docker (sbx)', res.body.docker || { state: 'unknown' }, 'docker')
      renderSignInPart('signin-agent', signInShown.agent, res.body.agentSignIn || { state: 'unknown' }, 'agent')
      renderSignInNotice(res.body)
    } catch (e) {
      signInProblem("Couldn't check: " + (e && e.message ? e.message : 'the host did not answer'))
    } finally {
      signInShown.checking = false
      $('signin-check').disabled = false
    }
    // A sign-in begun before this page was opened (or reloaded) is picked up, not lost.
    if (state.signinStart && !signInShown.watching) await followSignIn(true)
  }
  $('signin-check').addEventListener('click', loadSignIn)
  $('convo-signin-open').addEventListener('click', () => select('admin'))

  async function startSignIn(kind) {
    signInProblem('')
    try {
      const res = await api('/api/signin/start', { method: 'POST', body: JSON.stringify({ kind }) })
      if (!res.ok) {
        signInProblem("Couldn't start signing in: " + (res.body.error || 'HTTP ' + res.status))
        return
      }
      signInShown.watching = true
      signInShown.lastJob = ''
      signInShown.codeSent = false
      signInShown.sentPolls = 0
      showSignInJob(res.body)
      if (signInShown.parts) {
        renderSignInPart('signin-docker', 'Docker (sbx)', signInShown.parts.docker || { state: 'unknown' }, 'docker')
        renderSignInPart('signin-agent', signInShown.agent, signInShown.parts.agentSignIn || { state: 'unknown' }, 'agent')
      }
    } catch (e) {
      signInProblem("Couldn't start signing in: " + (e && e.message ? e.message : 'the host did not answer'))
    }
  }

  // followSignIn reads the sign-in under way and shows it; with adopt, one that is running is taken up and followed.
  async function followSignIn(adopt) {
    let res
    try { res = await api('/api/signin/job') } catch (e) { return }
    if (!res.ok) return
    const job = res.body
    if (!job.kind || (!signInShown.watching && !(adopt && job.running))) return
    signInShown.watching = !!job.running || signInShown.watching
    if (job.running && signInShown.codeSent) signInShown.sentPolls += 1
    showSignInJob(job)
    if (job.done) {
      signInShown.watching = false
      if (job.ok) {
        loadSignIn()
        // Signed in, the next thing is to prompt the agent, which isn't obvious to someone new: go there, if still here.
        if (job.kind === 'agent' && state.tab === 'admin' && state.conversation) {
          toast('Signed in. You can prompt the agent now.')
          select('conversation')
        }
      }
    }
  }

  function pollSignInJob() {
    if (signInShown.watching) followSignIn(false)
  }
  setInterval(pollSignInJob, 1500)

  async function sendSignInCode() {
    const field = $('signin-job').querySelector('input')
    const code = field ? field.value.trim() : ''
    if (!code) return
    field.value = ''
    // The field is gone at once, so what is shown has to say it is being checked.
    signInShown.codeSent = true
    signInShown.sentPolls = 0
    if (signInShown.job) showSignInJob(signInShown.job)
    try {
      const res = await api('/api/signin/code', { method: 'POST', body: JSON.stringify({ code }) })
      if (!res.ok) signInProblem("Couldn't give it the code: " + (res.body.error || 'HTTP ' + res.status))
    } catch (e) {
      signInProblem("Couldn't give it the code: " + (e && e.message ? e.message : 'the host did not answer'))
    }
  }

  async function cancelSignIn() {
    try { await api('/api/signin/cancel', { method: 'POST', body: '{}' }) } catch (e) { /* the next poll says how it ended */ }
    followSignIn(false)
  }

  function closeSignInJob() {
    signInShown.lastJob = ''
    $('signin-job').hidden = true
    $('signin-job').replaceChildren()
  }

  // showSignInJob draws the sign-in under way in its place on the card: the address to open, the code to confirm or a field
  // for the one to paste, and what is happening. It is redrawn only when the job has changed, keeping what is being typed.
  function showSignInJob(job) {
    const box = $('signin-job')
    signInShown.job = job
    const late = signInShown.codeSent && signInShown.sentPolls >= CODE_PATIENCE_POLLS
    const key = JSON.stringify([job, signInShown.codeSent, late])
    if (key === signInShown.lastJob && !box.hidden) return
    signInShown.lastJob = key
    const typed = box.querySelector('input') ? box.querySelector('input').value : ''
    const who = job.kind === 'docker' ? 'Docker (sbx)' : signInShown.agent
    const parts = [el('strong', { text: (job.running ? 'Signing in to ' : 'Sign-in to ') + who })]
    const waiting = (text) => el('p', { class: 'note' }, el('span', { class: 'spinner', 'aria-hidden': 'true' }), text)
    const cancel = el('div', { class: 'bar' }, el('button', { type: 'button', class: 'signin-cancel ghost', text: 'Cancel', onclick: cancelSignIn }))
    if (job.running && !job.url) {
      // Nothing is said of an address the user can't see yet.
      parts.push(waiting('Starting the sign-in…'), cancel)
    } else if (job.running && job.kind === 'agent' && signInShown.codeSent) {
      parts.push(waiting('Checking the code…'))
      if (late) {
        parts.push(el('p', { class: 'note muted', text: "It's taking a while. If the code was wrong or didn't go through, you can enter it again." }),
          el('div', { class: 'bar' }, el('button', { type: 'button', class: 'signin-again', text: 'Enter the code again', onclick: () => { signInShown.codeSent = false; showSignInJob(job) } })))
      }
      parts.push(cancel)
    } else if (job.running) {
      parts.push(el('p', { class: 'note', text: job.kind === 'docker'
        ? 'Open the address, check the code matches, and sign in. This page notices when you have.'
        : 'Open the address and sign in; it then shows a code. Paste that code here.' }))
      parts.push(/^https:\/\//.test(job.url)
        ? el('p', {}, el('a', { href: job.url, target: '_blank', rel: 'noopener noreferrer', text: job.url }))
        : el('p', { class: 'note', text: job.url }))
      if (job.code) parts.push(el('p', {}, 'Code: ', el('code', { text: job.code })))
      if (job.kind === 'agent') {
        parts.push(el('div', { class: 'bar' },
          el('input', { type: 'password', autocomplete: 'off', placeholder: 'the code it showed', 'aria-label': 'The code the sign-in showed', onkeydown: (ev) => { if (ev.key === 'Enter') sendSignInCode() } }),
          el('button', { type: 'button', class: 'signin-send', text: 'Send code', onclick: sendSignInCode })))
      }
      parts.push(cancel)
    } else if (job.ok) {
      parts.push(el('p', { class: 'note', text: 'Signed in.' }), el('div', { class: 'bar' }, el('button', { type: 'button', class: 'ghost', text: 'Close', onclick: closeSignInJob })))
    } else {
      parts.push(el('p', { class: 'note bad-text', text: "Didn't sign in: " + (job.error || 'it ended without signing in') }),
        el('div', { class: 'bar' }, el('button', { type: 'button', class: 'ghost', text: 'Close', onclick: closeSignInJob })))
    }
    if ((job.lines || []).length) {
      parts.push(el('details', {}, el('summary', { text: 'What it printed' }), el('pre', { class: 'info', text: job.lines.join('\n') })))
    }
    box.replaceChildren(...parts)
    box.hidden = false
    const field = box.querySelector('input')
    if (field && typed) { field.value = typed; field.focus() }
  }

  async function loadAdmin() {
    try {
      const res = await api('/api/admin')
      reached(true)
      if (!res.ok) { problem('admin', res); return }
      banner('admin', '')
      renderAdmin(res.body)
    } catch (e) {
      reached(false)
    }
  }

  // --- making what the sandbox restored visible to the user's IDE -----------

  const syncView = { listKey: '', last: null, starting: false }

  // A fetch is heard of only once the host is under way with it, and a restore says nothing until
  // it ends: so the bar is up at its start (5%) from the click, and the rest of it is the packages
  // as they come into the cache, counted by the host.
  const PROGRESS_START = 5
  function showProgress(bar, got) {
    bar.max = 100
    bar.value = got && got.total > 0 ? PROGRESS_START + (100 - PROGRESS_START) * Math.min(got.done, got.total) / got.total : PROGRESS_START
  }
  const MAX_LISTED = 300

  async function loadSync() {
    try {
      const res = await api('/api/packages/sync')
      reached(true)
      // A sandbox with nothing of the kind has no such card.
      $('admin-sync-card').hidden = !res.ok
      showPackagesCard()
      if (res.ok) renderSync(res.body)
    } catch (e) {
      reached(false)
    }
  }

  // The packages are one card, of the agent's requests and of what the IDE can't see:
  // it's there when either is, and ringed when either wants the user.
  const packagesWant = { requests: false, sync: false }
  function showPackagesCard() {
    const card = $('admin-packages-all')
    card.hidden = $('admin-packages-card').hidden && $('admin-sync-card').hidden
    card.classList.toggle('wants-you', packagesWant.requests || packagesWant.sync)
  }

  const packagesWord = (n) => n + (n === 1 ? ' package' : ' packages')
  const packageName = (p) => p.id + ' ' + p.version

  function renderSync(b) {
    syncView.last = b
    const count = b.count || 0
    const running = !!b.running || syncView.starting
    $('admin-sync-summary').textContent = count
      ? packagesWord(count) + " the sandbox restored " + (count === 1 ? "isn't" : "aren't") + " in your package cache, so your IDE (Rider, say) can't find " +
        (count === 1 ? 'it' : 'them') + '. Fetching uses your own NuGet config on this machine, as for a package the agent asks for.'
      : (b.stale || 0) > 0
        ? 'Everything the sandbox restored is in your package cache.'
        : 'Everything the sandbox restored is in your package cache, so your IDE can find it.'
    $('admin-sync-details').hidden = count === 0
    const key = JSON.stringify(b.missing || [])
    if (key !== syncView.listKey) {
      syncView.listKey = key
      $('admin-sync-list').replaceChildren(...(b.missing || []).flatMap((g) => {
        // What was only downloaded (a targeting pack, say) is as missing as what was resolved.
        const all = (g.packages || []).concat(g.downloads || [])
        const shown = all.slice(0, MAX_LISTED).map(packageName).join(', ')
        const more = all.length > MAX_LISTED ? ' … and ' + (all.length - MAX_LISTED) + ' more' : ''
        return [el('p', { text: g.framework + ' (' + all.length + ')' }), el('p', { class: 'muted', text: shown + more })]
      }))
    }
    $('admin-sync-run').disabled = running || count === 0
    const stale = !running && !(b.actionable > 0) ? b.stale || 0 : 0
    $('admin-sync-stale').hidden = stale === 0
    $('admin-sync-stale-ask').hidden = !state.conversation
    if (stale) $('admin-sync-stale-text').textContent = staleText(stale, b.staleProjects || [])
    packagesWant.sync = (b.actionable || 0) > 0
    showPackagesCard()
    const problems = b.problems || []
    $('admin-sync-problems').hidden = problems.length === 0
    $('admin-sync-problems').textContent = problems.length ? "Couldn't read: " + problems.join('; ') : ''

    const status = $('admin-sync-status')
    const last = b.last
    status.classList.remove('bad-text')
    const bar = $('admin-sync-progress')
    bar.hidden = !running
    if (running) {
      // The host counts them in the cache; without a count the bar is of unknown length.
      const got = b.progress
      showProgress(bar, got)
      status.hidden = false
      status.textContent = got && got.total > 0 && got.done >= got.total
        ? 'Finishing… all ' + got.total + ' are in your cache; the restore is wrapping up.'
        : 'Fetching…' + (got && got.total > 0 ? ' ' + got.done + ' of ' + got.total + ' packages are in your cache.' : '') +
          ' The host downloads them with your own NuGet config; this can take a few minutes.'
    } else if (last && last.error && count > 0) {
      // Said only while something is still missing: once it's all come, however
      // (a retry, the IDE's own restore), a failure from before would say the card is wrong.
      status.hidden = false
      status.classList.add('bad-text')
      status.textContent = last.error + (last.detail ? '\n' + last.detail : '')
    } else if (last && !last.error) {
      status.hidden = false
      status.textContent = packagesWord(last.fetched || 0) + ' fetched' +
        ((last.unresolved || []).length ? '; no feed has: ' + last.unresolved.map(packageName).join(', ') : '') + '.' +
        // The files the sandbox generated name its own cache for what it downloaded itself.
        (count === 0 && (last.fetched || 0) > 0 ? ' Your IDE may restore once, to point its files at your cache instead of the sandbox\'s.' : '')
    } else {
      status.hidden = true
    }
    const failed = !running && count > 0 && last && last.failed ? last.failed : []
    $('admin-sync-failed').hidden = failed.length === 0
    $('admin-sync-failed').replaceChildren(...failed.map((f) => el('li', {}, el('code', { text: packageName(f.package) }), ' ', el('span', { class: 'was', text: f.message }))))
  }

  async function runSync() {
    $('admin-sync-run').disabled = true
    syncView.starting = true
    if (syncView.last) renderSync(syncView.last)
    try {
      const res = await post('/api/packages/sync/run', {})
      reached(true)
      syncView.starting = false
      if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); if (syncView.last) renderSync(syncView.last) }
      else renderSync(res.body)
    } catch (e) {
      reached(false)
      syncView.starting = false
      toast('Could not reach vibe.', true)
      if (syncView.last) renderSync(syncView.last)
    }
  }

  // --- package requests on the Admin tab -----------------------------------

  // unticked is the keys of the requests the user has unticked: all are
  // ticked until then, and a poll doesn't undo it.
  const packagesView = { key: '', unticked: new Set(), config: '', byKey: new Map() }

  async function loadPackages() {
    try {
      const res = await api('/api/packages')
      reached(true)
      // A sandbox that can't ask for packages has no such card.
      $('admin-packages-card').hidden = !res.ok
      showPackagesCard()
      if (res.ok) renderPackages(res.body)
    } catch (e) {
      reached(false)
    }
  }

  // The conversation says when the agent is waiting on the user for
  // packages: it can't, itself, while its command waits for the approval.
  function renderPackagesWaiting(waiting) {
    $('convo-packages').hidden = waiting === 0
    $('convo-packages-text').textContent = waiting === 1 ? '1 package request is waiting for your approval.'
      : waiting + ' package requests are waiting for your approval.'
  }

  // The conversation says too when packages the sandbox restored could be
  // fetched for the user's IDE: it can't find them until the user allows it.
  function renderSyncWaiting(count) {
    $('convo-sync').hidden = count === 0
    $('convo-sync-text').textContent = packagesWord(count) + " the sandbox restored " + (count === 1 ? "isn't" : "aren't") +
      " in your package cache, so your IDE can't find " + (count === 1 ? 'it' : 'them') + '.'
  }

  // Once the packages are in the user's cache, the files the sandbox generated still name its own
  // copies, which the IDE can't see. Nothing in the user's terms says so, so it's said here until
  // the files are generated again (the host looks at them), with the way to do that.
  const TIDY_PROMPT = 'The packages are in my package cache now. Please run vibe-nuget status, tidy up if it says to, ' +
    'and restore, so the generated files name my cache.'
  // inProjects says which projects, so that one outside the solution (which restoring the solution
  // leaves as it was) can be found.
  const inProjects = (projects) => !projects.length ? '' :
    ' (in ' + plural(projects.length, 'project') + ': ' + projects.slice(0, 3).join(', ') +
    (projects.length > 3 ? ' and ' + (projects.length - 3) + ' more' : '') + ')'

  const staleText = (count, projects) => packagesWord(count) + ' the sandbox restored' + inProjects(projects) + ' ' + (count === 1 ? 'is' : 'are') +
    ' in your package cache now, but the files it generated still point at its own ' + (count === 1 ? 'copy' : 'copies') +
    ", so your IDE can't find " + (count === 1 ? 'it' : 'them') + ' yet. ' +
    (state.conversation ? 'Ask the agent to tidy up, or restore in your IDE.' : 'Restore in your IDE.')

  // STALE_DISMISSED keeps the projects the user said not to be told of again, as a JSON list, per
  // browser and sandbox. A project not in it brings the notice back.
  const STALE_DISMISSED = 'vibe-companion-stale-dismissed'
  const MAX_STALE_DISMISSED = 200
  const staleNow = { count: 0, projects: [] }

  function dismissedStale() {
    try {
      const list = JSON.parse(store.get(STALE_DISMISSED) || '[]')
      return Array.isArray(list) ? list : []
    } catch (e) { return [] }
  }

  // staleNames is the projects the notice is about, "(unknown)" for a vibe that doesn't say.
  const staleNames = (projects) => (projects.length ? projects : ['(unknown)'])

  function renderStaleWaiting(count, projects) {
    Object.assign(staleNow, { count, projects })
    const dismissed = dismissedStale()
    $('convo-stale').hidden = count === 0 || staleNames(projects).every((name) => dismissed.includes(name))
    $('convo-stale').title = projects.join('\n')
    if (count) $('convo-stale-text').textContent = staleText(count, projects)
  }

  function dismissStale() {
    store.set(STALE_DISMISSED, JSON.stringify(dismissedStale().concat(staleNames(staleNow.projects)).slice(-MAX_STALE_DISMISSED)))
    renderStaleWaiting(staleNow.count, staleNow.projects)
  }

  // Sent at once when the agent is free: pressing the button is the user's choice. While it works
  // the message waits in the box (Send is off then), unless the box holds something already.
  async function askAgentToTidy() {
    select('conversation')
    const input = $('convo-input')
    if (convo.running) {
      if (!input.value.trim()) { input.value = TIDY_PROMPT; promptChanged() }
      toast('The agent is working: the message is in the box for you to send when it is done.')
      return
    }
    const draft = input.value
    await deliverPrompt(TIDY_PROMPT)
    if (draft) { input.value = draft; promptChanged() }
  }

  function describeRequest(r) {
    return r.ecosystem + ' ' + r.id + ' ' + r.version + (r.framework ? ' (' + r.framework + ')' : '')
  }

  // The buttons act on requests not yet decided: approved ones are queued
  // or being fetched, and can't be taken back.
  function packageButtons() {
    const undecided = Array.from($('admin-packages').querySelectorAll('input:not(:disabled)'))
    const ticked = undecided.filter((b) => b.checked).length
    const left = undecided.length - ticked
    // The button says what it will do: what's ticked is allowed, and what isn't is left out.
    const allow = $('admin-packages-allow')
    allow.disabled = ticked === 0
    allow.textContent = ticked === 0 ? 'Allow selected'
      : left > 0 ? 'Allow ' + ticked + ', deny ' + left
      : ticked === 1 ? 'Allow it' : 'Allow all ' + ticked
    $('admin-packages-deny').disabled = undecided.length === 0
  }

  function renderPackages(data) {
    const requests = data.requests || []
    const keys = new Set(requests.map((r) => r.key))
    packagesView.byKey = new Map(requests.map((r) => [r.key, r]))
    for (const k of packagesView.unticked) if (!keys.has(k)) packagesView.unticked.delete(k)
    const key = JSON.stringify(requests)
    if (key !== packagesView.key) {
      packagesView.key = key
      $('admin-packages').replaceChildren(...requests.map((r) => {
        const box = el('input', { type: 'checkbox', 'data-key': r.key, disabled: !!r.approved })
        box.checked = !packagesView.unticked.has(r.key)
        box.addEventListener('change', () => {
          if (box.checked) packagesView.unticked.delete(r.key)
          else packagesView.unticked.add(r.key)
          packageButtons()
        })
        const state = r.fetching ? 'fetching…' : r.approved ? 'approved — about to be fetched' : r.note || ''
        return el('li', {}, el('label', {}, box, el('span', { text: describeRequest(r) }), el('span', { class: 'was', text: state })))
      }))
    }
    const approved = requests.filter((r) => r.approved).length
    const undecided = requests.length - approved
    $('admin-packages-none').hidden = requests.length > 0
    $('admin-packages-bar').hidden = requests.length === 0
    $('admin-packages-status').hidden = approved === 0
    $('admin-packages-status').textContent = approved + ' approved — being fetched together.' +
      (undecided ? ' ' + undecided + ' more to decide.' : '')
    packagesWant.requests = undecided > 0
    showPackagesCard()
    pill('admin-packages-count', requests.length ? String(undecided) + ' waiting' + (approved ? ' · ' + approved + ' approved' : '') : '', undecided ? 'bad' : '')
    packageButtons()
    renderNugetConfig(data.config, data.configProblem)
  }

  function renderNugetConfig(config, problemText) {
    const box = $('admin-nugetconfig')
    box.hidden = !config && !problemText
    if (!config) { $('admin-nugetconfig-note').textContent = problemText || ''; return }
    const key = JSON.stringify(config)
    if (key === packagesView.config) return
    packagesView.config = key
    $('admin-nugetconfig-note').textContent = config.problem ? config.problem
      : config.approved ? 'Approved: vibe uses it, as it reads now, for fetches. Any change asks again.'
      : "Not approved, so vibe ignores it for fetches. It can name feeds (and change more than that): read it before you approve it."
    $('admin-nugetconfig-sources').replaceChildren(...(config.sources || []).map((s) => el('li', {}, el('code', { text: s.name }), ' ', s.url)))
    $('admin-nugetconfig-warnings').replaceChildren(...(config.redefines || []).map((w) => el('li', { text: 'Warning: ' + w })))
    $('admin-nugetconfig-text').textContent = config.text || ''
    const approve = $('admin-nugetconfig-approve')
    approve.hidden = config.approved || !config.hash
    approve.dataset.hash = config.hash || ''
  }

  // packagesDecide sends the user's word to vibe and, once it is taken, says what it was: the request
  // leaves the list as it's decided, so without the line nothing would show what was done.
  async function packagesDecide(path, decision, line) {
    // At once, not at the next poll: the answer shows what happened.
    $('admin-packages-allow').disabled = true
    $('admin-packages-deny').disabled = true
    try {
      const res = await post(path, decision)
      reached(true)
      if (!res.ok) toast((res.body && res.body.error) || 'That failed.', true)
      else {
        renderPackages(res.body)
        $('admin-packages-decision').textContent = line
        $('admin-packages-decision').hidden = false
      }
      loadState()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  const requestName = (key) => {
    const r = packagesView.byKey.get(key)
    return r ? r.id + ' ' + r.version : key
  }

  function decisionLine(allow, notSelected) {
    return 'Allowed ' + allow.length +
      (notSelected.length ? '; left out ' + notSelected.map(requestName).join(', ') + ': the agent will ask you about ' + (notSelected.length === 1 ? 'it' : 'them') : '') + '.'
  }

  function wirePackages() {
    $('admin-sync-run').addEventListener('click', runSync)
    $('convo-gate-fetch').addEventListener('click', async () => {
      await startGateFetch()
      renderGate()
    })
    $('convo-gate-send').addEventListener('click', () => {
      const prompt = gate.prompt
      closeGate()
      deliverPrompt(prompt)
    })
    $('convo-gate-cancel').addEventListener('click', closeGate)
    // Escape closes the dialog: that's Cancel, the prompt kept.
    $('convo-gate').addEventListener('close', () => { if (gate.active) closeGate() })
    $('convo-gate-review').addEventListener('click', () => {
      closeGate()
      select('admin')
      $('admin-packages-all').scrollIntoView({ block: 'start' })
      tick()
    })
    $('convo-blocked-open').addEventListener('click', () => select('log'))
    $('convo-blocked-dismiss').addEventListener('click', dismissBlocked)
    $('convo-stale-ask').addEventListener('click', askAgentToTidy)
    $('convo-stale-dismiss').addEventListener('click', dismissStale)
    $('admin-sync-stale-ask').addEventListener('click', askAgentToTidy)
    $('convo-sync-review').addEventListener('click', () => {
      select('admin')
      $('admin-sync-card').scrollIntoView({ block: 'start' })
      tick()
    })
    $('convo-packages-review').addEventListener('click', () => {
      select('admin')
      $('admin-packages-card').scrollIntoView({ block: 'start' })
      tick()
    })
    $('admin-packages-allow').addEventListener('click', () => {
      const undecided = Array.from($('admin-packages').querySelectorAll('input:not(:disabled)'))
      const allow = undecided.filter((b) => b.checked).map((b) => b.dataset.key)
      const notSelected = undecided.filter((b) => !b.checked).map((b) => b.dataset.key)
      if (!allow.length) { toast('Nothing is ticked.', true); return }
      packagesDecide('/api/packages/decide', { allow, notSelected }, decisionLine(allow, notSelected))
    })
    $('admin-packages-deny').addEventListener('click', () => {
      const n = $('admin-packages').querySelectorAll('input:not(:disabled)').length
      packagesDecide('/api/packages/decline-all', {}, 'Declined ' + n + ": the agent will tell you and ask what you'd like instead.")
    })
    $('admin-nugetconfig-approve').addEventListener('click', async () => {
      try {
        const res = await post('/api/packages/approve-config', { hash: $('admin-nugetconfig-approve').dataset.hash })
        reached(true)
        if (!res.ok) toast((res.body && res.body.error) || 'That failed.', true)
        else renderPackages(res.body)
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
      }
    })
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
    note.textContent = 'Select what the sandbox should have; re-composing rebuilds it. New features go after the ones it has.'
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
    $('admin-upgrades-card').classList.toggle('wants-you', found.length > 0)
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

  // What the toast says is under way once an action has begun: a bare
  // "Started" after Stop reads as the sandbox starting.
  const UNDER_WAY = { stop: 'Stopping', rebuild: 'Rebuilding', update: 'Updating', recompose: 'Re-composing' }
  const ADMIN_UNDER_WAY = {
    stop: 'Stopping the sandbox', reinit: 'Rebuilding the sandbox',
    recompose: 'Re-composing the profile and rebuilding the sandbox', restore: 'Restoring the backup and rebuilding the sandbox',
  }
  const adminProgress = () => showProgress('admin-job-card', loadAdmin)
  const manageProgress = () => showProgress('manage-job-card', loadManage)

  function underWay(kind, what) {
    return (UNDER_WAY[kind] ? UNDER_WAY[kind] + ' ' + what : 'Under way') + '…'
  }

  function adminConfirm(kind, extra, opts) {
    const commands = (adminView.data && adminView.data.status && adminView.data.status.commands) || {}
    confirmThen(Object.assign({
      command: commands[kind] || '',
      started: (ADMIN_UNDER_WAY[kind] || 'Under way') + '…',
      progress: adminProgress,
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
    wirePackages()
    $('admin-cancel').addEventListener('click', () => {
      if (!REBUILDS.includes(adminView.jobKind)) { cancelAdmin(); return }
      confirmThen({
        title: 'Cancel the rebuild?',
        text: 'Stopping part-way may leave the sandbox half-made. Afterwards, the card says what state it is in, and Rebuild makes it again from its profile.',
        command: 'vibe --cancel',
        danger: true,
        started: 'Cancelling…',
        progress: adminProgress,
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
  // The filters are kept for this tab, so a reload (or vibe restarting) keeps them.
  const MANAGE_FILTER = 'vibe-manage-filter'
  const MANAGE_STATE = 'vibe-manage-state'
  const manageView = {
    inv: null, key: '', ticked: new Set(), jobLines: -1, busy: false,
    filter: sessionGet(MANAGE_FILTER), state: sessionGet(MANAGE_STATE) || 'all',
  }

  function sessionGet(key) {
    try { return sessionStorage.getItem(key) || '' } catch (e) { return '' }
  }

  function sessionSet(key, value) {
    try { sessionStorage.setItem(key, value) } catch (e) { /* kept for this page only */ }
  }

  // marked is text with the filter's words in it marked, as text nodes and
  // <mark>s: never markup made from it.
  function marked(text) {
    const wanted = manageView.filter.toLowerCase().split(/\s+/).filter(Boolean)
    const lower = String(text).toLowerCase()
    const hit = new Array(lower.length).fill(false)
    for (const w of wanted) {
      for (let i = lower.indexOf(w); i >= 0; i = lower.indexOf(w, i + 1)) hit.fill(true, i, i + w.length)
    }
    const nodes = []
    let start = 0
    for (let i = 1; i <= lower.length; i++) {
      if (i === lower.length || hit[i] !== hit[start]) {
        const part = String(text).slice(start, i)
        nodes.push(hit[start] ? el('mark', { class: 'mgr-hit', text: part }) : document.createTextNode(part))
        start = i
      }
    }
    return nodes
  }
  const M = window.vibeManage

  async function loadManage() {
    try {
      // Taking stock asks sbx, which vibe gives 20 seconds.
      const res = await api('/api/manage', { timeout: 30000 })
      reached(true)
      if (!res.ok) { problem('manage', res); return }
      banner('manage', '')
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
    stop: { label: 'Stop…', ask: (name, isProfile, row) => row.attached ? {
      title: 'Stop sandbox ' + name + ', and the vibe attached to it?',
      text: "The vibe working in it ends first, saving the agent's memories — and with it, its terminal session, or its page if it was started with --headless. Then the sandbox stops.",
      command: 'vibe <its folder>, then choose to stop the other vibe' } : {
      title: 'Stop sandbox ' + name + '?',
      text: 'The agent stops.',
      command: 'vibe --stop <its folder>' } },
    rebuild: { label: 'Rebuild…', ask: (name) => ({
      title: 'Rebuild sandbox ' + name + '?',
      text: "It's destroyed and made again from its profile, which takes a few minutes, and left running or stopped as it was. The agent's memories are kept; anything installed by hand, or kept outside its folder, goes.",
      command: 'vibe -r <its folder>' }) },
    startvibe: { label: 'Start vibe…', power: 'start' },
    restartvibe: { label: 'Restart vibe…', power: 'restart' },
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
    if (a.power) { askPowerForRow(a.power, row); return }
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
      started: underWay(kind, (isProfile ? 'profile ' : 'sandbox ') + row.name),
      progress: manageProgress,
      run,
    }, a.ask(row.name, isProfile, row)))
  }

  function keptNote(row) {
    return row.kept ? el('span', { class: 'mgr-kept muted', text: "can't be deleted: " + row.kept }) : null
  }

  function drawManage() {
    const inv = manageView.inv
    const list = $('manage-list')
    if (!inv) return
    const view = M.filtered(inv, manageView.filter, manageView.state)
    $('manage-shown').textContent = manageView.filter.trim() || manageView.state !== 'all' ? 'showing ' + view.shown + ' of ' + view.total + ' sandboxes' : ''
    const groups = view.profiles.map((p) => {
      const head = el('div', { class: 'mgr-row mgr-head' },
        tickBox(p, el('span', {}, el('strong', {}, ...marked(p.name)),
          p.features && p.features.length ? el('span', { class: 'muted', text: ' ' + p.features.join(', ') }) : null)),
        ...flagPills(p.flags), keptNote(p), el('span', { class: 'spacer' }), ...actionButtons(p, true))
      const sandboxes = (p.sandboxes || []).map((s) => el('li', { class: 'mgr-row' },
        tickBox(s, el('span', {}, el('span', {}, ...marked(s.name), s.own ? ' (this page)' : ''),
          el('code', { class: 'mgr-folder' }, ...marked(s.folder))), M.coveredBy(inv, manageView.ticked, s.id)),
        ...flagPills(s.flags), keptNote(s), el('span', { class: 'spacer' }), ...actionButtons(s, false)))
      return el('div', { class: 'mgr-profile' }, head, sandboxes.length ? el('ul', { class: 'mgr-sandboxes' }, ...sandboxes) : null)
    })
    if (!groups.length && !inv.error) {
      groups.push(el('p', { class: 'muted', text: (inv.profiles || []).length ? 'No profile matches the filters.' : 'No profiles yet.' }))
    }
    const unknown = view.unknown
    if (unknown.length) {
      groups.push(el('h3', { class: 'mgr-unknown-h', text: 'Sandboxes vibe has no record of' }),
        el('ul', { class: 'mgr-sandboxes' }, ...unknown.map((u) => el('li', { class: 'mgr-row' },
          tickBox(u, el('span', {}, ...marked(u.name))), ...flagPills([u.running ? 'running' : 'stopped'])))))
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
        title: 'Delete ' + M.summary(manageView.inv, manageView.ticked).replace(/ selected$/, '') + '?',
        text: "There's no undo. This deletes:",
        list: res.body.lines || [],
        commandNote: 'From a terminal, vibe --cleanup does the same.',
        command: '',
        danger: true,
        runLabel: 'Delete',
        started: 'Deleting…',
        progress: manageProgress,
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
    $('review-refresh').addEventListener('click', refreshReview)
    $('review-afresh').addEventListener('click', startAfresh)
    $('review-whitespace').checked = reviewView.ignoreSpace
    $('review-whitespace').addEventListener('change', (ev) => {
      reviewView.ignoreSpace = ev.target.checked
      store.set(REVIEW_WHITESPACE, reviewView.ignoreSpace ? 'ignore' : 'show')
      // The hunks are another shape: what was opened in them goes.
      forgetFiles()
      loadReview(true)
    })
    setUpReviewTree()
    $('review-close').addEventListener('click', () => closeReview())
    setUpCommentsPane()
    setUpLineDrag()
    setUpOrderMenu()
    $('panel-review').addEventListener('paste', reviewPaste)
    // Scrolling by hand (not the arrows' scrolling) says where the page is.
    const panel = $('panel-review')
    const byHand = () => { reviewView.steering = false; reviewView.fileSteering = false }
    panel.addEventListener('wheel', byHand, { passive: true })
    panel.addEventListener('touchstart', byHand, { passive: true })
    panel.addEventListener('pointerdown', (ev) => { if (ev.target === panel) byHand() }) // its scrollbar
    panel.addEventListener('keydown', (ev) => {
      if (['PageUp', 'PageDown', 'Home', 'End', 'ArrowUp', 'ArrowDown', ' '].includes(ev.key) && !typingInto(ev.target)) byHand()
    })
    panel.addEventListener('scroll', () => {
      if (!reviewView.steering) countComments()
      if (!reviewView.fileSteering) measureCurrentFile()
    }, { passive: true })
    $('review-reopen').addEventListener('click', reopenReview)
    $('review-prev-comment').addEventListener('click', () => goToComment('prev'))
    $('review-next-comment').addEventListener('click', () => goToComment('next'))
    $('review-reply-next').checked = reviewView.replyNext
    $('review-reply-next').addEventListener('change', (ev) => {
      reviewView.replyNext = ev.target.checked
      store.set(REVIEW_REPLY_NEXT, reviewView.replyNext ? 'on' : 'off')
    })
    // Clicking or typing anywhere in the review is looking at it: an update
    // that comes while it's in view is marked until then.
    for (const type of ['pointerdown', 'keydown']) $('panel-review').addEventListener(type, reviewSeen, true)
    $('review-submit').addEventListener('click', once(async () => {
      try {
        const res = await changeReview('/api/review/submit', {})
        reached(true)
        if (!res.ok) { toast((res.body && res.body.error) || 'That failed.', true); return }
        const n = res.body.sent || 0
        toast('Sent ' + plural(n, 'comment') + ' to the agent: it answers on them, and says here what it did.')
        loadReview(false)
        select('conversation')
      } catch (e) {
        reached(false)
        toast('Could not reach vibe.', true)
      }
    }))
    // The tree sticks under the title bar, and fits in what's left of the
    // window: both measured, as the bar wraps on a narrow window.
    const measure = () => {
      const panel = $('panel-review')
      const bar = panel.querySelector(':scope > .bar')
      const barH = bar.offsetHeight
      panel.style.setProperty('--review-bar-h', barH + 'px')
      panel.style.setProperty('--review-file-head-h', fileHeadHeight() + 'px')
      panel.style.setProperty('--review-pane-top', Math.max(0, bar.getBoundingClientRect().bottom) + 'px')
      panel.style.setProperty('--review-tree-max', Math.max(160, window.innerHeight - panel.getBoundingClientRect().top - barH) + 'px')
    }
    if (window.ResizeObserver) new ResizeObserver(measure).observe($('panel-review').querySelector(':scope > .bar'))
    window.addEventListener('resize', measure)
    measure()
    showReviewLayout()
    $('review-layout').addEventListener('click', () => {
      reviewView.layout = reviewView.layout === 'split' ? 'unified' : 'split'
      store.set(REVIEW_LAYOUT, reviewView.layout)
      showReviewLayout()
      redrawReview()
    })
    const showState = () => {
      for (const b of $('manage-state').querySelectorAll('button')) b.setAttribute('aria-pressed', String(b.dataset.state === manageView.state))
    }
    showState()
    for (const b of $('manage-state').querySelectorAll('button')) {
      b.addEventListener('click', () => {
        manageView.state = b.dataset.state
        sessionSet(MANAGE_STATE, manageView.state)
        showState()
        drawManage()
      })
    }
    const filter = $('manage-filter')
    filter.value = manageView.filter
    filter.addEventListener('input', () => {
      manageView.filter = filter.value
      sessionSet(MANAGE_FILTER, filter.value)
      drawManage()
    })
    filter.addEventListener('keydown', (ev) => {
      if (ev.key === 'Escape' && filter.value) {
        ev.preventDefault()
        filter.value = ''
        filter.dispatchEvent(new Event('input'))
      }
    })
    // / finds, as on many sites: from anywhere on the tab but a box being
    // typed into.
    document.addEventListener('keydown', (ev) => {
      if (ev.key !== '/' || ev.ctrlKey || ev.metaKey || ev.altKey || $('panel-manage').hidden || typingInto(document.activeElement)) return
      ev.preventDefault()
      filter.focus()
      filter.select()
    })
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
      const res = await post('/api/admin/instructions', payload, { timeout: 60000 })
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
    // A toast showing stays over the dialog.
    raiseToast()
  }

  async function optimiseInstructions(ed) {
    const started = Date.now()
    ed.running = new AbortController()
    const tickTime = () => setStatus(ed, 'Optimising… ' + Math.round((Date.now() - started) / 1000) + 's — the agent is reading it', '', true)
    tickTime()
    ed.timer = setInterval(tickTime, 1000)
    ed.refresh()
    try {
      const res = await post('/api/admin/instructions/optimise', { which: ed.file.which, text: ed.box.value }, {
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

  // news: the agent has said something since the Conversation tab was last
  // open (its dot); loaded: the conversation so far has been fetched once.
  const convo = { last: 0, running: false, polling: false, behind: false, news: false, loaded: false, lastPrompt: '', ticks: new Map(), typing: null, group: null }

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
    // inLink: in a link's text, where a button can't go.
    const inline = (nodes, inLink) => nodes.flatMap((n) => {
      switch (n.type) {
        case 'text': return inLink ? document.createTextNode(visible(n.text)) : withThreadRefs(visible(n.text))
        case 'code': return !inLink && state.viewFiles && looksLikeFile(n.text) ? fileCandidate(n.text) : el('code', { text: visible(n.text) })
        case 'br': return el('br')
        case 'strong': return el('strong', {}, ...inline(n.children, inLink))
        case 'em': return el('em', {}, ...inline(n.children, inLink))
        case 'del': return el('del', {}, ...inline(n.children, inLink))
        case 'link': {
          const link = localLink(n.href)
          if (link) { link.replaceChildren(...inline(n.children, true)); return link }
          const label = n.children.map((c) => c.text || '').join('')
          const shown = visible(label === n.href ? n.href : label + ' (' + n.href + ')')
          const file = state.viewFiles && !inLink ? fileLinkPath(n.href) : null
          return file ? fileCandidate(file, shown, visible(label)) : document.createTextNode(shown)
        }
      }
      return document.createTextNode('')
    })
    const blocks = (list) => list.map((b) => {
      switch (b.type) {
        case 'para': return el('p', {}, ...inline(b.inline))
        case 'heading': return el('h' + Math.min(6, Math.max(1, b.level)), {}, ...inline(b.inline))
        case 'code': return codeBlock(b.text)
        case 'hr': return el('hr')
        case 'quote': return el('div', { class: 'quote-block' }, el('blockquote', {}, ...blocks(b.blocks)),
          el('button', { type: 'button', class: 'copy-code small', title: 'Copy the quote as Markdown', 'aria-label': 'Copy the quote as Markdown', onclick: () => copyText(b.source) }, copyIcon()))
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

  // looksLikeFile is whether code the agent wrote reads as a path to a file: no spaces, a name with an
  // extension at its end. Nothing is known of it until the host is asked, so this only decides what is
  // offered; a version (1.2.3) or an address with no folder in it (a@b.com) isn't.
  function looksLikeFile(text) {
    if (text.length > 200 || !/^[\w@+~.\/-]+$/.test(text) || !/\.[A-Za-z0-9]{1,10}$/.test(text)) return false
    if (/^[\d.]+$/.test(text) || (!text.includes('/') && text.includes('@'))) return false
    return !text.endsWith('/')
  }

  // Code that reads like a path is only code until the host says it is a file it can show: one ask, for
  // everything drawn at that moment, and then each one that is becomes a button. What isn't stays as it was
  // (and is not asked about again): not everything that reads like a path is one (safe.directory).
  const FILES_PER_ASK = 25
  let fileCheckQueued = false

  // The folder of the file the viewer is showing while its text is drawn, from the workspace ('' for its
  // root or when it isn't showing one): a path in it that starts with ./ or ../ is from that file.
  let viewedFolder = ''

  // pathFrom is path as the host knows it: from the workspace. One written ./x or ../x in a file in
  // folder (the viewer's) is from that file, so is worked out from folder; any other is already from the
  // workspace, as the agent writes them. A path that climbs out of the workspace keeps its ..s, for the host to refuse.
  function pathFrom(folder, path) {
    if (!folder || !/^\.\.?\//.test(path)) return path
    const parts = folder.split('/')
    for (const part of path.split('/')) {
      if (part === '.') continue
      if (part === '..') { if (parts.length && parts[parts.length - 1] !== '..') parts.pop(); else parts.push('..') } else parts.push(part)
    }
    return parts.join('/')
  }

  // fileLinkPath is the file a Markdown link's address names, when it is a reference to one on this machine
  // ("./other.md", "docs/a.md#top"): no scheme, not a network address, no query; null for anything else.
  function fileLinkPath(href) {
    if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith('//') || href.startsWith('#')) return null
    let path = href.replace(/[#?].*$/, '')
    try { path = decodeURIComponent(path) } catch (e) { return null }
    return looksLikeFile(path) ? path : null
  }

  // fileCandidate is a path the host has yet to say is a file: code showing it, or (for a Markdown link to
  // it) its words, with label for what the button says once it is one.
  function fileCandidate(path, text = path, label = '') {
    if (!fileCheckQueued) { fileCheckQueued = true; queueMicrotask(checkFileCandidates) }
    const props = { class: 'file-candidate', text, 'data-path': pathFrom(viewedFolder, path) }
    if (label) props['data-label'] = label
    return el(label ? 'span' : 'code', props)
  }

  async function checkFileCandidates() {
    fileCheckQueued = false
    const nodes = Array.from(document.querySelectorAll('.file-candidate'))
    nodes.forEach((n) => n.classList.remove('file-candidate'))
    const paths = Array.from(new Set(nodes.map((n) => n.dataset.path)))
    const files = new Set()
    for (let i = 0; i < paths.length; i += FILES_PER_ASK) {
      try {
        const res = await api('/api/workspace-files?' + paths.slice(i, i + FILES_PER_ASK).map((p) => 'path=' + encodeURIComponent(p)).join('&'))
        if (res.ok && Array.isArray(res.body.files)) res.body.files.forEach((f) => files.add(f))
        else console.warn("vibe couldn't tell which paths are files it can show:", res.body && res.body.error)
      } catch (err) {
        console.warn("vibe couldn't tell which paths are files it can show:", err.message)
      }
    }
    for (const n of nodes) if (n.isConnected && files.has(n.dataset.path)) n.replaceWith(fileRef(n.dataset.path, n.dataset.label || n.textContent, !n.dataset.label))
  }

  // fileRef is a path the agent named, as a button showing that file over the conversation. It is never a
  // link: the agent chooses the path, so what comes of clicking is only the host reading it, if it is in the
  // workspace, and the page showing it as text.
  function fileRef(path, shown = path, asCode = true) {
    const words = asCode ? el('code', { text: shown }) : el('span', { text: shown })
    return el('button', { type: 'button', class: 'file-ref', title: shown === path ? 'Show this file' : 'Show ' + path, onclick: () => viewFile(path) }, words)
  }

  // The viewer keeps a history as a browser does: each file followed from a link in it is a step on from the
  // last, Back and Forward move along the steps, and following a link from a step back along drops those that
  // were ahead. Each step keeps how far down it was read, to go back to. Closing the viewer ends it: the next file
  // opened is the first of a new one.
  const viewer = { steps: [], at: -1, loading: 0 }

  // viewFile shows path over the conversation: from a button in it, the first step of a new history; from a
  // link in a file already shown, the next step of that one.
  function viewFile(path) {
    const dialog = $('file-view')
    if (dialog.open) {
      rememberPlace()
      viewer.steps.length = viewer.at + 1
      viewer.steps.push({ path, place: 0 })
      viewer.at++
    } else {
      viewer.steps = [{ path, place: 0 }]
      viewer.at = 0
    }
    return showStep()
  }

  function rememberPlace() {
    if (viewer.steps[viewer.at]) viewer.steps[viewer.at].place = $('file-view-body').scrollTop
  }

  async function goViewer(to) {
    if (to < 0 || to >= viewer.steps.length) return
    rememberPlace()
    viewer.at = to
    await showStep()
  }

  function drawViewerButtons() {
    $('file-view-back').disabled = viewer.at <= 0
    $('file-view-forward').disabled = viewer.at >= viewer.steps.length - 1
  }

  // showStep shows the step the history is at, read-only: as Markdown if it is, else as it is. The host says
  // why when it won't (outside the workspace, not there, too big, not text), and that is shown in its place.
  async function showStep() {
    const dialog = $('file-view')
    const step = viewer.steps[viewer.at]
    const path = step.path
    const mine = ++viewer.loading
    $('file-view-path').value = path
    $('file-view-path').title = path
    endOfPath()
    drawViewerButtons()
    dialog.classList.remove('outside')
    $('file-view-copy').hidden = true
    $('file-view-body').replaceChildren(el('p', { class: 'muted', text: 'Loading…' }))
    if (!dialog.open) dialog.showModal()
    endOfPath()
    let res
    try {
      res = await api('/api/workspace-file?path=' + encodeURIComponent(path))
    } catch (err) {
      res = { ok: false, body: { error: "Couldn't ask vibe for it: " + err.message } }
    }
    // Another step was asked for meanwhile.
    if (mine !== viewer.loading) return
    if (!res.ok) {
      dialog.classList.toggle('outside', res.body.code === 'outside')
      $('file-view-body').replaceChildren(el('p', { text: res.body.error || 'That file could not be shown.' }))
      $('file-view-body').scrollTop = step.place
      return
    }
    const text = res.body.text
    $('file-view-copy').hidden = false
    // It narrows the path bar.
    endOfPath()
    $('file-view-copy').onclick = () => copyText(text)
    viewedFolder = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''
    try {
      $('file-view-body').replaceChildren(...(/\.(md|markdown)$/i.test(path) ? [markdown(text)] : codeView(text, res.body.colour)))
    } finally {
      viewedFolder = ''
    }
    $('file-view-body').scrollTop = step.place
  }

  // codeView is a file that isn't Markdown, as text: coloured where the host gave its colours (a line at a
  // time, by what each run of it is: a line they no longer spell is plain), saying why where it didn't colour it.
  function codeView(text, colour) {
    const note = colour && colour.skipped ? [el('p', { class: 'muted', text: 'Not coloured: ' + colour.skipped })] : []
    if (!colour || !Array.isArray(colour.lines)) return [...note, el('pre', {}, el('code', { text: visible(text) }))]
    const lines = text.replace(/\r\n/g, '\n').split('\n')
    const parts = []
    lines.forEach((line, i) => {
      const runs = R.lineRuns(colour.lines[i], line)
      if (runs) for (const r of runs) parts.push(r.kind ? el('span', { class: 'hl-' + r.kind, text: visible(r.text) }) : visible(r.text))
      else parts.push(visible(line))
      if (i < lines.length - 1) parts.push('\n')
    })
    return [...note, el('pre', {}, el('code', {}, ...parts))]
  }

  // endOfPath scrolls the path bar to the end of the path, where the file's name is, when it is too long for the
  // bar, and says so (the start fades out). Left to right as it is, not by turning the text round, which would
  // move a leading dot (.github/...) to the other end. Done again when the bar changes size.
  function endOfPath() {
    const bar = $('file-view-path')
    bar.scrollLeft = bar.scrollWidth
    bar.classList.toggle('cut', bar.scrollWidth > bar.clientWidth)
  }
  if (window.ResizeObserver) new ResizeObserver(endOfPath).observe($('file-view-path'))

  // viewerIcon is a small drawing for the viewer's buttons, as copyIcon is for copying.
  function viewerIcon(d) {
    const ns = 'http://www.w3.org/2000/svg'
    const svg = document.createElementNS(ns, 'svg')
    svg.setAttribute('viewBox', '0 0 16 16')
    svg.setAttribute('aria-hidden', 'true')
    const path = document.createElementNS(ns, 'path')
    path.setAttribute('d', d)
    svg.append(path)
    return svg
  }

  $('file-view-copy').append(copyIcon())
  $('file-view-back').append(viewerIcon('M10 3 L5 8 L10 13'))
  $('file-view-forward').append(viewerIcon('M6 3 L11 8 L6 13'))
  $('file-view-reload').append(viewerIcon('M13 8a5 5 0 1 1-1.6-3.7 M13 2.5v3h-3'))
  $('file-view-close').append(viewerIcon('M4 4l8 8 M12 4l-8 8'))
  $('file-view-back').onclick = () => goViewer(viewer.at - 1)
  $('file-view-forward').onclick = () => goViewer(viewer.at + 1)
  $('file-view-reload').onclick = () => { rememberPlace(); return showStep() }
  $('file-view-close').onclick = () => $('file-view').close()

  // HIDDEN_CONTROL is what a terminal acts on but a page doesn't show: the
  // control characters bar tab and newline. In code the agent wrote, one can
  // hide a command (an escape ending a terminal's bracketed paste, so the
  // rest runs), so a code block shows them and its copy leaves them out.
  const HIDDEN_CONTROL = /[\x00-\x08\x0b-\x1f\x7f-\x9f]/g

  // controlPicture is the symbol Unicode has for control character c (␛ for
  // escape); ␦ for the C1 controls, which have none.
  function controlPicture(c) {
    const code = c.charCodeAt(0)
    if (code < 0x20) return String.fromCharCode(0x2400 + code)
    return code === 0x7f ? '\u2421' : '\u2426'
  }

  // visible is text with its hidden control characters shown as symbols.
  function visible(text) {
    return text.replace(/\r\n/g, '\n').replace(HIDDEN_CONTROL, controlPicture)
  }

  // codeBlock is a block of code with a button copying it: selecting it by
  // hand means scrolling, as Ctrl+A takes the whole conversation.
  function codeBlock(code) {
    const text = code.replace(/\r\n/g, '\n')
    return el('div', { class: 'code-block' }, el('pre', {}, el('code', { text: visible(text) })),
      el('button', { type: 'button', class: 'copy-code small', title: 'Copy', 'aria-label': 'Copy the code', onclick: () => copyText(text) }, copyIcon()))
  }

  // copyText puts text on the user's clipboard, leaving out hidden control characters (an escape
  // that ends a terminal's bracketed paste would make what follows run), and says what it did. A
  // page has no clipboard unless it is on localhost or HTTPS: a forwarded port is neither.
  async function copyText(text) {
    const hidden = text.match(HIDDEN_CONTROL) || []
    if (!navigator.clipboard) {
      toast("Couldn't copy: the browser only lets a page copy from localhost or HTTPS. Open this page at http://localhost:<port>, not by a forwarded address.", true)
      return
    }
    try {
      await navigator.clipboard.writeText(text.replace(HIDDEN_CONTROL, ''))
      toast(hidden.length ? 'Copied, leaving out ' + plural(hidden.length, 'hidden control character') : 'Copied')
    } catch (err) {
      toast("Couldn't copy: " + err.message, true)
    }
  }

  // copyIcon is two overlapping pages, the copy a step down and right.
  function copyIcon() {
    const ns = 'http://www.w3.org/2000/svg'
    const svg = document.createElementNS(ns, 'svg')
    svg.setAttribute('viewBox', '0 0 16 16')
    svg.setAttribute('aria-hidden', 'true')
    const path = document.createElementNS(ns, 'path')
    path.setAttribute('d', 'M2.5 10.5v-8h8 M5.5 5.5h8v8h-8z')
    svg.append(path)
    return svg
  }

  // withThreadRefs is text as nodes, each review comment it names (#24, not
  // in a word: issue#3, C#4 aren't) a link to that comment on the Review tab.
  function withThreadRefs(text) {
    const nodes = []
    let at = 0
    // Not in a word (issue#3), a path or address (example.com/#4), nor a
    // number going on (#1.5); shown as written (#007).
    for (const m of text.matchAll(/(^|[^\w#/])#(\d+)(?![\w.]*\d)\b/g)) {
      const start = m.index + m[1].length
      const id = Number(m[2])
      if (start > at) nodes.push(document.createTextNode(text.slice(at, start)))
      nodes.push(el('button', { type: 'button', class: 'thread-ref', 'data-thread': String(id), title: 'Go to comment #' + id + ' on the Review tab',
        text: '#' + m[2], onclick: () => openThread(id) }))
      at = start + 1 + m[2].length
    }
    if (at < text.length) nodes.push(document.createTextNode(text.slice(at)))
    return nodes
  }

  // openThread goes to comment id on the Review tab: once the review is
  // there to go to, when it hasn't been loaded yet.
  function openThread(id) {
    reviewView.goTo = id
    select('review')
    goToWanted()
  }

  // goToWanted goes to the comment openThread asked for, once the review
  // and its diff are drawn; one the review hasn't is said not to be there.
  function goToWanted() {
    const id = reviewView.goTo
    if (id == null || !reviewView.review || reviewView.files === null) return
    reviewView.goTo = null
    if (!reviewIsOn(reviewView.review)) {
      toast('Comment #' + id + " is on a review that's closed: reopen it to see its comments.")
      return
    }
    if (!$('review-thread-' + id)) {
      toast("There's no comment #" + id + ' on this review: it may have been on an earlier one.')
      return
    }
    goToThread(id)
  }

  // seq, when given, marks the line as that event of the conversation, so vibe
  // can take it back (see the retract event).
  function convoNote(cls, text, at, seq) {
    endToolGroup()
    const time = stamp(at)
    const words = cls === 'agent' ? markdown(text) : el('span', { text })
    // What the agent said can be copied whole, as the Markdown it wrote: its footer has the button.
    const copy = cls === 'agent'
      ? el('button', { type: 'button', class: 'copy-message small ghost', title: 'Copy this message', 'aria-label': 'Copy this message', onclick: () => copyText(text) }, copyIcon())
      : null
    const mark = seq == null ? {} : { 'data-seq': String(seq) }
    $('convo-log').append(time || copy
      ? el('div', Object.assign({ class: 'msg ' + cls }, mark), words, el('span', { class: 'foot' }, time, copy))
      : el('div', Object.assign({ class: 'msg ' + cls }, mark), words))
  }

  // The ticks on what the user said, as on a chat message: one when vibe has
  // it, two when the sandbox has it, two in blue once the agent has started
  // answering.
  const RECEIPT_TICKS = { sent: '✓', delivered: '✓✓', seen: '✓✓', undelivered: '✕' }
  const RECEIPT_WORDS = { sent: 'Sent', delivered: 'Delivered', seen: 'The agent is answering', undelivered: 'Not given to the agent' }

  function showReceipt(seq, receipt) {
    const ticks = convo.ticks.get(seq)
    if (!ticks || !RECEIPT_TICKS[receipt]) return
    ticks.textContent = RECEIPT_TICKS[receipt]
    ticks.className = 'ticks ' + receipt
    ticks.title = RECEIPT_WORDS[receipt]
    ticks.setAttribute('aria-label', RECEIPT_WORDS[receipt])
  }

  // Whether the user's prompts are shown as Markdown, as the agent's
  // replies are (and as the agent reads them), or as plain text. Either way,
  // what was sent is the text as typed; only how it's shown changes. On
  // unless the user turned it off, in this browser.
  const promptsAsMarkdown = { on: store.get('vibe-prompts-markdown') !== 'off' }

  function promptText(text) {
    return promptsAsMarkdown.on ? markdown(text) : el('span', { class: 'plain', text })
  }

  // What the Review tab's "Submit to agent" sends is long, and the user has its context: the first
  // line says what it is, and the rest is folded away, to open if wanted, not to be read. A prompt
  // is one of those when it begins with the words the page sends it with.
  const REVIEW_PROMPT_START = 'On the Review tab, '

  function promptBody(text) {
    const line = text.indexOf('\n')
    const rest = line < 0 ? '' : text.slice(line + 1).trim()
    if (!text.startsWith(REVIEW_PROMPT_START) || !rest) return promptText(text)
    return el('div', { class: 'prompt-folded' },
      el('div', { class: 'prompt-first' }, promptText(text.slice(0, line))),
      el('details', { class: 'prompt-more' }, el('summary', { text: 'Show what was sent' }), promptText(rest)))
  }

  function userNote(ev) {
    endToolGroup()
    const ticks = el('span', { class: 'ticks' })
    convo.ticks.set(ev.seq, ticks)
    const text = ev.text || ''
    const msg = el('div', { class: 'msg user' }, promptBody(text), el('span', { class: 'foot' }, stamp(ev.at), ticks))
    msg.prompt = text
    $('convo-log').append(msg)
    showReceipt(ev.seq, ev.receipt || 'sent')
  }

  // setPromptsAsMarkdown shows every prompt on the page afresh, the new way.
  function setPromptsAsMarkdown(on) {
    promptsAsMarkdown.on = on
    store.set('vibe-prompts-markdown', on ? 'on' : 'off')
    for (const msg of $('convo-log').querySelectorAll('.msg.user')) {
      if (typeof msg.prompt === 'string') msg.firstChild.replaceWith(promptBody(msg.prompt))
    }
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

  // The tools the agent uses between what it says are gathered in one group, open while the agent is
  // working, so what it does is seen as it happens, and collapsed once it speaks (or the turn ends), so
  // what it said is not lost among them. Each tool's line and result are inside, as they always were.
  const GROUP_NAMES_SHOWN = 5

  function toolGroup() {
    if (convo.group) return convo.group
    const summary = el('summary')
    const body = el('div', { class: 'tool-group-body' })
    const group = el('details', { class: 'tool-group' }, summary, body)
    group.open = true
    group.body = body
    group.tally = { uses: 0, failed: 0, names: new Map() }
    // A group the user has opened or closed is as they left it.
    summary.addEventListener('click', () => { group.touched = true })
    $('convo-log').append(group)
    convo.group = group
    drawToolGroup(group)
    return group
  }

  function drawToolGroup(group) {
    const { uses, failed, names } = group.tally
    const ranked = Array.from(names).sort((a, b) => b[1] - a[1])
    const shown = ranked.slice(0, GROUP_NAMES_SHOWN).map(([name, n]) => name + ' ×' + n)
    const more = ranked.length - shown.length
    group.querySelector('summary').textContent = '⚙ ' + (uses ? plural(uses, 'tool use') : 'tool results') +
      (shown.length ? ' · ' + shown.join(', ') + (more > 0 ? ', and ' + more + ' more' : '') : '') +
      (failed ? ' · ' + failed + ' failed' : '')
    group.classList.toggle('bad', failed > 0)
  }

  // endToolGroup is the agent having said something, or the turn having ended: the tools it used so far
  // are put away.
  function endToolGroup() {
    const group = convo.group
    convo.group = null
    if (group && !group.touched) group.open = false
  }

  function toolUsed(block) {
    const group = toolGroup()
    const name = block.name || 'tool'
    group.tally.uses++
    group.tally.names.set(name, (group.tally.names.get(name) || 0) + 1)
    drawToolGroup(group)
    group.body.append(el('div', { class: 'msg tool' }, el('span', { text: '⚙ ' + name + '  ' + summarise(block.input) })))
  }

  function showResult(block) {
    let text = resultText(block.content)
    if (!text && !block.is_error) return
    const group = toolGroup()
    if (block.is_error) { group.tally.failed++; drawToolGroup(group) }
    if (!text) return
    if (text.length > RESULT_SHOWN) text = text.slice(0, RESULT_SHOWN) + '\n… (' + (text.length - RESULT_SHOWN) + ' more characters)'
    group.body.append(el('details', { class: 'tool-result' + (block.is_error ? ' bad' : '') },
      el('summary', { text: block.is_error ? 'tool error' : 'tool result' }),
      el('pre', { text })))
  }

  function showAgent(line, at) {
    if (!line || typeof line !== 'object') return
    const content = line.message && Array.isArray(line.message.content) ? line.message.content : []
    if (line.type === 'assistant') {
      for (const block of content) {
        if (block.type === 'text' && block.text) convoNote('agent', block.text, at)
        else if (block.type === 'tool_use') toolUsed(block)
      }
      // The agent says to run /login, which needs its terminal, which this page can't show: the Sign-in card on the Admin
      // tab does the same without one (where the host offers it).
      if (line.error === 'authentication_failed') {
        if (state.signinStart) {
          convoNote('error', "The agent isn't signed in. Sign in from the Admin tab, then send your prompt again.", at)
          $('convo-log').lastElementChild.append(el('button', { type: 'button', class: 'small ghost', text: 'Open Sign-in', onclick: () => select('admin') }))
          // A sign-in that has expired since the page was opened: the card and the notice find out.
          loadSignIn()
        } else {
          convoNote('error', "The agent isn't signed in, and can't be from this page. In a terminal, run vibe in the sandbox's folder and sign in there with /login, then send your prompt again.", at)
        }
      }
    } else if (line.type === 'user') {
      for (const block of content) if (block.type === 'tool_result') showResult(block)
    } else if (line.type === 'result') {
      endToolGroup()
      if (line.is_error) {
        convoNote('error', typeof line.result === 'string' && line.result ? line.result : 'The agent reported an error.', at)
      }
      const bits = []
      if (typeof line.duration_ms === 'number') bits.push((line.duration_ms / 1000).toFixed(1) + 's')
      if (typeof line.total_cost_usd === 'number') bits.push('$' + line.total_cost_usd.toFixed(4))
      if (bits.length) convoNote('meta', bits.join(' · '))
    } else if (line.type === 'system' && line.subtype === 'compact_boundary') {
      convoNote('meta', 'The agent compacted its context: it now works from a summary of the conversation so far.')
    } else if (line.type === 'truncated') {
      convoNote('meta', '(that was too long to show)')
    }
  }

  function showEvent(ev) {
    if (ev.kind === 'reset') { $('convo-log').replaceChildren(); convo.ticks.clear(); convo.group = null }
    else if (ev.kind === 'user') { convo.lastPrompt = ev.text || ''; userNote(ev) }
    else if (ev.kind === 'agent') showAgent(ev.agent, ev.at)
    else if (ev.kind === 'note') convoNote('meta', ev.text || '', undefined, ev.seq)
    // What vibe said it was doing, no longer so (the sandbox is up): gone, not left to be read as current.
    else if (ev.kind === 'retract') $('convo-log').querySelectorAll('[data-seq="' + Number(ev.of) + '"]').forEach((n) => n.remove())
    else if (ev.kind === 'error') convoNote('error', ev.text || 'That failed.', ev.at)
    else if (ev.kind === 'done' && ev.text === 'stopped') convoNote('meta', 'Stopped.')
  }

  // running is the agent answering; background is it having answered, but
  // still waiting on something it left running (a review page, say). Then
  // the page goes on as if it were done: sending ends that work.
  // quietMinutes, when not 0, is how long the agent has been answering
  // without a word: long enough that it may be stuck. The turn isn't
  // stopped — that's the user's call. compacting is the agent making room
  // in a full context first, which can take minutes with nothing to show.
  const LIGHT_USE = 'This light is on while the agent is working on a prompt, or has left something running; it is off when the agent is idle.'

  // drawAgentLight shows whether the agent is busy, in the tab bar, so it can be
  // seen from every tab, whether or not the conversation has said anything lately.
  function drawAgentLight(running, background, quietMinutes, compacting) {
    const light = $('agent-light')
    light.hidden = !state.conversation
    const on = running || background
    light.classList.toggle('on', on)
    light.classList.toggle('busy', running)
    light.classList.toggle('background', !running && background)
    light.classList.toggle('quiet', running && quietMinutes > 0)
    const now = running && compacting ? 'The agent is compacting its context.'
      : running && quietMinutes ? 'The agent is working on a prompt, but there has been no output for ' + quietMinutes + ' minutes: it may be stuck.'
      : running ? 'The agent is working on a prompt.'
      : background ? 'The agent has answered and left something running in the sandbox.'
      : 'The agent is idle.'
    light.title = now + ' ' + LIGHT_USE
    light.setAttribute('aria-label', light.title)
  }

  function setConvoRunning(running, background, quietMinutes, compacting) {
    const ended = convo.running && !running
    drawAgentLight(running, background, quietMinutes, compacting)
    if (running) setFaviconDot('working')
    else if (convo.running) setFaviconDot(document.visibilityState === 'visible' && document.hasFocus() ? '' : 'done')
    convo.running = running
    // What the agent ran is known now, and what was chosen has been used.
    if (ended && state.choice) choiceTurnEnded()
    $('convo-send').disabled = running
    // Only while it answers: what it has left running ends with the next
    // prompt anyway.
    $('convo-stop').hidden = !running
    $('convo-new').disabled = running
    // While it answers, the typing bubble says so.
    const status = $('convo-status')
    status.classList.toggle('warn-text', !!(running && quietMinutes))
    status.textContent = running && compacting
      ? 'The agent is compacting its context (summarising the conversation so far to make room). This can take a few minutes; it answers once it is done.'
      : running && quietMinutes
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
      if (!res.ok) { problem('conversation', res); return }
      banner('conversation', '')
      const log = $('convo-log')
      const stick = nearBottom(log)
      if (res.body.reset) { log.replaceChildren(); convo.ticks.clear(); convo.group = null }
      if (Array.isArray(res.body.commands)) {
        slash.commands = res.body.commands.filter((c) => typeof c === 'string')
        slash.seq = res.body.commandsSeq || 0
        if (slash.open) updateCommands(false)
      }
      for (const ev of res.body.events || []) showEvent(ev)
      if (res.body.receipt) showReceipt(res.body.receipt.seq, res.body.receipt.state)
      convo.last = res.body.last || 0
      // A turn that isn't running has no tools still being used, whatever the last thing said was.
      if (!res.body.running) endToolGroup()
      setConvoRunning(!!res.body.running, !!res.body.background, res.body.quietMinutes || 0, !!res.body.compacting)
      if ((res.body.events || []).length) {
        if ($('panel-conversation').hidden) {
          convo.behind = true
          // What was said before the page was opened isn't news.
          if (convo.loaded) convo.news = true
        } else if (stick) log.scrollTop = log.scrollHeight
      }
      convo.loaded = true
      showAllAttention()
      showTyping()
      startedIfReady()
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

  // fitPrompt grows the prompt box with what's in it, so a long prompt can
  // be read over whole before it's sent, up to half the conversation's
  // column: the log keeps the other half. Past that, the box scrolls.
  function fitPrompt() {
    const box = $('convo-input')
    // The log above gives up the room the box takes: one read to its end
    // stays at its end, or its last lines slide out of sight under the box
    // (and resizing it on every focus would put them there again).
    const log = $('convo-log')
    const atEnd = nearBottom(log)
    const cap = Math.max(box.offsetHeight, Math.floor(box.closest('.convo').clientHeight / 2))
    // Back to its own size first, so it can shrink as well as grow.
    box.style.height = ''
    const borders = box.offsetHeight - box.clientHeight
    const want = box.scrollHeight + borders
    if (want > box.offsetHeight) box.style.height = Math.min(want, cap) + 'px'
    if (atEnd) log.scrollTop = log.scrollHeight
  }

  function promptChanged() {
    saveDraft()
    fitPrompt()
  }

  async function sendPrompt() {
    const input = $('convo-input')
    const prompt = input.value.trim()
    if (!prompt || convo.running || gate.active) return
    if (await holdForPackages(prompt)) return
    await deliverPrompt(prompt)
  }

  async function deliverPrompt(prompt) {
    const input = $('convo-input')
    try {
      const res = await post('/api/conversation/send', { prompt })
      reached(true)
      if (!res.ok) { toast((res.body && res.body.error) || 'That could not be sent.', true); return }
      input.value = ''
      promptChanged()
      closeCommands()
      // Whoever scrolled up to read and forgot would see nothing happen: the reply
      // would arrive below the fold. Being at the end also makes the renders that follow stick.
      const log = $('convo-log')
      log.scrollTop = log.scrollHeight
      setConvoRunning(true)
      showTyping()
      pollConversation()
    } catch (e) {
      reached(false)
      toast('Could not reach vibe.', true)
    }
  }

  // --- packages, looked at when the user sends -----------------------------

  // An agent that builds downloads its own copy of a package the user's cache lacks, which the
  // user's IDE can't see; and one that has asked for a package waits on the user's word. So a
  // prompt is held, and the user asked, while either is so: in a dialog, so pressing Send is
  // never silently ignored. The host looks at the workspace at that moment, not at its last scan.
  // When the user's settings fetch packages without asking, they are fetched first without being
  // asked about.
  const gate = { active: false, fetching: false, prompt: '', failed: '' }

  async function checkPackages() {
    const sync = post('/api/packages/sync/check', {}).then((res) => (res.ok && res.body ? res.body : {})).catch(() => ({}))
    const requests = api('/api/packages').then((res) => (res.ok && res.body && res.body.requests) || []).catch(() => [])
    const [b, asked] = await Promise.all([sync, requests])
    return Object.assign({}, b, { undecided: asked.filter((r) => !r.approved).length })
  }

  async function holdForPackages(prompt) {
    const b = await checkPackages()
    if (!(b.actionable > 0) && !b.running && !(b.undecided > 0)) return false
    gate.active = true
    gate.prompt = prompt
    gate.failed = ''
    gate.fetching = !!b.running
    if (!gate.fetching && b.auto && b.actionable > 0) await startGateFetch()
    renderGate(gate.fetching && !b.running ? Object.assign({}, b, { running: true }) : b)
    return true
  }

  async function startGateFetch() {
    gate.failed = ''
    try {
      const res = await post('/api/packages/sync/run', {})
      reached(true)
      // A fetch already under way (409) is as good as one just begun.
      gate.fetching = res.ok || res.status === 409
      if (!gate.fetching) gate.failed = (res.body && res.body.error) || 'The fetch could not be started.'
    } catch (e) {
      reached(false)
      gate.failed = 'Could not reach vibe.'
    }
  }

  function renderGate(b) {
    const dialog = $('convo-gate')
    if (!gate.active) {
      if (dialog.open) dialog.close()
      return
    }
    const got = b && b.progress
    const toFetch = (b && b.actionable) || 0
    const undecided = (b && b.undecided) || 0
    const asking = undecided > 0
      ? undecided + (undecided === 1 ? ' package request from the agent is' : ' package requests from the agent are') +
        ' waiting for your approval: it can\'t have ' + (undecided === 1 ? 'that package' : 'those packages') + ' until you decide.'
      : ''
    $('convo-gate-progress').hidden = !gate.fetching
    showProgress($('convo-gate-progress'), got)
    const ide = packagesWord(toFetch) + " the agent will build with aren't in your package cache. Your IDE can't see them, and the agent will download its own copies."
    $('convo-gate-text').textContent = [
      gate.fetching
        ? 'Fetching' + (got && got.total > 0 ? ': ' + got.done + ' of ' + got.total + ' packages are in your cache' : ' the packages') + '. Your prompt goes when they are.'
        : gate.failed ? "Fetching didn't finish: " + gate.failed
        : toFetch > 0 ? ide + ' Fetch them first?' : '',
      asking,
    ].filter(Boolean).join(' ')
    $('convo-gate-fetch').hidden = gate.fetching || toFetch === 0
    if (!dialog.open) dialog.showModal()
    raiseToast()
  }

  function closeGate() {
    gate.active = false
    gate.fetching = false
    renderGate()
  }

  async function refreshGate() {
    if (!gate.active || !gate.fetching) return
    const b = await checkPackages()
    if (!gate.active) return
    if (b.running) { renderGate(b); return }
    if (!(b.actionable > 0)) {
      if (b.undecided > 0) {
        // They're in; but the agent still waits on the user's word for what it asked for.
        gate.fetching = false
        renderGate(b)
        return
      }
      // All in the cache: the prompt goes.
      const prompt = gate.prompt
      closeGate()
      await deliverPrompt(prompt)
      return
    }
    // The fetch is over and some are still missing.
    gate.fetching = false
    gate.failed = (b.last && b.last.error) || 'some packages are still missing.'
    renderGate(b)
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

  // --- searching the conversation ----------------------------------------

  // Matches are found in the text as it is rendered and coloured with CSS highlights, which change
  // nothing in the log: it is redrawn as the agent speaks, and a mark put in it would be lost or
  // would get in the way of copying. What is inside a collapsed tool result is left out unless
  // asked for, as it isn't in view.
  const find = { open: false, ranges: [], current: -1, queued: false, observer: null }

  const findSupported = () => !!(window.CSS && window.CSS.highlights && window.Highlight)

  // textOfLog is the log's text nodes that are in view (or, with tools, in a tool result too),
  // as { node, start } where start is where the node begins in the text joined from them: a
  // line break between one message and the next, so no match spans two.
  function textOfLog(withTools) {
    const log = $('convo-log')
    const walker = document.createTreeWalker(log, NodeFilter.SHOW_TEXT)
    const parts = []
    let text = ''
    let lastMessage = null
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const parent = node.parentElement
      if (!parent || parent.closest('[hidden]')) continue
      // What the agent did, in its groups, is out of sight until a group is opened: the box says whether to look.
      const inGroup = parent.closest('.tool-group-body')
      if (inGroup && !withTools) continue
      let message = node
      while (message.parentNode && message.parentNode !== log && !message.parentNode.classList.contains('tool-group-body')) message = message.parentNode
      if (lastMessage && message !== lastMessage) text += '\n'
      lastMessage = message
      parts.push({ node, start: text.length })
      text += node.data
    }
    return { text, parts }
  }

  function findRanges(query, withTools) {
    if (!query) return []
    const { text, parts } = textOfLog(withTools)
    const pattern = new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi')
    const at = (offset) => {
      let found = parts[0]
      for (const part of parts) {
        if (part.start > offset) break
        found = part
      }
      return found
    }
    const ranges = []
    for (let m = pattern.exec(text); m; m = pattern.exec(text)) {
      const from = at(m.index)
      const to = at(m.index + m[0].length - 1)
      const range = document.createRange()
      range.setStart(from.node, m.index - from.start)
      range.setEnd(to.node, m.index + m[0].length - to.start)
      ranges.push(range)
    }
    return ranges
  }

  // firstInView is the first match at or below the top of the log's view, as a browser's own
  // search starts from where it is looking; else the first.
  function firstInView(ranges) {
    const top = $('convo-log').getBoundingClientRect().top
    const i = ranges.findIndex((r) => r.startContainer.parentElement.getBoundingClientRect().top >= top)
    return i < 0 ? 0 : i
  }

  function paintFind() {
    if (!findSupported()) return
    const { ranges, current } = find
    if (ranges.length) {
      CSS.highlights.set('find-match', new Highlight(...ranges))
      CSS.highlights.set('find-current', new Highlight(ranges[current]))
    } else {
      CSS.highlights.delete('find-match')
      CSS.highlights.delete('find-current')
    }
  }

  function showFindCount(query) {
    const n = find.ranges.length
    $('convo-find-count').textContent = !query ? '' : n === 0 ? 'No matches' : (find.current + 1) + ' of ' + n
    $('convo-find-input').classList.toggle('nomatch', !!query && n === 0)
  }

  // runFind looks again at the log: scrolling to the match only when the user asked for one, and
  // keeping the match they were on when what is said afterwards adds to the others.
  function runFind(scroll) {
    if (!find.open) return
    const query = $('convo-find-input').value
    const had = find.current
    find.ranges = findRanges(query, $('convo-find-tools').checked)
    find.current = !find.ranges.length ? -1 : scroll ? firstInView(find.ranges) : Math.min(Math.max(had, 0), find.ranges.length - 1)
    paintFind()
    showFindCount(query)
    if (scroll && find.current >= 0) showMatch()
  }

  function showMatch() {
    const range = find.ranges[find.current]
    const parent = range.startContainer.parentElement
    for (let d = parent.closest('details'); d; d = d.parentElement && d.parentElement.closest('details')) d.open = true
    parent.scrollIntoView({ block: 'center' })
  }

  function stepFind(by) {
    if (!find.ranges.length) return
    find.current = (find.current + by + find.ranges.length) % find.ranges.length
    paintFind()
    showFindCount($('convo-find-input').value)
    showMatch()
  }

  function openFind() {
    find.open = true
    $('convo-find').hidden = false
    $('convo-find-open').hidden = true
    const input = $('convo-find-input')
    input.focus()
    input.select()
    runFind(true)
  }

  function closeFind() {
    find.open = false
    find.ranges = []
    find.current = -1
    $('convo-find').hidden = true
    $('convo-find-open').hidden = false
    paintFind()
    $('convo-input').focus()
  }

  // The log changes as the agent speaks: the matches are found again, once for all the changes
  // that arrive together.
  function findAgainSoon() {
    if (!find.open || find.queued) return
    find.queued = true
    queueMicrotask(() => { find.queued = false; runFind(false) })
  }

  function setUpFind() {
    $('convo-find-open').addEventListener('click', openFind)
    $('convo-find-close').addEventListener('click', closeFind)
    $('convo-find-next').addEventListener('click', () => stepFind(1))
    $('convo-find-prev').addEventListener('click', () => stepFind(-1))
    $('convo-find-input').addEventListener('input', () => runFind(true))
    $('convo-find-tools').addEventListener('change', () => runFind(true))
    $('convo-find-input').addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter') { ev.preventDefault(); stepFind(ev.shiftKey ? -1 : 1) }
      else if (ev.key === 'Escape') { ev.preventDefault(); ev.stopPropagation(); closeFind() }
    })
    new MutationObserver(findAgainSoon).observe($('convo-log'), { childList: true, subtree: true, characterData: true })
    // F3 is the browser's find-next: taken here, on the conversation, for a search that knows the
    // conversation (and what is collapsed in it); left alone on every other tab.
    document.addEventListener('keydown', (ev) => {
      if (ev.key !== 'F3' || ev.ctrlKey || ev.metaKey || ev.altKey) return
      if (!state.conversation || state.tab !== 'conversation') return
      ev.preventDefault()
      if (!find.open) openFind()
      else if (document.activeElement === $('convo-find-input')) stepFind(ev.shiftKey ? -1 : 1)
      else { $('convo-find-input').focus(); $('convo-find-input').select() }
    })
  }
  setUpFind()

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
    promptChanged()
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

  // refocusPrompt puts the cursor back in the prompt when the page is come
  // back to with the conversation showing, so typing goes where it's
  // meant: focus is easily left elsewhere, in the clipboard beside it after
  // a paste, say. It leaves alone another box being typed in, a dialog or
  // the frosted glass, and text selected for copying.
  function refocusPrompt() {
    if (state.tab !== 'conversation' || document.visibilityState !== 'visible') return
    if (document.querySelector('dialog[open]') || !$('offline').hidden) return
    const active = document.activeElement
    if (active === $('convo-input') || (editable(active) && active.tagName !== 'IFRAME')) return
    const selection = window.getSelection && window.getSelection()
    if (selection && !selection.isCollapsed) return
    $('convo-input').focus()
  }
  window.addEventListener('focus', refocusPrompt)
  document.addEventListener('visibilitychange', refocusPrompt)

  // promptCanTake says whether a character typed with focus elsewhere on
  // the conversation tab is meant for the prompt.
  function promptCanTake(ev) {
    if (state.tab !== 'conversation' || ev.isComposing || ev.ctrlKey || ev.metaKey || ev.altKey || ev.key.length !== 1) return false
    if (document.querySelector('dialog[open]') || !$('offline').hidden) return false
    const active = document.activeElement
    if (active === $('convo-input') || typingInto(active)) return false
    // Space presses a focused button or ticks a box, as it should.
    return !(ev.key === ' ' && active && (active.tagName === 'BUTTON' || active.tagName === 'INPUT'))
  }

  // typingInto says whether node is something typed into: a checkbox isn't.
  function typingInto(node) {
    if (!node) return false
    if (node.isContentEditable || node.tagName === 'TEXTAREA' || node.tagName === 'SELECT') return true
    return node.tagName === 'INPUT' && !['checkbox', 'radio', 'button', 'submit', 'reset', 'file', 'range', 'color'].includes(node.type)
  }

  // Typing goes to the prompt, wherever focus was left on the tab: moving
  // focus there as the key goes down lets the character land in it.
  document.addEventListener('keydown', (ev) => {
    if (promptCanTake(ev)) $('convo-input').focus()
  }, true)

  // The clipboard beside the conversation passes on what's typed in it
  // (outside its own boxes): only from that frame, at the clipboard's
  // origin, is it taken.
  window.addEventListener('message', (ev) => {
    const frame = $('convo-clipboard-frame')
    const d = ev.data
    if (!frame || ev.source !== frame.contentWindow || !d || d.type !== 'vibe-clipboard-key') return
    let origin
    try { origin = new URL(state.clipboardUrl).origin } catch (e) { return }
    if (ev.origin !== origin || typeof d.key !== 'string' || d.key.length !== 1) return
    if (!promptCanTake({ key: d.key })) return
    const input = $('convo-input')
    input.focus()
    input.setRangeText(d.key, input.selectionStart, input.selectionEnd, 'end')
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })

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

  // --- the model and effort ----------------------------------------------

  // The drop-downs beside Send choose the model and effort the agent is asked
  // to use (see docs/plan-model-effort.md). What they show is what vibe keeps,
  // which the agent is asked for on each turn; the lists are the agent's own.
  // A change applies from the next prompt.
  //
  // What is good to know goes in a bubble over the "i" button, which takes no
  // room in the page so nothing moves when it comes and goes: that a change
  // waits for the next prompt (which can be asked not to show again, in this
  // browser, the button still telling there's something to know), that the
  // agent ran another model than was asked for (always shown), and why the
  // lists couldn't be had. What failed to be kept is a dialog that must be
  // answered.
  const OTHER_MODEL = '__other__'
  const CHOICE_QUIET = 'vibe-companion-choice-quiet'
  // How long the drop-downs stay disabled for the agent's lists when none are
  // known, before they're usable anyway (the lists carry on arriving).
  const CHOICE_LOCK_MS = 30000
  const choice = { model: '', effort: '', running: '', session: false, options: null, optionsAt: 0, optionsError: '', problem: '', pending: false, bubble: false, unseen: false, started: false, pendingTurns: 0, inherited: { model: false, effort: false },
    goneModel: '', goneEffort: '', goneLabel: '', refreshing: false, adopting: false, adoptWait: false,
    loaded: false, asking: false, saving: false, choosing: false, changes: 0, adoptSkipped: false, lockExpired: false, slow: false, lockTimer: 0 }

  // dialogAnswer shows a modal dialog and gives what it was closed with, ''
  // when it was dismissed (Escape).
  function dialogAnswer(dialog) {
    return new Promise((resolve) => {
      dialog.returnValue = ''
      dialog.addEventListener('close', () => resolve(dialog.returnValue), { once: true })
      dialog.showModal()
      raiseToast()
    })
  }

  // ranAsAsked says what the agent reported running fits what was asked for:
  // an alias ("opus") is part of the full name it stands for. A name the lists
  // say picks by a rule ("best") is no one model, so what ran can't be held
  // against it.
  function ranAsAsked(asked, ran) {
    const rules = (choice.options && choice.options.rules) || []
    if (!asked || !ran || rules.includes(asked)) return true
    return ran.toLowerCase().includes(asked.replace(/\[[^\]]*\]$/, '').toLowerCase())
  }

  // choiceOdd says the agent ran another model than was asked for. Not while
  // a change waits for its first turn: what ran then is what was asked before.
  function choiceOdd() { return !choice.pending && !ranAsAsked(choice.model, choice.running) }

  function choiceQuiet() { return store.get(CHOICE_QUIET) === 'yes' }

  // choiceWaiting says vibe's answer is awaited: what's chosen (not yet read)
  // or the agent's lists (being asked for).
  function choiceWaiting() { return !choice.loaded || choice.asking }

  // choiceLocked says there's nothing to choose from yet, so the widget is
  // disabled: not while kept lists are being asked for again, which are
  // usable, and not past the limit.
  function choiceLocked() {
    return !choice.lockExpired && (!choice.loaded || (choice.asking && !choice.options))
  }

  function startChoiceLock() {
    clearTimeout(choice.lockTimer)
    choice.lockExpired = false
    choice.slow = false
    choice.lockTimer = setTimeout(choiceLockExpired, CHOICE_LOCK_MS)
  }

  function endChoiceLock() {
    clearTimeout(choice.lockTimer)
    const wasSlow = choice.slow
    choice.lockExpired = false
    choice.slow = false
    if (wasSlow) choice.bubble = false
  }

  // choiceLockExpired lets the drop-downs be used though the lists haven't
  // come, and says what's going on.
  function choiceLockExpired() {
    if (!choiceWaiting() || !choiceLocked()) return
    choice.lockExpired = true
    choice.slow = true
    choice.unseen = true
    setChoiceBubble(true)
  }

  // choiceProblem is why there are no lists to choose from, or what was chosen
  // couldn't be read: "" when there's nothing wrong, or when the lists are
  // only out of date (see choiceStale).
  function choiceProblem() {
    return choice.problem || (choice.options ? '' : choice.optionsError)
  }

  // choiceStale says the lists are from an earlier run, and asking the agent
  // again failed: they're used, and the bubble says how old they are.
  function choiceStale() {
    return choice.options && choice.optionsError
      ? 'The lists are from ' + new Date(choice.optionsAt).toLocaleString() + "; they couldn't be refreshed: " + choice.optionsError
      : ''
  }

  // choiceBubble is what the bubble says now, and of what kind: "problem"
  // (the lists, or what was chosen, couldn't be had), "odd" (ran something
  // else), or news that a change waits.
  function choiceBubble() {
    const problem = choiceProblem()
    if (problem) return { kind: 'problem', text: problem }
    const gone = choiceGone()
    if (gone) return { kind: 'gone', text: gone }
    if (choice.slow && choiceWaiting()) return { kind: 'slow', text: "Still asking the agent which models and efforts it has: it's taking longer than usual. You can carry on, and the lists fill in when they arrive." }
    if (choiceOdd()) return { kind: 'odd', text: 'You asked for ' + choice.model + ', but the agent says it ran ' + choice.running + '.' }
    // What comes from the global settings: a model can be overridden by choosing another here; an
    // effort has a Default that can't take it away.
    const from = []
    if (choice.inherited.model) from.push(' The model comes from your global settings.yaml; choosing one here overrides it for this profile.')
    if (choice.inherited.effort) from.push(" The effort comes from your global settings.yaml, so Default here can't take it away.")
    return { kind: 'news', text: 'A change to the model or effort applies to your next prompt.' + (choice.running ? ' The last turn ran on ' + choice.running + '.' : '') + from.join('') }
  }

  // modelName is what the agent calls the model an alias stands for ("Opus
  // 5.5" for "opus"), when it said.
  function modelName(alias) {
    const names = choice.options && choice.options.names
    return (names && names[alias]) || ''
  }

  // titleCase puts a capital on each word of a short lower-case name the agent
  // hands out ("opus" is "Opus", "opus[1m]" is "Opus[1M]"), for showing: what's
  // kept and sent is the name as it was. A full model id ("claude-opus-4-1")
  // is exact, so it's left as it is.
  function titleCase(text) {
    if (!/^[a-z]+(\[[a-z0-9]+\])?$/.test(text)) return text
    return text.replace(/(^|[^A-Za-z])([a-z])/g, (all, before, letter) => before + letter.toUpperCase())
  }

  // effortLabel is how an effort is shown: in title case, and with the word
  // "effort", since the drop-down has no label of its own and "High" alone
  // wouldn't say what it is. "xhigh" runs two words together, so it's the one
  // that's spelt out.
  function effortLabel(effort) {
    return (effort === 'xhigh' ? 'Extra High' : titleCase(effort)) + ' effort'
  }

  // modelLabel is how a model is shown: by the name the agent gives it where
  // it's one of the plain ones ("Opus 5.5", as the agent's own picker has it);
  // elsewhere with the alias it is kept as after the name, so what's chosen is
  // plain. An alias the agent gave no name for is shown as itself, in title case.
  function modelLabel(alias, plain) {
    const name = modelName(alias)
    return !name ? titleCase(alias) : plain ? name : name + ' · ' + alias
  }

  // fillSelect makes select hold items with value chosen. Its options are only
  // rebuilt when they differ: a drop-down that is open is closed by a rebuild,
  // and a redraw for some other reason (the spinner, a bubble) shouldn't do that.
  function fillSelect(select, items, value) {
    const same = select.options.length === items.length &&
      items.every((item, i) => select.options[i].value === item.value && select.options[i].textContent === item.text)
    // A drop-down in use is left as it is: what changed is taken in once it is let go (blur).
    if (!same && select === document.activeElement) return
    if (!same) select.replaceChildren(...items.map((i) => el('option', { value: i.value, text: i.text })))
    if (select.value !== value) select.value = value
  }

  function drawChoice() {
    const o = choice.options
    const current = o && o.current ? o.current.replace(/\s*\(default\)\s*$/i, '') : ''
    // The user is on a model, and "Default" is no choice (a change of claude's default must
    // not move them): so the list is the models, with the word "(default)" on the one claude
    // defaults to. Only while none is in use (nothing to say which it is yet) there's a Default entry.
    const def = (o && o.defaultAlias) || ''
    const defaultMark = (m) => (m === def ? ' (default)' : '')
    const models = choice.model ? [] : [{ value: '', text: current ? 'Default (' + current + ')' : 'Default' }]
    for (const m of (o && o.models) || []) models.push({ value: m, text: modelLabel(m, true) + defaultMark(m) })
    if (choice.model && !models.some((m) => m.value === choice.model)) models.push({ value: choice.model, text: modelLabel(choice.model, false) + defaultMark(choice.model) })
    models.push({ value: OTHER_MODEL, text: 'Other…' })
    fillSelect($('choice-model'), models, choice.model)
    const efforts = [{ value: '', text: 'Default effort' }]
    for (const e of (o && o.efforts) || []) efforts.push({ value: e, text: effortLabel(e) })
    if (choice.effort && !efforts.some((e) => e.value === choice.effort)) efforts.push({ value: choice.effort, text: effortLabel(choice.effort) })
    fillSelect($('choice-effort'), efforts, choice.effort)

    // Said beside the drop-downs only when it's news: that the models are on
    // their way, or that the agent ran another than was asked for. That it ran
    // what was asked is what's expected.
    const odd = choiceOdd()
    const locked = choiceLocked()
    const waiting = choiceWaiting()
    const ran = $('choice-ran')
    ran.classList.toggle('warn-text', odd && !locked)
    ran.textContent = locked ? 'Loading models…' : odd ? 'ran ' + choice.running : ''
    ran.title = ran.textContent
    $('choice-spinner').hidden = !waiting
    $('choice-busy').textContent = !waiting ? '' : locked ? 'Loading the models and efforts…' : 'Checking for new models…'
    $('convo-choice').classList.toggle('loading', locked)
    $('convo-choice').setAttribute('aria-busy', String(locked))
    $('choice-model').disabled = $('choice-effort').disabled = locked || choice.saving

    const said = choiceBubble()
    const problem = said.kind === 'problem'
    const info = $('choice-info')
    const stale = choiceStale()
    const gone = said.kind === 'gone'
    info.classList.toggle('warn', problem || odd || gone)
    // The dot says there's something to know that hasn't been looked at:
    // closing the bubble, whichever way, is looking.
    info.classList.toggle('news', choice.unseen && (choice.pending || !!stale || odd || problem || gone || choice.slow))
    info.setAttribute('aria-expanded', String(choice.bubble))
    $('choice-bubble').hidden = !choice.bubble
    $('choice-bubble-text').textContent = said.text
    $('choice-bubble-stale').textContent = stale
    $('choice-bubble-stale').hidden = !stale
    $('choice-bubble-retry').hidden = !problem && !stale
    // Only what can be told to stop is: not that the agent ran another model.
    $('choice-quiet-row').hidden = said.kind !== 'news'
    $('choice-quiet').checked = choiceQuiet()
  }

  function setChoiceBubble(on) {
    if (!on && choice.bubble) choice.unseen = false
    choice.bubble = on
    drawChoice()
  }

  // choiceFailed tells the user what couldn't be kept, in a dialog that has
  // to be answered: the drop-downs go back to what vibe has behind it.
  async function choiceFailed(text) {
    drawChoice()
    $('choice-error-text').textContent = text
    await dialogAnswer($('choice-error'))
  }

  function takeChoice(body) {
    choice.model = body.model || ''
    choice.effort = body.effort || ''
    choice.running = body.running || ''
    choice.session = !!body.session
    choice.inherited = { model: !!(body.inherited && body.inherited.model), effort: !!(body.inherited && body.inherited.effort) }
    if (body.options) adoptOptions(body.options, body.optionsAt || 0)
  }

  // maybeAdopt asks the host to record the model the conversation is on, when
  // none is chosen or inherited and the lists are known (the kept ones will do):
  // the user is on a model, and a change of claude's default must not move them
  // to another. Silent, as nothing has changed; the host says when there is
  // nothing to go by, and then it isn't asked again until there are new lists.
  async function maybeAdopt() {
    if (!state.choice || choice.model || choice.inherited.model || !choice.options || choice.adopting || choice.adoptWait) return
    // Not over a choice the user is making or saving: it is tried again once
    // they are done (see settleChoice).
    if (choice.saving || choice.choosing) { choice.adoptSkipped = true; return }
    choice.adopting = true
    const changes = choice.changes
    try {
      const res = await post('/api/conversation/choice/adopt', {})
      if (res.ok) {
        reached(true)
        // What the host says was true before a choice the user made meanwhile.
        if (changes !== choice.changes) return
        takeChoice(res.body)
        if (!res.body.adopted) choice.adoptWait = true
        drawChoice()
      }
    } catch (e) {
      // The host logs why; it is asked again with the next lists.
    } finally {
      choice.adopting = false
    }
  }

  // settleChoice is where a choice the user was making ends, so that a model
  // to adopt that was held off meanwhile is looked at again.
  function settleChoice() {
    if (choice.saving || choice.choosing || !choice.adoptSkipped) return
    choice.adoptSkipped = false
    maybeAdopt()
  }

  // modelListed says the lists offer model, among the plain ones or the rest.
  function modelListed(opts, model) {
    return !!opts && ((opts.models || []).includes(model) || (opts.otherModels || []).includes(model))
  }

  // adoptOptions takes the lists the agent gave. A new default, or a model
  // added, is simply taken in. What is in use that the lists offered and now
  // don't is said, once (see choiceGone): something chosen from beyond the
  // lists (a full name typed under Other…) was never offered, so isn't news.
  function adoptOptions(next, at) {
    const before = choice.options
    if (before !== next && JSON.stringify(before) !== JSON.stringify(next)) choice.adoptWait = false
    choice.options = next
    choice.optionsAt = at
    if (!before) return
    if (choice.model && modelListed(before, choice.model) && !modelListed(next, choice.model) && choice.goneModel !== choice.model) {
      choice.goneModel = choice.model
      choice.goneLabel = (before.names && before.names[choice.model]) || titleCase(choice.model)
      choice.unseen = true
      choice.bubble = true
    }
    if (choice.effort && (before.efforts || []).includes(choice.effort) && !(next.efforts || []).includes(choice.effort) && choice.goneEffort !== choice.effort) {
      choice.goneEffort = choice.effort
      choice.unseen = true
      choice.bubble = true
    }
  }

  // choiceGone is what to say when what is chosen isn't offered by the agent
  // any more, or "" when it is (or never was, as a typed name wasn't).
  function choiceGone() {
    const gone = []
    if (choice.goneModel && choice.goneModel === choice.model && !modelListed(choice.options, choice.model)) gone.push(choice.goneLabel)
    if (choice.goneEffort && choice.goneEffort === choice.effort && !((choice.options && choice.options.efforts) || []).includes(choice.effort)) gone.push(effortLabel(choice.effort))
    if (!gone.length) return ''
    return gone.map((g) => '"' + g + '"').join(' and ') + " isn't offered by the agent any more, and is what's chosen: your next prompt will probably fail. Choose another" +
      (gone.length > 1 ? ' model and effort.' : choice.goneModel === choice.model ? ' model.' : ' effort.')
  }

  async function readChoice() {
    try {
      const res = await api('/api/conversation/choice')
      reached(true)
      if (res.ok) { takeChoice(res.body); choice.problem = '' } else choice.problem = res.body.error || "vibe couldn't say what model and effort are chosen."
    } catch (e) {
      reached(false)
      choice.problem = 'Could not reach vibe to ask what model and effort are chosen.'
    }
    if (choice.problem) choice.unseen = true
    drawChoice()
    maybeAdopt()
  }

  // askChoiceOptions asks for the agent's lists. wake says the sandbox may be
  // woken to be asked: not when the page opens, only when the user retries.
  async function askChoiceOptions(wake) {
    choice.asking = true
    drawChoice()
    try {
      const res = await post('/api/conversation/choice/options', { wake }, { timeout: 3 * 60 * 1000 })
      reached(true)
      if (res.ok) { adoptOptions(res.body.options, res.body.optionsAt || 0); choice.optionsError = '' } else choice.optionsError = res.body.error || "the agent couldn't say which models and efforts it has."
    } catch (e) {
      reached(false)
      choice.optionsError = 'Could not reach vibe to ask the agent which models and efforts it has.'
    }
    if (choice.optionsError) choice.unseen = true
    choice.asking = false
    endChoiceLock()
    drawChoice()
    maybeAdopt()
  }

  // Nothing the user did has failed when the page opens, so a problem then is
  // said in the bubble, opened, and not in a dialog in the way.
  async function initChoice() {
    if (choice.started) return
    choice.started = true
    startChoiceLock()
    drawChoice()
    await readChoice()
    choice.loaded = true
    await askChoiceOptions(false)
    if (choiceProblem()) setChoiceBubble(true)
  }

  async function retryChoice() {
    startChoiceLock()
    if (choice.problem) await readChoice()
    await askChoiceOptions(true)
    if (!choiceProblem() && !choice.optionsError) setChoiceBubble(false)
  }

  // choiceTurnEnded: what the agent ran is known now, and what was chosen has
  // been used, so what was said of it is out of date. Another model than
  // asked for is said at the end of every turn it happens.
  async function choiceTurnEnded() {
    // A change made while this turn ran is for the next one, so this one
    // ending is no word on it yet.
    if (choice.pendingTurns > 0) choice.pendingTurns--
    choice.pending = choice.pendingTurns > 0
    if (!choice.pending && !choiceProblem() && !choice.optionsError) { choice.bubble = false; choice.unseen = false }
    await readChoice()
    if (choiceOdd()) { choice.unseen = true; setChoiceBubble(true) }
    refreshChoiceQuietly()
  }

  // refreshChoiceQuietly asks the host for the agent's lists again, as the
  // session goes on: the host asks the agent only when its last answer is
  // old (ten minutes), so this is cheap. Nothing of it shows (no spinner, no
  // lock, no bubble): a new default or a new model is simply taken in, and a
  // failure is the host's to say in its log. The one thing said is what is
  // in use no longer being offered (see adoptOptions). Not while the first
  // ask is under way, nor on top of another refresh.
  async function refreshChoiceQuietly() {
    if (!state.choice || !choice.loaded || choice.asking || choice.refreshing) return
    choice.refreshing = true
    try {
      const res = await post('/api/conversation/choice/options', { wake: false }, { timeout: 3 * 60 * 1000 })
      if (res.ok) {
        reached(true)
        adoptOptions(res.body.options, res.body.optionsAt || 0)
        choice.optionsError = ''
        drawChoice()
        maybeAdopt()
      }
    } catch (e) {
      // The page's other polls say when vibe can't be reached.
    } finally {
      choice.refreshing = false
    }
  }

  // saveChoice asks vibe to keep model and effort, and shows what it kept: it
  // may have changed the effort, or refused.
  async function saveChoice(model, effort) {
    choice.saving = true
    choice.changes++
    drawChoice()
    let failed = ''
    try {
      const res = await post('/api/conversation/choice', { model, effort })
      reached(true)
      if (res.ok) { takeChoice(res.body); choice.problem = ''; choice.pending = true; choice.pendingTurns = convo.running ? 2 : 1 } else failed = res.body.error || "that choice couldn't be kept."
    } catch (e) {
      reached(false)
      failed = 'Could not reach vibe to keep that choice.'
    }
    choice.saving = false
    choice.changes++
    if (failed) { await choiceFailed(failed); settleChoice(); return }
    // Default asked for and not given, as the global settings choose: said whatever
    // the tick says, as it would be a puzzle otherwise.
    const refused = (model === '' && choice.inherited.model) || (effort === '' && choice.inherited.effort)
    // After the bubble is settled, for closing it counts as having looked.
    setChoiceBubble(refused || !choiceQuiet())
    choice.unseen = true
    drawChoice()
    settleChoice()
  }

  // pickOtherModel asks which of the models the agent lists beyond the
  // plain names, or which full name, and gives it; null for none.
  async function pickOtherModel() {
    const others = (choice.options && choice.options.otherModels) || []
    $('choice-other-intro').textContent = others.length
      ? 'The agent also lists these, besides the ones in the drop-down.'
      : "The agent lists no others, but it takes a model's full name."
    $('choice-other-list').replaceChildren(...others.map((m) => el('label', {},
      el('input', { type: 'radio', name: 'choice-other', value: m }), ' ', modelLabel(m, false) + (choice.options && m === choice.options.defaultAlias ? ' (default)' : ''))))
    $('choice-other-name').value = ''
    if ((await dialogAnswer($('choice-other'))) !== 'go') return null
    const typed = $('choice-other-name').value.trim()
    const picked = $('choice-other-list').querySelector('input:checked')
    return typed || (picked ? picked.value : null)
  }

  async function changeModel(model) {
    if (model === choice.model) { drawChoice(); return }
    // Only a conversation under way has anything to re-read.
    if (choice.session) {
      $('choice-warn-title').textContent = 'Change the model to ' + (model ? modelName(model) || titleCase(model) : 'the default') + '?'
      if ((await dialogAnswer($('choice-warn'))) !== 'go') { drawChoice(); return }
    }
    await saveChoice(model, choice.effort)
  }

  async function modelPicked() {
    choice.choosing = true
    try {
      const value = $('choice-model').value
      if (value !== OTHER_MODEL) { await changeModel(value); return }
      const other = await pickOtherModel()
      if (other == null) { drawChoice(); return }
      await changeModel(other)
    } finally {
      choice.choosing = false
      settleChoice()
    }
  }

  // --- "New conversation" -----------------------------------------------

  // The dialog offers only what applies (see docs/plan-new-conversation.md).
  // What the sandbox can do is asked when it opens, which may wake the
  // sandbox; origin is checked once a new branch is ticked, once per opening.
  // generation tells an answer for an earlier opening, which is dropped.
  const FRESH_BRANCH_START = 'feat/'
  // Backstops, past vibe's own: waking a sandbox takes up to 3 minutes,
  // a fetch up to 2.
  const FRESH_OPTIONS_MS = 4 * 60 * 1000
  const FRESH_FETCH_MS = 3 * 60 * 1000
  const FRESH_START_MS = 5 * 60 * 1000
  const fresh = { generation: 0, options: null, fetching: false, fetched: null, cancel: null, nameTyped: false }

  // branchNameProblem is why git would refuse name for a branch (git check-ref-format's rules, as far
  // as they matter to a name typed by hand), or "" when it wouldn't. Starting asks git itself: this is
  // for saying so while it's typed.
  function branchNameProblem(name) {
    if (name.startsWith('-')) return "it can't start with -"
    if (/[\s\x00-\x1f\x7f]/.test(name)) return "it can't have spaces or control characters"
    const bad = name.match(/[~^:?*[\\]/)
    if (bad) return "it can't have " + bad[0]
    if (name.includes('..')) return "it can't have .."
    if (name.includes('@{')) return "it can't have @{"
    if (name === '@') return "it can't be just @"
    if (name.endsWith('.')) return "it can't end with ."
    const parts = name.split('/')
    if (parts.some((part) => part === '')) return "it can't start or end with /, nor have // in it"
    if (parts.some((part) => part.startsWith('.'))) return "no part of it can start with ."
    if (parts.some((part) => part.endsWith('.lock'))) return "no part of it can end with .lock"
    return ''
  }

  // drawBranchName says, under the new branch's name, what's wrong with it: an error for a name git
  // would refuse or that is a branch here; a warning for one origin has, as a push of the new branch
  // under that name would be refused. Nothing until the user has typed.
  function drawBranchName(b) {
    const input = $('fresh-branch-name')
    const note = $('fresh-branch-note')
    const name = input.value
    let text = ''
    let error = false
    if (fresh.nameTyped && name) {
      const problem = branchNameProblem(name)
      if (problem) {
        text = '"' + name + "\" can't be a branch name: " + problem + '.'
        error = true
      } else if ((b.branches || []).includes(name)) {
        text = 'There is already a branch called ' + name + '.'
        error = true
      } else if ((b.remote || []).includes(name)) {
        text = 'origin has a branch called ' + name + ", as last fetched. A push of this one under that name would be refused unless it's forced, so you may want a different name."
      }
    }
    input.setAttribute('aria-invalid', String(error))
    note.textContent = text
    note.className = 'note ' + (error ? 'bad-text' : 'warn-text')
    note.hidden = !text
  }

  function showIf(id, on) { $(id).hidden = !on }

  // plural is n with the word for one, or for more (one + 's' unless given).
  function plural(n, one, more) { return n + ' ' + (n === 1 ? one : more || one + 's') }

  function openFresh() {
    if (fresh.cancel) fresh.cancel.abort()
    const generation = ++fresh.generation
    Object.assign(fresh, { options: null, fetching: false, fetched: null, setterWhy: null, cancel: new AbortController() })
    $('fresh-clipboard').checked = true
    $('fresh-unsent').checked = true
    $('fresh-review').checked = true
    $('fresh-branch').checked = false
    $('fresh-branch-name').value = FRESH_BRANCH_START
    fresh.nameTyped = false
    $('fresh-from-here').checked = true
    $('fresh-update').checked = false
    $('fresh-run').textContent = 'Start a new conversation'
    showIf('fresh-error', false)
    drawFresh()
    $('fresh').showModal()
    raiseToast()
    askFreshOptions(false)
  }

  // askFreshOptions asks vibe what else the dialog can offer, and draws it,
  // unless the dialog has been opened afresh meanwhile. Failing, it says
  // why, and keep says whether what was offered before stays offered.
  function askFreshOptions(keep) {
    const generation = fresh.generation
    const failed = (text) => {
      if (generation !== fresh.generation) return
      freshError(text)
      if (!keep) { fresh.options = {}; drawFresh() }
    }
    post('/api/conversation/new/options', {}, { timeout: FRESH_OPTIONS_MS, cancel: fresh.cancel }).then((res) => {
      if (!res.ok) { failed(res.body.error || "vibe couldn't say what else can be done."); return }
      if (generation !== fresh.generation) return
      fresh.options = res.body
      drawFresh()
    }, () => failed('Could not reach vibe to ask what else can be done.'))
  }

  function freshError(text) {
    $('fresh-error').textContent = text
    showIf('fresh-error', true)
  }

  // freshDefault is the default branch the dialog offers to branch from, or
  // '' when it doesn't.
  function freshDefault() {
    const b = fresh.options && fresh.options.branch
    return b && !b.unavailable && b.default && b.current !== b.default ? b.default : ''
  }

  function drawFresh() {
    const o = fresh.options
    showIf('fresh-loading', !o)
    $('fresh-run').disabled = !o
    const c = o && o.clipboard
    showIf('fresh-clipboard-row', !!c && (!!c.unavailable || c.sent + c.unsent > 0))
    if (c) {
      $('fresh-clipboard').disabled = !!c.unavailable
      if (c.unavailable) $('fresh-clipboard').checked = false
      $('fresh-clipboard-why').textContent = c.unavailable || ''
      showIf('fresh-clipboard-why', !!c.unavailable)
      $('fresh-clipboard-count').textContent = !c.unavailable && c.sent ? '(' + plural(c.sent, 'item') + ' sent)' : ''
      showIf('fresh-unsent-row', !c.unavailable && c.unsent > 0 && $('fresh-clipboard').checked)
      $('fresh-unsent-count').textContent = '(' + c.unsent + ' waiting to go with your next message)'
    }
    showIf('fresh-review-row', !!(o && o.review))
    const b = o && o.branch
    showIf('fresh-branch-row', !!b)
    if (!b) return
    $('fresh-branch').disabled = !!b.unavailable
    if (b.unavailable) $('fresh-branch').checked = false
    $('fresh-branch-why').textContent = b.unavailable || ''
    showIf('fresh-branch-why', !!b.unavailable)
    showIf('fresh-branch-details', $('fresh-branch').checked)
    $('fresh-from-here-name').textContent = b.current ? '(' + b.current + ')' : '(the detached HEAD)'
    const def = freshDefault()
    showIf('fresh-from-default-row', !!def)
    // A default branch set for the repository, origin not saying, is
    // branched from as it is here.
    $('fresh-default-name').textContent = b.defaultSet ? def + ' (local)' : 'origin/' + def
    $('fresh-from-default').disabled = !!b.dirty || !!b.defaultEmpty
    if ($('fresh-from-default').disabled) $('fresh-from-here').checked = true
    $('fresh-from-default-why').textContent = b.dirty
      ? 'The current branch has uncommitted changes.'
      : b.defaultEmpty
        ? def + ' has no commits yet.'
        : 'Switching to its files changes them under anything else using this folder: your editor, or an agent in the terminal.'
    const noDefault = !b.default && b.noDefault ? b.noDefault : ''
    showIf('fresh-no-default', !!noDefault)
    if (noDefault && fresh.setterWhy !== noDefault) $('fresh-no-default').replaceChildren(defaultBranchSetter(noDefault, refreshFresh))
    fresh.setterWhy = noDefault
    showIf('fresh-dirty', !!b.dirty)
    drawBranchName(b)
    drawFreshFetch(b.defaultSet ? '' : def)
  }

  // refreshFresh asks again what the dialog can offer, keeping what's
  // chosen: after the default branch has been set.
  function refreshFresh() {
    askFreshOptions(true)
  }

  function drawFreshFetch(def) {
    const f = fresh.fetched
    let text = ''
    let warn = false
    let behind = 0
    if (def && fresh.fetching) {
      text = 'Checking whether origin/' + def + ' is up to date…'
    } else if (def && f && !f.checked) {
      text = "Couldn't check whether origin/" + def + ' is up to date: ' + String(f.why || 'no reason given').replace(/\.$/, '') + '.'
      warn = true
    } else if (def && f && f.local && f.ahead > 0) {
      text = 'Local ' + def + ' has ' + plural(f.ahead, 'commit') + " not on origin, so it's left as it is."
    } else if (def && f && f.local) {
      behind = f.behind
    }
    $('fresh-fetch').textContent = text
    $('fresh-fetch').className = 'note' + (warn ? ' warn-text' : ' muted')
    showIf('fresh-fetch', !!text)
    showIf('fresh-update-row', behind > 0)
    $('fresh-update-text').textContent = behind > 0 ? 'Local ' + def + ' is ' + plural(behind, 'commit') + ' behind origin: update it too?' : ''
  }

  function fetchOrigin() {
    if (!freshDefault() || fresh.options.branch.defaultSet || fresh.fetching || fresh.fetched) return
    const generation = fresh.generation
    fresh.fetching = true
    const answered = (fetched) => {
      if (generation !== fresh.generation) return
      fresh.fetching = false
      fresh.fetched = fetched
      // A fast-forward loses nothing: it's offered ticked.
      $('fresh-update').checked = !!(fetched.checked && fetched.local && !fetched.ahead && fetched.behind > 0)
      drawFresh()
    }
    post('/api/conversation/new/fetch', {}, { timeout: FRESH_FETCH_MS, cancel: fresh.cancel }).then(
      (res) => answered(res.ok ? res.body : { checked: false, why: res.body.error || 'vibe said ' + res.status }),
      () => answered({ checked: false, why: "vibe couldn't be reached" }))
  }

  function freshRequest() {
    const o = fresh.options
    const body = { clipboard: '', review: false, branch: null }
    if (!$('fresh-clipboard-row').hidden && !$('fresh-clipboard').disabled && $('fresh-clipboard').checked) {
      body.clipboard = !$('fresh-unsent-row').hidden && $('fresh-unsent').checked ? 'all' : 'sent'
    }
    if (o.review && $('fresh-review').checked) body.review = true
    if (o.branch && !o.branch.unavailable && $('fresh-branch').checked) {
      const fromDefault = !!freshDefault() && !$('fresh-from-default').disabled && $('fresh-from-default').checked
      body.branch = {
        name: $('fresh-branch-name').value,
        from: fromDefault ? 'default' : 'here',
        update: !$('fresh-update-row').hidden && $('fresh-update').checked,
      }
    }
    return body
  }

  async function startFresh() {
    if (!fresh.options || $('fresh-run').disabled) return
    const body = freshRequest()
    if (body.branch && !body.branch.name.trim()) {
      freshError('Name the new branch.')
      $('fresh-branch-name').focus()
      return
    }
    showIf('fresh-error', false)
    $('fresh-run').disabled = true
    $('fresh-run').textContent = 'Starting…'
    let res
    try {
      res = await post('/api/conversation/new', body, { timeout: FRESH_START_MS })
      reached(true)
    } catch (e) {
      reached(false)
      res = { ok: false, body: { error: 'Could not reach vibe.' } }
    }
    $('fresh-run').textContent = 'Start a new conversation'
    $('fresh-run').disabled = false
    if (!res.ok) {
      freshError(res.body.error || 'That failed.')
      return
    }
    $('fresh').close('done')
    toast('Started a new conversation.', false)
    tick()
  }

  function setUpFresh() {
    $('convo-new').addEventListener('click', openFresh)
    $('choice-model').addEventListener('change', modelPicked)
    $('choice-effort').addEventListener('change', () => saveChoice(choice.model, $('choice-effort').value))
    $('choice-model').addEventListener('blur', drawChoice)
    $('choice-effort').addEventListener('blur', drawChoice)
    $('choice-info').addEventListener('click', () => setChoiceBubble(!choice.bubble))
    $('choice-bubble-close').addEventListener('click', () => { setChoiceBubble(false); $('choice-info').focus() })
    $('choice-bubble-retry').addEventListener('click', retryChoice)
    $('choice-quiet').addEventListener('change', () => { store.set(CHOICE_QUIET, $('choice-quiet').checked ? 'yes' : ''); drawChoice() })
    // Escape is the agent's Stop in the prompt box, so it only closes the
    // bubble from inside the choice.
    $('convo-choice').addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && choice.bubble) { e.stopPropagation(); setChoiceBubble(false); $('choice-info').focus() }
    })
    document.addEventListener('click', (e) => {
      if (choice.bubble && !$('convo-choice').contains(e.target)) setChoiceBubble(false)
    })
    $('fresh-form').addEventListener('submit', (ev) => {
      ev.preventDefault()
      if (ev.submitter && ev.submitter.value === 'cancel') $('fresh').close('cancel')
      else startFresh()
    })
    // Enter in the name does nothing: more of the form follows it, to be
    // seen before starting, and the browser would press the form's first
    // button, which is Cancel.
    $('fresh-branch-name').addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.isComposing) ev.preventDefault()
    })
    $('fresh-branch-name').addEventListener('input', () => { fresh.nameTyped = true; drawFresh() })
    $('fresh').addEventListener('close', () => {
      if (fresh.cancel) fresh.cancel.abort()
      fresh.generation++
    })
    $('fresh-clipboard').addEventListener('change', drawFresh)
    $('fresh-branch').addEventListener('change', () => {
      drawFresh()
      if (!$('fresh-branch').checked) return
      const name = $('fresh-branch-name')
      name.focus()
      name.setSelectionRange(name.value.length, name.value.length)
      fetchOrigin()
      drawFresh()
    })
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
        promptChanged()
      }
    })
    $('convo-input').value = store.get(DRAFT) || ''
    $('convo-input').addEventListener('input', promptChanged)
    window.addEventListener('resize', fitPrompt)
    // A draft put back at load is fitted once the box can be measured: the
    // tab it's on may not be showing yet.
    $('convo-input').addEventListener('focus', fitPrompt)
    requestAnimationFrame(fitPrompt)
    $('convo-stop').addEventListener('click', () => conversationCall('/api/conversation/stop'))
    setUpFresh()
    $('settings-prompts-markdown').checked = promptsAsMarkdown.on
    $('settings-prompts-markdown').addEventListener('change', () => setPromptsAsMarkdown($('settings-prompts-markdown').checked))
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
  for (const [on, held] of [['mouseenter', true], ['mouseleave', false], ['focusin', true], ['focusout', false]]) {
    $('toast').addEventListener(on, () => holdToast(held))
  }
  setUpManage()

  // Out of sight, the next time in view asks to keep the sandbox awake at
  // once, rather than when the last ask is old enough.
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') awakeAsked = 0 })
  document.addEventListener('visibilitychange', tick)
  state.timer = setInterval(tick, POLL_MS)
  setInterval(ping, PING_MS)
  $('offline-dismiss').addEventListener('click', () => {
    if (offline.stoppedHere) return closeThisTab($('offline-text'))
    offline.dismissed = true
    showGlass()
  })
  $('page-reload').addEventListener('click', () => location.reload())

  setLink('connecting', 'connecting…')
  setUpPowerMenu()
  loadState().then(() => select(state.tab, false))
}

  start()
})()
