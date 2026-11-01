import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceDocker, DEFAULT_DOCKER, validDockerHost } from './dockerSettings'

describe('docker settings', () => {
  it('defaults for anything missing or invalid', () => {
    assert.deepEqual(coerceDocker(undefined), DEFAULT_DOCKER)
    assert.deepEqual(coerceDocker({ host: 'ssh://x', logTailLines: 'many' }), DEFAULT_DOCKER)
  })
  it('clamps and trims', () => {
    assert.deepEqual(coerceDocker({ host: ' unix:///a.sock ', logTailLines: 99999 }), { host: 'unix:///a.sock', logTailLines: 10000 })
    assert.equal(coerceDocker({ logTailLines: -4 }).logTailLines, 1)
  })
  it('accepts the hosts Go accepts', () => {
    for (const ok of ['', '/var/run/docker.sock', 'unix:///a', 'tcp://h:2376', 'https://h']) assert.ok(validDockerHost(ok), ok)
    for (const bad of ['ssh://h', 'a b', 'npipe:////x', 'tcp://']) assert.ok(!validDockerHost(bad), bad)
  })
})
