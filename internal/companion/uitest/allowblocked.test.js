// The Network log's Allow on a host a deny rule blocks: a deny beats any allow, so an allow added beside it does nothing: the sandbox's
// deny rules that cover the host are removed, with the allow. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const deny = (over) => ({ id: 'd-exact', name: '', resources: ['www.kernel.org:443'], source: 'local', scope: 'sandbox:box', removable: true, wider: false, ...over })
const blocked = (blockedBy) => ({ host: 'www.kernel.org:443', decision: 'blocked', hits: 1, lastSeen: '2026-11-12T12:39:25Z', rule: '', ...(blockedBy ? { blockedBy } : {}) })

async function logPage(entries, routes = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': () => ({ entries }),
      '/api/allow': { command: 'sbx policy allow network ...' },
      ...routes,
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  await p.poll()
  return p
}

const allow = (p) => [...p.$('log-table').querySelectorAll('tr.entry')].find((r) => r.textContent.includes('kernel')).querySelector('button')
const posted = (p, path) => p.calls.filter((c) => c.path === path).map((c) => c.body)
const ask = (p) => ({ title: p.$('confirm-title').textContent, text: p.$('confirm-text').textContent, list: p.$('confirm-list').textContent, command: p.$('confirm-command').textContent })
const run = async (p) => { p.$('confirm').close('run'); await p.settle() }

test("Allow on a host the sandbox's own deny blocks removes that deny and adds the allow, in one request naming the rule", async () => {
  const p = await logPage([blocked([deny({ name: 'no kernel' })])])
  try {
    await p.click(allow(p))
    const q = ask(p)
    assert.match(q.title, /Allow www\.kernel\.org:443\?/)
    assert.match(q.text, /beats/i, 'nothing says why the block is removed')
    assert.match(q.list, /no kernel/)
    assert.match(q.command, /sbx policy rm network --sandbox box --id d-exact --force\nsbx policy allow network --sandbox box www\.kernel\.org:443/)
    await run(p)
    assert.deepEqual(posted(p, '/api/allow'), [{ host: 'www.kernel.org:443', remove: ['d-exact'] }])
  } finally { p.close() }
})

test('a block that covers more hosts says which, and that they are allowed too', async () => {
  const p = await logPage([blocked([deny({ id: 'd-wild', resources: ['*.kernel.org'], wider: true })])])
  try {
    await p.click(allow(p))
    assert.match(ask(p).text, /also blocks \*\.kernel\.org/)
    assert.match(ask(p).text, /allowed too/)
  } finally { p.close() }
})

test('a global deny is explained, with the command that removes it for every sandbox, and nothing is sent', async () => {
  const p = await logPage([blocked([deny({ id: 'g-1', name: 'g-1', scope: 'global' }), deny({ id: 'mine' })])])
  try {
    await p.click(allow(p))
    assert.equal(p.$('confirm').open, false)
    const said = p.$('toast').textContent
    assert.match(said, /global rule/)
    assert.ok(said.includes('sbx policy rm network --id g-1'), said)
    assert.deepEqual(posted(p, '/api/allow'), [], "the sandbox's own deny was removed and the global one left")
  } finally { p.close() }
})

test('a deny that cannot be removed from here is explained, and nothing is sent', async () => {
  const p = await logPage([blocked([deny({ id: 'org', name: 'company policy', removable: false, source: 'organisation' })])])
  try {
    await p.click(allow(p))
    assert.equal(p.$('confirm').open, false)
    assert.match(p.$('toast').textContent, /company policy/)
    assert.match(p.$('toast').textContent, /can't be removed here/)
    assert.deepEqual(posted(p, '/api/allow'), [])
  } finally { p.close() }
})

test('a host only the default blocks is allowed as it always was', async () => {
  const p = await logPage([blocked(null)])
  try {
    await p.click(allow(p))
    assert.match(ask(p).command, /^sbx policy allow network --sandbox box www\.kernel\.org:443$/)
    await run(p)
    assert.deepEqual(posted(p, '/api/allow'), [{ host: 'www.kernel.org:443' }])
  } finally { p.close() }
})

test('the row says it is updating while the removal and the allow are made', async () => {
  const p = await logPage([blocked([deny()])], { '/api/allow': () => new Promise(() => {}) })
  try {
    await p.click(allow(p))
    p.$('confirm').close('run')
    await new Promise((resolve) => setImmediate(resolve))
    await new Promise((resolve) => setImmediate(resolve))
    assert.match(p.$('log-table').textContent, /Updating…/)
  } finally { p.close() }
})

// The command that removes a global rule for every sandbox works for one sbx calls editable; for one it does not (an organisation's) it
// would fail, so it is not given.
test('a global deny sbx will not let anyone edit is explained without a command that cannot work', async () => {
  const p = await logPage([blocked([deny({ id: 'org-1', name: 'company policy', scope: 'global', removable: false, source: 'organisation' })])])
  try {
    await p.click(allow(p))
    assert.equal(p.$('confirm').open, false)
    const said = p.$('toast').textContent
    assert.match(said, /company policy/)
    assert.match(said, /can't be removed here/)
    assert.doesNotMatch(said, /sbx policy rm/, 'a command that cannot remove it is given: ' + said)
    assert.deepEqual(posted(p, '/api/allow'), [])
  } finally { p.close() }
})
