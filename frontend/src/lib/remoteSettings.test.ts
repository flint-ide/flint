import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceRemote, DEFAULT_REMOTE, formatCountdown, parseHosts, validRemoteHost } from './remoteSettings'
import { coerceSettings } from './settings'

describe('remote settings', () => {
  it('defaults to off on the fixed port', () => {
    assert.deepEqual(coerceSettings({}).remote, DEFAULT_REMOTE)
    assert.equal(DEFAULT_REMOTE.enabled, false)
    assert.equal(DEFAULT_REMOTE.port, 7419)
  })

  it('keeps valid values and defaults the rest, like Go', () => {
    const r = coerceRemote({ enabled: true, port: 80, allowedHosts: ['Laptop.tail1234.ts.net', 'bad host', 'laptop.tail1234.ts.net', 7], replayEvents: 5, requestsPerMinute: 600 })
    assert.equal(r.enabled, true)
    assert.equal(r.port, 7419)
    assert.deepEqual(r.allowedHosts, ['laptop.tail1234.ts.net'])
    assert.equal(r.replayEvents, 2000)
    assert.equal(r.requestsPerMinute, 600)
    assert.deepEqual(coerceRemote('x'), DEFAULT_REMOTE)
  })

  it('accepts plain host names only', () => {
    for (const ok of ['laptop.tail1234.ts.net', 'devbox', 'a-b.example.com']) assert.equal(validRemoteHost(ok), true, ok)
    for (const bad of ['', 'a b', 'https://a.ts.net', 'a.ts.net:443', '*.ts.net', '-a.ts.net', 'a..b', 'a/b']) assert.equal(validRemoteHost(bad), false, bad)
  })

  it('parses one host per line', () => {
    assert.deepEqual(parseHosts(' Laptop.ts.net \n\nlaptop.ts.net\nbad host\n'), { hosts: ['laptop.ts.net'], invalid: ['bad host'] })
  })

  it('formats the pairing countdown', () => {
    assert.equal(formatCountdown(120_000), '2:00')
    assert.equal(formatCountdown(61_500), '1:02')
    assert.equal(formatCountdown(9_000), '0:09')
    assert.equal(formatCountdown(-5), '0:00')
  })
})
