import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceGitHub, DEFAULT_GITHUB, isSignInError, parseScopes, validClientId, validGitHubHost } from './githubSettings'

describe('githubSettings', () => {
  it('accepts host names like Go does', () => {
    for (const h of ['github.com', 'ghe.example.com', 'localhost:8443', '127.0.0.1:34230']) assert.ok(validGitHubHost(h), h)
    for (const h of ['', 'https://github.com', 'github.com/x', 'a b', 'github.com:0', 'github.com:080', '-a.com', 'user@github.com']) {
      assert.ok(!validGitHubHost(h), h)
    }
  })

  it('checks client ids and scopes', () => {
    assert.ok(validClientId(''))
    assert.ok(validClientId('Ov23liAbC.d-1'))
    assert.ok(!validClientId('a b'))
    assert.deepEqual(parseScopes('repo, read:org repo  Bad!'), { scopes: ['repo', 'read:org'], invalid: ['Bad!'] })
  })

  it('coerces untrusted JSON to valid settings', () => {
    assert.deepEqual(coerceGitHub(undefined), { ...DEFAULT_GITHUB, scopes: ['repo', 'read:org', 'workflow'] })
    assert.deepEqual(coerceGitHub({ host: ' GHE.corp ', clientId: 'x y', scopes: ['repo', 3, 'nope!'], maxRetryWaitSeconds: 999 }), {
      host: 'ghe.corp',
      clientId: '',
      scopes: ['repo'],
      maxRetryWaitSeconds: 60,
    })
    assert.equal(coerceGitHub({ host: 'https://evil' }).host, 'github.com')
  })

  it('recognises the sign-in error', () => {
    assert.ok(isSignInError('not signed in to GitHub: sign in with GitHub (Settings ▸ Accounts), or use the token of the GitHub CLI'))
    assert.ok(!isSignInError('GitHub: Not Found (HTTP 404)'))
    assert.ok(!isSignInError(null))
  })
})
