import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { age, diagnosePrompt, execCommand, fluxReconcileCommand, isAuthError, logsCommand, podTone, rolloutBadge, shortContext, shortRevision, workloadTone } from './kube'
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

describe('apps view', () => {
  it('labels rollouts', () => {
    assert.deepEqual(rolloutBadge({ rollout: 'complete', revision: '7', rolloutMessage: '' }).text, 'rev 7 ✓')
    assert.equal(rolloutBadge({ rollout: 'failed', revision: '', rolloutMessage: 'timed out' }).tone, 'bad')
    assert.equal(rolloutBadge({ rollout: 'progressing', revision: '', rolloutMessage: '1 of 3 new replicas updated' }).title, '1 of 3 new replicas updated')
  })

  it('shortens Flux revisions', () => {
    assert.equal(shortRevision('main@sha1:abc1234def5678'), 'main@abc1234')
    assert.equal(shortRevision('sha256:0123456789abcdef'), '0123456')
    assert.equal(shortRevision('v1.2.3'), 'v1.2.3')
  })

  it('reconciles Flux in the right namespace', () => {
    assert.equal(shellCommand(fluxReconcileCommand('prod', { name: 'shop', namespace: 'flux-system' })), 'flux --context=prod reconcile kustomization shop --namespace=flux-system --with-source')
  })
})

describe('AI diagnosis', () => {
  it('pins the cluster and keeps the agent read-only', () => {
    const p = diagnosePrompt('prod', 'shop', 'shop', 'Problems:\n- pod crashed\n')
    assert.match(p, /--context=prod --namespace=shop/)
    assert.match(p, /Do not change the cluster/)
    assert.ok(p.endsWith('- pod crashed'))
  })
})
