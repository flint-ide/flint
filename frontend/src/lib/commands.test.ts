import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  commandForKey,
  formatKeys,
  getCommand,
  listCommands,
  matchesKeys,
  parseKeys,
  registerCommand,
  registerCommands,
  runCommand,
  setCommandErrorHandler,
  type Command,
  type KeyEventLike,
} from './commands'

const noop = () => undefined
const key = (k: string, mods: Partial<Omit<KeyEventLike, 'key'>> = {}): KeyEventLike => ({
  key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...mods,
})
const nowhere = () => false

let cleanups: (() => void)[] = []
const register = (cmd: Command) => cleanups.push(registerCommand(cmd))

afterEach(() => {
  cleanups.forEach((off) => off())
  cleanups = []
})

describe('registry', () => {
  it('lists commands in registration order and unregisters them', () => {
    const off = registerCommands([
      { id: 't.a', title: 'A', run: noop },
      { id: 't.b', title: 'B', run: noop },
    ])
    assert.deepEqual(listCommands().map((c) => c.id), ['t.a', 't.b'])
    off()
    assert.deepEqual(listCommands(), [])
  })

  it('replaces a command with the same id; the stale unregister keeps the new one', () => {
    const offOld = registerCommand({ id: 't.a', title: 'Old', run: noop })
    register({ id: 't.a', title: 'New', run: noop })
    offOld()
    assert.equal(getCommand('t.a')?.title, 'New')
  })

  it('hands out a fresh list so callers cannot mutate the registry', () => {
    register({ id: 't.a', title: 'A', run: noop })
    listCommands().pop()
    assert.equal(listCommands().length, 1)
  })
})

describe('shortcuts', () => {
  it('parses modifiers in any case and order', () => {
    assert.deepEqual(parseKeys('Shift+Mod+P'), { key: 'p', meta: true, ctrl: false, alt: false, shift: true })
    assert.deepEqual(parseKeys('Ctrl+Alt+F7'), { key: 'f7', meta: false, ctrl: true, alt: true, shift: false })
    assert.equal(parseKeys('Mod++').key, '+')
    assert.throws(() => parseKeys('Hyper+K'), /Unknown modifier/)
  })

  it('matches modifiers exactly and ignores the letter case', () => {
    assert.ok(matchesKeys(key('p', { metaKey: true }), 'Mod+P'))
    assert.ok(matchesKeys(key('P', { metaKey: true, shiftKey: true }), 'Mod+Shift+P'))
    assert.ok(!matchesKeys(key('p', { metaKey: true, shiftKey: true }), 'Mod+P'))
    assert.ok(!matchesKeys(key('p', { metaKey: true, altKey: true }), 'Mod+P'))
    assert.ok(!matchesKeys(key('p', { ctrlKey: true }), 'Mod+P'))
    assert.ok(matchesKeys(key('[', { metaKey: true }), 'Mod+['))
  })

  it('formats shortcuts the macOS way', () => {
    assert.equal(formatKeys('Mod+Shift+P'), '⇧⌘P')
    assert.equal(formatKeys('Mod+Enter'), '⌘↵')
    assert.equal(formatKeys('Alt+F7'), '⌥F7')
    assert.equal(formatKeys('Mod+Alt+G'), '⌥⌘G')
    assert.equal(formatKeys('Mod+['), '⌘[')
  })
})

describe('commandForKey', () => {
  it('finds the command bound to the event', () => {
    register({ id: 't.open', title: 'Open', keys: 'Mod+P', run: noop })
    register({ id: 't.palette', title: 'Palette', keys: 'Mod+Shift+P', run: noop })
    assert.equal(commandForKey(key('p', { metaKey: true }), nowhere)?.id, 't.open')
    assert.equal(commandForKey(key('P', { metaKey: true, shiftKey: true }), nowhere)?.id, 't.palette')
    assert.equal(commandForKey(key('p'), nowhere), undefined)
  })

  it('leaves the key alone inside keysIgnoredIn', () => {
    register({ id: 't.sidebar', title: 'Sidebar', keys: 'Mod+B', keysIgnoredIn: '.cm-editor', run: noop })
    const inEditor = (s: string) => s === '.cm-editor'
    assert.equal(commandForKey(key('b', { metaKey: true }), inEditor), undefined)
    assert.equal(commandForKey(key('b', { metaKey: true }), nowhere)?.id, 't.sidebar')
  })

  it('skips unavailable commands and lets later registrations shadow earlier ones', () => {
    register({ id: 't.global', title: 'Global', keys: 'Mod+R', run: noop })
    register({ id: 't.off', title: 'Off', keys: 'Mod+R', when: () => false, run: noop })
    assert.equal(commandForKey(key('r', { metaKey: true }), nowhere)?.id, 't.global')
    register({ id: 't.local', title: 'Local', keys: 'Mod+R', run: noop })
    assert.equal(commandForKey(key('r', { metaKey: true }), nowhere)?.id, 't.local')
  })

  it('treats a throwing when() as unavailable', () => {
    register({ id: 't.bad', title: 'Bad', keys: 'Mod+E', when: () => { throw new Error('boom') }, run: noop })
    assert.equal(commandForKey(key('e', { metaKey: true }), nowhere), undefined)
  })
})

describe('runCommand', () => {
  it('reports sync throws and rejected promises', async () => {
    const errors: string[] = []
    setCommandErrorHandler((err) => errors.push(String(err)))
    runCommand({ id: 't.sync', title: 'Sync', run: () => { throw new Error('sync') } })
    runCommand({ id: 't.async', title: 'Async', run: () => Promise.reject(new Error('async')) })
    await new Promise((r) => setTimeout(r, 0))
    assert.deepEqual(errors, ['Error: sync', 'Error: async'])
  })
})
