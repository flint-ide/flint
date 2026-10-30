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
})
