import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { addRootError, displayPath, labelOf, pickRoot, rootLabels, rootOf, withoutRoot } from './workspaceRoots'

const roots = ['/w/api', '/w/web']

describe('workspace roots', () => {
  it('finds the root of a path by segment', () => {
    assert.equal(rootOf(roots, '/w/api/main.go'), '/w/api')
    assert.equal(rootOf(roots, '/w/web'), '/w/web')
    assert.equal(rootOf(roots, '/w/apix/main.go'), null)
  })

  it('rejects duplicate and nested folders', () => {
    assert.ok(/already in/.test(addRootError(roots, '/w/api') ?? ''))
    assert.ok(/inside api/.test(addRootError(roots, '/w/api/internal') ?? ''))
    assert.ok(/contains api/.test(addRootError(roots, '/w') ?? ''))
    assert.equal(addRootError(roots, '/other/api'), null)
  })

  it('labels clashing folder names like workspace.Labels in Go', () => {
    assert.deepEqual(rootLabels(['/w/api', '/w/web']), ['api', 'web'])
    assert.deepEqual(rootLabels(['/a/app', '/b/app', '/c/web']), ['app (a)', 'app (b)', 'web'])
    assert.deepEqual(rootLabels(['/x/a/app', '/y/a/app']), ['app (a)', 'app 2'])
    assert.equal(labelOf(['/a/app', '/b/app'], '/b/app'), 'app (b)')
    assert.equal(labelOf(roots, '/elsewhere/tool'), 'tool')
  })

  it('picks the root of the active context, else the primary', () => {
    assert.equal(pickRoot(roots, '/w/web/src/app.ts'), '/w/web')
    assert.equal(pickRoot(roots, '/tmp'), '/w/api')
    assert.equal(pickRoot(roots, null), '/w/api')
    assert.equal(pickRoot([], '/w/web'), null)
  })

  it('drops entries of a removed root', () => {
    const rec = { '/w/api': 1, '/w/api/pkg': 2, '/w/web': 3, '/w/apix': 4 }
    assert.deepEqual(withoutRoot(rec, '/w/api'), { '/w/web': 3, '/w/apix': 4 })
  })

  it('shows paths relative to their root, with the label when there are several', () => {
    assert.equal(displayPath(['/w/api'], '/w/api/main.go'), 'main.go')
    assert.equal(displayPath(roots, '/w/web/src/app.ts'), 'web/src/app.ts')
    assert.equal(displayPath(roots, '/w/web'), 'web')
    assert.equal(displayPath(roots, '/tmp/x'), '/tmp/x')
  })
})
