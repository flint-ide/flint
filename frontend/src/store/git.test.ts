import { beforeEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { changesUnder, commitTargets, useGit, type FileChange, type GitStatus } from './git'

const file = (path: string, kind = 'modified'): FileChange => ({ path, kind, index: '', worktree: 'M' }) as unknown as FileChange
const status = (files: FileChange[]): GitStatus =>
  ({ head: 'abc', branch: 'main', upstream: '', ahead: 0, behind: 0, operation: '', files }) as unknown as GitStatus

const a = status([file('a.go'), file('new.txt', 'untracked')])
const b = status([file('b.go')])

beforeEach(() =>
  useGit.setState({
    root: '/ws/a',
    folder: '/ws/a',
    isRepo: true,
    status: a,
    repos: [
      { folder: '/ws/a', root: '/ws/a', status: a },
      { folder: '/ws/b', root: '/ws/b', status: b },
    ],
    excluded: {},
    includedUntracked: {},
    amend: false,
  }),
)

const targets = () => commitTargets(useGit.getState()).map((t) => [t.root, t.files.map((f) => f.path)])

describe('commit across repositories', () => {
  it('commits the tracked changes of every repository, untracked ones only when ticked', () => {
    assert.deepEqual(targets(), [
      ['/ws/a', ['a.go']],
      ['/ws/b', ['b.go']],
    ])
    useGit.getState().toggleFile(file('new.txt', 'untracked'), '/ws/a')
    assert.deepEqual(targets()[0], ['/ws/a', ['a.go', 'new.txt']])
  })

  it('keeps ticks per repository even for equal relative paths', () => {
    const same = status([file('main.go')])
    useGit.setState({
      repos: [
        { folder: '/ws/a', root: '/ws/a', status: same },
        { folder: '/ws/b', root: '/ws/b', status: same },
      ],
      status: same,
    })
    useGit.getState().toggleFile(file('main.go'), '/ws/b')
    assert.deepEqual(targets(), [['/ws/a', ['main.go']]])
  })

  it('skips repositories with nothing ticked', () => {
    useGit.getState().setGroup(b.files, false, '/ws/b')
    assert.deepEqual(targets(), [['/ws/a', ['a.go']]])
  })

  it('amends the shown repository when nothing is ticked', () => {
    useGit.getState().setGroup(a.files, false, '/ws/a')
    useGit.getState().setGroup(b.files, false, '/ws/b')
    assert.deepEqual(targets(), [])
    useGit.setState({ amend: true })
    assert.deepEqual(targets(), [['/ws/a', []]])
  })

  it('uses the shown repository alone with one folder', () => {
    useGit.setState({ repos: [] })
    assert.deepEqual(targets(), [['/ws/a', ['a.go']]])
  })
})

describe('changesUnder', () => {
  it('groups changes by repository', () => {
    assert.deepEqual(
      changesUnder(['/ws/a/a.go', '/ws/b']).map((g) => [g.root, g.files.map((f) => f.path)]),
      [
        ['/ws/a', ['a.go']],
        ['/ws/b', ['b.go']],
      ],
    )
  })
})
