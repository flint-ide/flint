import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  checkMachine,
  checkMachines,
  coerceMachines,
  endedOnMachine,
  machineBadge,
  machineLine,
  machinesSummary,
  remoteTabTitle,
  remoteTermId,
  restoreRemoteTab,
  splitRemoteTermId,
  suggestMachineId,
  tabBanner,
  withStatus,
  type Machine,
  type MachineStatus,
} from './machines'

const ok = (): Machine => ({ id: 'dev-box', name: 'Dev box', target: 'me@dev.example.com', port: 2222, command: 'flint' })
const long = (n: number) => 'a'.repeat(n)

// The values of internal/settings/machines_test.go: Go and this form must agree on them.
describe('checkMachine accepts what Go accepts', () => {
  const table: [string, (m: Machine) => void][] = [
    ['a normal machine', () => undefined],
    ['no name', (m) => void (m.name = '')],
    ['no port', (m) => void (m.port = 0)],
    ['max port', (m) => void (m.port = 65535)],
    ['bare host', (m) => void (m.target = 'devbox')],
    ['ssh alias with dash and dot', (m) => void (m.target = 'dev-box.internal')],
    ['underscore first', (m) => void (m.target = '_svc@host')],
    ['ip address', (m) => void (m.target = '10.0.0.5')],
    ['path command', (m) => void (m.command = '~/bin/flint')],
    ['absolute command', (m) => void (m.command = '/opt/flint/bin/flint-host')],
    ['empty command is the default', (m) => void (m.command = '')],
    ['one-char id', (m) => void (m.id = 'a')],
    ['digit id', (m) => void (m.id = '9')],
    ['32-char id', (m) => void (m.id = long(32))],
    ['unicode name', (m) => void (m.name = 'Büro ☕')],
  ]
  for (const [name, edit] of table) {
    it(name, () => {
      const m = ok()
      edit(m)
      assert.equal(checkMachine(m), '')
    })
  }
})

describe('checkMachine refuses what Go refuses', () => {
  const table: [string, (m: Machine) => void, string][] = [
    ['empty id', (m) => void (m.id = ''), 'id'],
    ['upper-case id', (m) => void (m.id = 'Dev'), 'id'],
    ['leading dash id', (m) => void (m.id = '-dev'), 'id'],
    ['id with colon', (m) => void (m.id = 'a:b'), 'id'],
    ['id with space', (m) => void (m.id = 'a b'), 'id'],
    ['33-char id', (m) => void (m.id = long(33)), 'id'],
    ['id with newline', (m) => void (m.id = 'dev\n'), 'id'],

    ['empty target', (m) => void (m.target = ''), 'target'],
    ['leading dash target', (m) => void (m.target = '-oProxyCommand=x'), 'target'],
    ['leading dash user', (m) => void (m.target = '-x@host'), 'target'],
    ['option as target', (m) => void (m.target = '-F/etc/passwd'), 'target'],
    ['space in target', (m) => void (m.target = 'host -v'), 'target'],
    ['semicolon', (m) => void (m.target = 'host;id'), 'target'],
    ['backtick', (m) => void (m.target = 'host`id`'), 'target'],
    ['command substitution', (m) => void (m.target = '$(id)@host'), 'target'],
    ['dollar', (m) => void (m.target = 'host$IFS'), 'target'],
    ['newline', (m) => void (m.target = 'host\nid'), 'target'],
    ['trailing newline', (m) => void (m.target = 'host\n'), 'target'],
    ['tab', (m) => void (m.target = 'host\tx'), 'target'],
    ['pipe', (m) => void (m.target = 'host|id'), 'target'],
    ['ampersand', (m) => void (m.target = 'host&id'), 'target'],
    ['quote', (m) => void (m.target = "ho'st"), 'target'],
    ['slash', (m) => void (m.target = 'host/x'), 'target'],
    ['empty user', (m) => void (m.target = '@host'), 'target'],
    ['empty host', (m) => void (m.target = 'user@'), 'target'],
    ['two at signs', (m) => void (m.target = 'a@b@c'), 'target'],
    ['host starts with dash', (m) => void (m.target = 'user@-host'), 'target'],
    ['host starts with dot', (m) => void (m.target = 'user@.host'), 'target'],
    ['cyrillic look-alike', (m) => void (m.target = 'dеv.example.com'), 'target'], // the e is U+0435
    ['fullwidth dash', (m) => void (m.target = '－oFoo'), 'target'],
    ['unicode host', (m) => void (m.target = 'user@bücher.example'), 'target'],
    ['64-char user', (m) => void (m.target = `${long(64)}@host`), 'target'],
    ['254-char host', (m) => void (m.target = `u@${long(254)}`), 'target'],
    ['64-char bare target', (m) => void (m.target = long(64)), 'target'],
    ['ipv6 brackets', (m) => void (m.target = '[::1]'), 'target'],
    ['nul', (m) => void (m.target = 'host\x00'), 'target'],

    ['negative port', (m) => void (m.port = -1), 'port'],
    ['port 65536', (m) => void (m.port = 65536), 'port'],
    ['fractional port', (m) => void (m.port = 22.5), 'port'],

    ['leading dash command', (m) => void (m.command = '-flint'), 'command'],
    ['command option', (m) => void (m.command = '--version'), 'command'],
    ['command with args', (m) => void (m.command = 'flint --x'), 'command'],
    ['command with semicolon', (m) => void (m.command = 'flint;id'), 'command'],
    ['command with backtick', (m) => void (m.command = '`id`'), 'command'],
    ['command substitution', (m) => void (m.command = '$(id)'), 'command'],
    ['command with newline', (m) => void (m.command = 'flint\nid'), 'command'],
    ['command with quote', (m) => void (m.command = "fl'int"), 'command'],
    ['command with glob', (m) => void (m.command = 'fl*'), 'command'],
    ['command with unicode', (m) => void (m.command = 'flіnt'), 'command'], // the і is U+0456
    ['257-char command', (m) => void (m.command = long(257)), 'command'],

    ['name with newline', (m) => void (m.name = 'a\nb'), 'name'],
    ['name with escape', (m) => void (m.name = 'a\x1b[31m'), 'name'],
    ['name with NUL', (m) => void (m.name = 'a\x00'), 'name'],
    ['name with DEL', (m) => void (m.name = 'a\x7f'), 'name'],
    ['padded name', (m) => void (m.name = ' a'), 'name'],
    ['65-char name', (m) => void (m.name = 'é'.repeat(65)), 'name'],
    ['lone surrogate name', (m) => void (m.name = 'a\ud800'), 'name'],
  ]
  for (const [name, edit, want] of table) {
    it(name, () => {
      const m = ok()
      edit(m)
      const why = checkMachine(m)
      assert.notEqual(why, '', `accepted ${JSON.stringify(m)}`)
      assert.ok(why.includes(want), `${why} does not mention ${want}`)
    })
  }

  it('never echoes a long rejected value whole', () => {
    const m = ok()
    m.target = `-${long(500)}`
    assert.ok(checkMachine(m).length < 300)
  })
})

describe('checkMachines', () => {
  it('refuses a repeated id and too many machines, accepts a good list', () => {
    assert.equal(checkMachines([ok(), { ...ok(), id: 'other' }]), '')
    assert.match(checkMachines([ok(), ok()]), /used twice/)
    const many = Array.from({ length: 33 }, (_, i) => ({ ...ok(), id: `m${i}` }))
    assert.match(checkMachines(many), /too many/)
    assert.match(checkMachines([{ ...ok(), target: '-x' }]), /target/)
  })
})

describe('coerceMachines', () => {
  it('keeps the shape and drops what is not an object', () => {
    assert.deepEqual(coerceMachines(undefined), [])
    assert.deepEqual(coerceMachines('x'), [])
    assert.deepEqual(coerceMachines([null, 3, { id: 'a', target: 'h', port: 22, command: 'flint' }]), [{ id: 'a', name: '', target: 'h', port: 22, command: 'flint' }])
    assert.equal(coerceMachines([{ id: 1, port: 'x' }])[0].port, 0)
  })
})

describe('suggestMachineId', () => {
  it('makes a valid id that is not taken', () => {
    const list = [{ ...ok(), id: 'build-box' }]
    assert.equal(suggestMachineId('Build Box!', []), 'build-box')
    assert.equal(suggestMachineId('Build Box', list), 'build-box-2')
    assert.equal(suggestMachineId('', []), 'machine')
    assert.equal(checkMachine({ ...ok(), id: suggestMachineId('Ünïcode  host', []) }), '')
    assert.ok(suggestMachineId(long(100), []).length <= 28)
  })
})

describe('terminal ids', () => {
  it('joins and splits like Go', () => {
    assert.equal(remoteTermId('box', 'abc'), 'box:abc')
    assert.deepEqual(splitRemoteTermId('box:abc'), { machine: 'box', session: 'abc' })
  })

  it('never reads a local id as remote, and refuses malformed ones', () => {
    for (let i = 0; i < 50; i++) assert.equal(splitRemoteTermId(crypto.randomUUID()), null)
    for (const bad of ['', ':x', 'box:', 'box:a:b', 'Box:x', '-box:x', 'bo x:y', `${long(33)}:x`, 'a\n:b']) {
      assert.equal(splitRemoteTermId(bad), null, bad)
    }
  })
})

describe('tab title and badge', () => {
  it('reads "<machine> ▸ <title>" with a short badge', () => {
    assert.equal(remoteTabTitle('Build box', 'npm test'), 'Build box ▸ npm test')
    assert.equal(machineBadge('Build box'), 'Build box')
    assert.equal(machineBadge('A very long machine name'), 'A very long…')
    assert.equal([...machineBadge('é'.repeat(30))].length, 12)
  })

  it('names a lost session', () => {
    assert.equal(endedOnMachine('Build box'), 'session ended on Build box')
  })
})

describe('restoreRemoteTab', () => {
  const known = new Set(['box'])
  it('is not for local tabs', () => {
    assert.equal(restoreRemoteTab(undefined, known), null)
    assert.equal(restoreRemoteTab(crypto.randomUUID(), known), null)
  })

  it('attaches when the machine is configured: it never starts a new shell', () => {
    assert.deepEqual(restoreRemoteTab('box:abc', known), { mode: 'attach', machine: 'box' })
  })

  it('opens ended when the machine was removed, and says why', () => {
    const r = restoreRemoteTab('gone:abc', known)
    assert.equal(r?.mode, 'ended')
    assert.match((r as { reason: string }).reason, /no longer in Settings/)
  })

  it('treats a malformed id as a local tab (the caller respawns it locally, never remotely)', () => {
    assert.equal(restoreRemoteTab('box:', known), null)
  })
})

const status = (over: Partial<MachineStatus>): MachineStatus => ({
  id: 'box', name: 'Build box', state: 'idle', error: '', stderr: '', attempt: 0, retryInSeconds: 0, ...over,
})

describe('machinesSummary', () => {
  it('is hidden while nothing needs attention', () => {
    assert.equal(machinesSummary([]), null)
    assert.equal(machinesSummary([status({ state: 'idle' }), status({ id: 'b', state: 'connected' }), status({ id: 'c', state: 'closed' })]), null)
  })

  it('shows connecting, reconnecting and failed', () => {
    assert.deepEqual(machinesSummary([status({ state: 'connecting' })])?.label, 'Build box: connecting…')
    assert.deepEqual(machinesSummary([status({ state: 'reconnecting', retryInSeconds: 4 })])?.label, 'Build box: reconnecting…')
    const failed = machinesSummary([status({ state: 'failed', error: 'ssh exited with status 255: Permission denied (publickey).' })])
    assert.equal(failed?.tone, 'error')
    assert.equal(failed?.label, 'Build box: connection failed')
    assert.match(failed?.details[0] ?? '', /Permission denied/)
  })

  it('counts several machines and lets a failure win', () => {
    const list = [status({ state: 'connecting' }), status({ id: 'b', name: 'B', state: 'connecting' })]
    assert.equal(machinesSummary(list)?.label, '2 machines connecting…')
    const mixed = machinesSummary([...list, status({ id: 'c', name: 'C', state: 'failed' })])
    assert.equal(mixed?.tone, 'error')
    assert.equal(mixed?.label, 'C: connection failed')
    assert.equal(mixed?.details.length, 3)
  })

  it('falls back to the id for a machine without a name', () => {
    assert.equal(machinesSummary([status({ name: '', state: 'connecting' })])?.label, 'box: connecting…')
  })
})

describe('machineLine and tabBanner', () => {
  it('puts the redacted ssh error and the retry wait in', () => {
    assert.equal(machineLine(status({ state: 'reconnecting', retryInSeconds: 8, error: 'connection lost' })), 'Build box: reconnecting… (next try in 8 s) — connection lost')
    assert.equal(
      machineLine(status({ state: 'failed', error: 'ssh exited with status 255', stderr: 'warning\nPermission denied (publickey).\n' })),
      'Build box: ssh exited with status 255 — Permission denied (publickey).',
    )
  })

  it('shows a banner only for states that need attention', () => {
    assert.equal(tabBanner(undefined), null)
    assert.equal(tabBanner(status({ state: 'connected' })), null)
    assert.equal(tabBanner(status({ state: 'connecting' }))?.tone, 'busy')
    assert.equal(tabBanner(status({ state: 'failed', error: 'x' }))?.tone, 'error')
  })
})

describe('withStatus', () => {
  it('replaces by id without touching the others', () => {
    const a = status({ id: 'a' })
    const b = status({ id: 'b' })
    const out = withStatus([a, b], { ...a, state: 'failed' })
    assert.equal(out[0].state, 'failed')
    assert.equal(out[1], b)
    assert.equal(withStatus([a], b).length, 2)
  })
})
