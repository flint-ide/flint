import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { profileEnv, profileStatus, shellCommand, timeLeft, validProfileName } from './aws'

const now = Date.parse('2026-10-31T10:00:00Z')

describe('AWS profiles', () => {
  it('counts down to expiry', () => {
    assert.equal(profileStatus({ status: 'valid', expiresAt: '2026-10-31T12:05:00Z', source: '' }, now).text, '2h 05m')
    assert.equal(profileStatus({ status: 'valid', expiresAt: '2026-10-31T10:10:00Z', source: '' }, now).tone, 'warn')
    assert.equal(profileStatus({ status: 'valid', expiresAt: '2026-10-31T09:00:00Z', source: '' }, now).text, 'expired')
  })

  it('labels profiles without an expiry', () => {
    assert.equal(profileStatus({ status: 'logged-out', expiresAt: '', source: '' }, now).text, 'logged out')
    assert.equal(profileStatus({ status: 'static', expiresAt: '', source: '' }, now).text, 'keys')
    assert.match(profileStatus({ status: 'unknown', expiresAt: '', source: 'default' }, now).title, /default/)
  })

  it('formats time left', () => {
    assert.equal(timeLeft(45_000), '45s')
    assert.equal(timeLeft(12 * 60_000), '12m')
    assert.equal(timeLeft(3 * 86_400_000), '3d')
  })

  it('quotes commands and builds the environment', () => {
    assert.equal(shellCommand(['saml2aws', 'login', '--profile', 'a b', "--role=it's"]), "saml2aws login --profile 'a b' '--role=it'\\''s'")
    assert.deepEqual(profileEnv({ name: 'prod', region: 'eu-west-1' }), { AWS_PROFILE: 'prod', AWS_REGION: 'eu-west-1' })
    assert.deepEqual(profileEnv({ name: 'dev', region: '' }), { AWS_PROFILE: 'dev' })
  })

  it('rejects names that read as flags', () => {
    assert.equal(validProfileName('bigdata-prod'), true)
    assert.equal(validProfileName('--debug'), false)
  })
})
