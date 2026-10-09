// The companion page. Everything that came from sbx — host names above all,
// which the sandbox itself chooses — goes into the page as text, never as
// markup: see el() and its use of textContent.
(function () {
  'use strict'

  const POLL_MS = 2000
  const TABS = ['clipboard', 'log', 'rules', 'fs']

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

  async function loadState() {
    try {
      const res = await api('/api/state')
      reached(true)
      if (!res.ok) { problem(res); return }
      state.sandbox = res.body.sandbox || ''
      $('sandbox').textContent = state.sandbox
      document.title = state.sandbox ? 'vibe · ' + state.sandbox : 'vibe companion'
      const url = res.body.clipboardUrl || ''
      $('tab-clipboard').hidden = !url
      if (url !== state.clipboardUrl) {
        state.clipboardUrl = url
        if (url) {
          $('clipboard-frame').src = url
          $('clipboard-open').href = url
        }
      }
      if (!url && state.tab === 'clipboard') select('log')
    } catch (e) {
      reached(false)
    }
  }

  function tick() {
    if (document.visibilityState !== 'visible') return
    if (state.tab === 'clipboard') { loadState(); return }
    loadState()
    load(state.tab)
  }

  // --- tabs -------------------------------------------------------------

  function select(tab) {
    if (!TABS.includes(tab)) tab = 'log'
    if (tab === 'clipboard' && !state.clipboardUrl) tab = 'log'
    state.tab = tab
    store.set('vibe-companion-tab', tab)
    for (const t of TABS) {
      $('panel-' + t).hidden = t !== tab
      const button = $('tab-' + t)
      button.setAttribute('aria-selected', String(t === tab))
      button.tabIndex = t === tab ? 0 : -1
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

  $('clipboard-reload').addEventListener('click', () => {
    if (state.clipboardUrl) $('clipboard-frame').src = state.clipboardUrl
  })

  document.addEventListener('visibilitychange', tick)
  state.timer = setInterval(tick, POLL_MS)

  setLink(false, 'connecting…')
  loadState().then(() => select(state.tab))
})()
