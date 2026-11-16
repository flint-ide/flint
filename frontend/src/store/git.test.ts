import { beforeEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { changesUnder, closePullPicker, commitTargets, rollbackPrompt, tickedOf, updateProject, useGit, type FileChange, type GitStatus } from './git'
import { useWorkspace } from './workspace'

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

  it('never amends another repository than the shown one', () => {
    useGit.setState({ amend: true })
    assert.deepEqual(targets(), [['/ws/a', ['a.go']]])
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

describe('rollback of the ticked files', () => {
  it('takes the ticked files of a group: tracked by default, unversioned once ticked', () => {
    const paths = () => tickedOf(a.files, '/ws/a').map((f) => f.path)
    assert.deepEqual(paths(), ['a.go'])
    useGit.getState().setGroup([file('new.txt', 'untracked')], true, '/ws/a')
    assert.deepEqual(paths(), ['a.go', 'new.txt'])
    useGit.getState().toggleFile(file('a.go'), '/ws/a')
    assert.deepEqual(paths(), ['new.txt'])
    assert.deepEqual(tickedOf([file('c.go', 'conflicted')], '/ws/a'), [])
  })

  it('asks to delete when only unversioned files are chosen', () => {
    assert.deepEqual(rollbackPrompt([file('new.txt', 'untracked')]), {
      message: 'Delete new.txt? It is removed from disk and cannot be restored.',
      label: 'Delete',
    })
    assert.deepEqual(rollbackPrompt([file('a.txt', 'untracked'), file('b.txt', 'untracked')]), {
      message: 'Delete 2 unversioned files? They are removed from disk and cannot be restored.',
      label: 'Delete',
    })
  })

  it('asks to roll back otherwise, counting the files that get deleted', () => {
    assert.deepEqual(rollbackPrompt([file('a.go')]), { message: 'Rollback changes in a.go?', label: 'Rollback' })
    assert.deepEqual(rollbackPrompt([file('a.go'), file('n.go', 'added'), file('u.txt', 'untracked')]), {
      message: 'Rollback changes in 3 files? 2 new file(s) will be deleted.',
      label: 'Rollback',
    })
  })
})

describe('update project', () => {
  it('asks which repository to pull when the workspace has several', () => {
    useWorkspace.setState({ roots: ['/ws/a', '/ws/b'] })
    updateProject()
    assert.equal(useGit.getState().pullPicker, true)
    closePullPicker()
    assert.equal(useGit.getState().pullPicker, false)
  })

  it('does not ask while another remote operation runs', () => {
    useGit.setState({ busy: 'fetch' })
    updateProject()
    assert.equal(useGit.getState().pullPicker, false)
    useGit.setState({ busy: null })
  })
})
