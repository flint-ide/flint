import { beforeEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { useEditor } from './editor'

const st = () => useEditor.getState()

beforeEach(() => useEditor.setState({ tabs: [], activeId: null, backStack: [], forwardStack: [] }))

describe('editor tabs history', () => {
  it('goes back from a file opened in the terminal to the terminal', () => {
    st().openTerminal('t1', 'zsh')
    st().openFile('/p/a.go', { line: 3, from: 0, to: 0 })
    assert.equal(st().activeId, 'file:/p/a.go')
    st().back()
    assert.equal(st().activeId, 'term:t1')
    st().forward()
    assert.equal(st().activeId, 'file:/p/a.go')
  })

  it('closing the active tab returns to the previously used one', () => {
    st().openTerminal('t1', 'zsh')
    st().openFile('/p/a.go')
    st().openFile('/p/b.go')
    st().setActive('term:t1')
    st().setActive('file:/p/b.go')
    st().closeTab('file:/p/b.go')
    assert.equal(st().activeId, 'term:t1')
  })

  it('skips closed tabs when going back and clears forward on navigation', () => {
    st().openTerminal('t1', 'zsh')
    st().openFile('/p/a.go')
    st().openFile('/p/b.go')
    st().closeTab('file:/p/a.go')
    st().back()
    assert.equal(st().activeId, 'term:t1')
    st().openFile('/p/c.go')
    assert.deepEqual(st().forwardStack, [])
  })

  it('reopening an existing chat or terminal just activates it', () => {
    st().openChat('s1', 'claude')
    st().openTerminal('t1', 'zsh')
    st().openChat('s1', 'claude')
    assert.equal((st().tabs.filter((t) => t.kind === 'chat')).length, 1)
    assert.equal(st().activeId, 'chat:s1')
  })

  it('restored tabs are appended in order without changing the active tab', () => {
    st().openFile('/p/a.go')
    st().addTab({ id: 'file:/p/b.go', kind: 'file', path: '/p/b.go', title: 'b.go', dirty: false })
    st().openTerminal('t1', 'zsh', true)
    st().openChat('s1', 'claude', true)
    st().addTab({ id: 'file:/p/a.go', kind: 'file', path: '/p/a.go', title: 'a.go', dirty: false })
    assert.deepEqual(st().tabs.map((t) => t.id), ['file:/p/a.go', 'file:/p/b.go', 'term:t1', 'chat:s1'])
    assert.equal(st().activeId, 'file:/p/a.go')
    assert.deepEqual(st().backStack, [])
  })
})

describe('revision diffs and history tabs', () => {
  const diff = { repoRoot: '/p', path: '/p/a.go', basePath: '/p/a.go', baseRev: 'abc1234^', rev: 'abc1234', baseLabel: 'abc1234^', label: 'abc1234' }

  it('opens one tab per revision pair, separate from the working-tree diff', () => {
    st().openDiff('/p', '/p/a.go')
    st().openRevisionDiff(diff)
    st().openRevisionDiff({ ...diff, baseRev: 'def5678^', rev: 'def5678', label: 'def5678' })
    st().openRevisionDiff(diff)
    assert.deepEqual(st().tabs.map((t) => t.id), ['diff:/p/a.go', 'diff:abc1234^..abc1234:/p/a.go', 'diff:def5678^..def5678:/p/a.go'])
    assert.equal(st().activeId, 'diff:abc1234^..abc1234:/p/a.go')
    const tab = st().tabs[1]
    assert.equal(tab.kind === 'diff' && tab.rev, 'abc1234')
    assert.equal(tab.title, 'Δ a.go @ abc1234')
  })

  it('titles a comparison with the working tree by its base', () => {
    st().openRevisionDiff({ repoRoot: '/p', path: '/p/a.go', basePath: '/p/old.go', baseRev: 'abc1234', baseLabel: 'abc1234' })
    assert.equal(st().tabs[0].title, 'Δ a.go vs abc1234')
    assert.equal(st().tabs[0].id, 'diff:abc1234..:/p/a.go')
  })

  it('reuses the history tab of a file', () => {
    st().openHistory('/p/a.go')
    st().openFile('/p/b.go')
    st().openHistory('/p/a.go')
    assert.deepEqual(st().tabs.map((t) => t.id), ['history:/p/a.go', 'file:/p/b.go'])
    assert.equal(st().activeId, 'history:/p/a.go')
    assert.equal(st().tabs[0].title, 'History: a.go')
  })

  it('does not notify for re-activating the active tab or an unchanged dirty flag', () => {
    st().openFile('/p/a.go')
    st().setDirty('file:/p/a.go', true)
    const before = st()
    let calls = 0
    const off = useEditor.subscribe(() => calls++)
    st().setActive('file:/p/a.go')
    st().setDirty('file:/p/a.go', true)
    st().setDirty('file:/p/missing.go', false)
    off()
    assert.equal(calls, 0)
    assert.equal(st(), before)
  })
})
