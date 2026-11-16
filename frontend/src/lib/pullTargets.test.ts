import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { pullHint, pullSummary, pullTargets } from './pullTargets'

const repo = (folder: string, over: Partial<{ branch: string; head: string; upstream: string; behind: number }> = {}) => ({
  folder,
  root: folder,
  status: { branch: 'main', head: '0123456789abcdef', upstream: 'origin/main', behind: 0, ...over },
})

describe('update project picker', () => {
  it('has nothing to ask with one repository', () => {
    assert.deepEqual(pullTargets([], ['/ws/a'], '/ws/a'), [])
    assert.deepEqual(pullTargets([repo('/ws/a')], ['/ws/a', '/ws/docs'], '/ws/a'), [])
  })

  it('offers every repository with its folder label and marks the shown one', () => {
    const targets = pullTargets(
      [repo('/ws/api/app', { behind: 3 }), repo('/ws/web/app', { branch: '', upstream: '' })],
      ['/ws/api/app', '/ws/notes', '/ws/web/app'],
      '/ws/web/app',
    )
    assert.deepEqual(targets, [
      { root: '/ws/api/app', label: 'app (api)', branch: 'main', behind: 3, noUpstream: false, current: false },
      { root: '/ws/web/app', label: 'app (web)', branch: 'detached 0123456', behind: 0, noUpstream: true, current: true },
    ])
    assert.deepEqual(targets.map(pullHint), ['main · ↓3', 'detached 0123456 · no upstream'])
  })

  it('labels a repository found outside the workspace folders by its folder name', () => {
    const [first] = pullTargets([repo('/elsewhere/lib'), repo('/ws/a')], ['/ws/a'], null)
    assert.equal(first.label, 'lib')
    assert.equal(pullHint(first), 'main')
  })

  it('says what was updated and what failed', () => {
    assert.deepEqual(pullSummary(['api'], []), { message: 'Updated api', kind: 'info' })
    assert.deepEqual(pullSummary(['api', 'web'], []), { message: 'Updated 2 projects', kind: 'info' })
    assert.deepEqual(pullSummary([], []), { message: 'Nothing to update', kind: 'info' })
    assert.deepEqual(pullSummary([], [{ label: 'web', error: 'no upstream' }]), { message: 'Update failed.\nweb: no upstream', kind: 'error' })
    assert.deepEqual(pullSummary([], [{ label: 'web', error: '\nerror: local changes would be overwritten\nhint: save them first\n  a.go' }]), {
      message: 'Update failed.\nweb: error: local changes would be overwritten',
      kind: 'error',
    })
    assert.equal(pullSummary([], [{ label: 'web', error: 'x'.repeat(500) }]).message.length, 'Update failed.\nweb: '.length + 200)
    assert.deepEqual(pullSummary(['api'], [{ label: 'web', error: 'conflict' }, { label: 'docs', error: 'offline' }]), {
      message: 'Updated api. Failed:\nweb: conflict\ndocs: offline',
      kind: 'error',
    })
  })
})
