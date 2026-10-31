import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { age, execCommand, isAuthError, logsCommand, podTone, shortContext, workloadTone } from './kube'
import { shellCommand } from './shellArgs'

describe('kubernetes', () => {
  it('shortens context names', () => {
    assert.equal(shortContext('arn:aws:eks:eu-central-1:200283852841:cluster/das-sese-dev'), 'das-sese-dev (200283852841)')
    assert.equal(shortContext('gke_acme-prod_europe-west1-b_main'), 'main (acme-prod)')
    assert.equal(shortContext('docker-desktop'), 'docker-desktop')
  })

  it('tells ages', () => {
    const now = Date.parse('2026-10-31T10:00:00Z')
    assert.equal(age('2026-10-31T09:59:20Z', now), '40s')
    assert.equal(age('2026-10-31T07:00:00Z', now), '3h')
    assert.equal(age('2026-10-26T10:00:00Z', now), '5d')
  })

  it('pins context and namespace in every command', () => {
    const ctx = 'arn:aws:eks:eu-central-1:1:cluster/prod'
    assert.equal(
      shellCommand(logsCommand(ctx, 'apps', 'deployment/api')),
      'kubectl --context=arn:aws:eks:eu-central-1:1:cluster/prod --namespace=apps logs -f --tail=200 --all-containers --prefix deployment/api',
    )
    assert.equal(
      shellCommand(execCommand('docker-desktop', 'default', 'web-1', 'app')),
      "kubectl --context=docker-desktop --namespace=default exec -it web-1 -c app -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'",
    )
  })

  it('colors health', () => {
    assert.equal(podTone({ status: 'Running', healthy: true }), 'ok')
    assert.equal(podTone({ status: 'CrashLoopBackOff', healthy: false }), 'bad')
    assert.equal(podTone({ status: 'Pending', healthy: false }), 'warn')
    assert.equal(workloadTone({ ready: 1, desired: 3 }), 'warn')
    assert.equal(workloadTone({ ready: 0, desired: 0 }), 'muted')
  })

  it('spots expired cluster logins', () => {
    assert.equal(isAuthError('error: You must be logged in to the server (Unauthorized)'), true)
    assert.equal(isAuthError('ExpiredToken: The security token included in the request is expired'), true)
    assert.equal(isAuthError('pods "x" not found'), false)
  })
})
