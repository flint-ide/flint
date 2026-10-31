// The keymap override layer of lib/commands.ts.
import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  canonicalKeys,
  commandForKey,
  effectiveKeys,
  eventKey,
  getKeyOverrides,
  isOverridden,
  isRebindable,
  keyConflicts,
  keysFromEvent,
  registerCommand,
  sameKeys,
  setKeyOverrides,
  shownKeys,
  withOverride,
  type Command,
  type KeyEventLike,
} from './commands'

const noop = () => undefined
const key = (k: string, mods: Partial<Omit<KeyEventLike, 'key'>> = {}): KeyEventLike => ({
  key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...mods,
})
const nowhere = () => false

const push: Command = { id: 'git.push', title: 'Push', keys: 'Mod+Shift+K', run: noop }
const pull: Command = { id: 'git.pull', title: 'Pull', run: noop }
const find: Command = { id: 'editor.find', title: 'Find', hintKeys: 'Mod+F', run: noop }
const sidebar: Command = { id: 'view.sidebar', title: 'Sidebar', keys: 'Mod+B', run: noop }

let cleanups: (() => void)[] = []
const register = (cmd: Command) => cleanups.push(registerCommand(cmd))

afterEach(() => {
  cleanups.forEach((off) => off())
  cleanups = []
  setKeyOverrides({})
})

describe('effective keys', () => {
  it('uses the default without an override', () => {
    assert.equal(effectiveKeys(push, {}), 'Mod+Shift+K')
    assert.equal(effectiveKeys(pull, {}), undefined)
  })

  it('lets an override replace, add or remove a shortcut', () => {
    assert.equal(effectiveKeys(push, { 'git.push': 'Mod+Alt+P' }), 'Mod+Alt+P')
    assert.equal(effectiveKeys(pull, { 'git.pull': 'Mod+Alt+L' }), 'Mod+Alt+L')
    assert.equal(effectiveKeys(push, { 'git.push': '' }), undefined)
  })

  it('never rebinds keys owned by CodeMirror, but shows them', () => {
    assert.equal(isRebindable(find), false)
    assert.equal(effectiveKeys(find, { 'editor.find': 'Mod+G' }), undefined)
    assert.equal(shownKeys(find, { 'editor.find': 'Mod+G' }), 'Mod+F')
  })

  it('tells overridden commands apart, ignoring an override equal to the default', () => {
    assert.equal(isOverridden(push, { 'git.push': 'Mod+Alt+P' }), true)
    assert.equal(isOverridden(push, { 'git.push': 'shift+mod+k' }), false)
    assert.equal(isOverridden(push, { 'git.push': '' }), true)
    assert.equal(isOverridden(push, {}), false)
  })
})

describe('dispatch with overrides', () => {
  it('runs the command by its new keys only', () => {
    register(push)
    setKeyOverrides({ 'git.push': 'Mod+Alt+P' })
    assert.equal(commandForKey(key('k', { metaKey: true, shiftKey: true }), nowhere), undefined)
    assert.equal(commandForKey(key('π', { code: 'KeyP', metaKey: true, altKey: true }), nowhere)?.id, 'git.push')
  })

  it('follows override changes and removals', () => {
    register(push)
    register(pull)
    setKeyOverrides({ 'git.pull': 'Mod+Shift+K', 'git.push': '' })
    assert.equal(commandForKey(key('k', { metaKey: true, shiftKey: true }), nowhere)?.id, 'git.pull')
    setKeyOverrides({})
    assert.equal(commandForKey(key('k', { metaKey: true, shiftKey: true }), nowhere)?.id, 'git.push')
  })

  it('drops overrides that do not parse', () => {
    setKeyOverrides({ 'git.push': 'Hyper+K', 'git.pull': 'Mod+L', 'x.none': '' })
    assert.deepEqual(getKeyOverrides(), { 'git.pull': 'Mod+L', 'x.none': '' })
  })
})

describe('conflicts', () => {
  const all = [push, pull, find, sidebar]

  it('finds other commands showing the same chord in any spelling', () => {
    assert.deepEqual(keyConflicts('shift+mod+k', 'git.pull', all, {}).map((c) => c.id), ['git.push'])
    assert.deepEqual(keyConflicts('Mod+Shift+K', 'git.push', all, {}), [])
  })

  it('sees overridden keys and editor-owned ones', () => {
    const map = { 'git.pull': 'Mod+F' }
    assert.deepEqual(keyConflicts('Mod+F', 'view.sidebar', all, map).map((c) => c.id), ['git.pull', 'editor.find'])
    assert.deepEqual(keyConflicts('Mod+Shift+K', 'x', all, { 'git.push': '' }), [])
  })
})

describe('editing the keymap', () => {
  it('stores a new chord and drops entries equal to the default', () => {
    assert.deepEqual(withOverride({}, push, 'Mod+Alt+P'), { 'git.push': 'Mod+Alt+P' })
    assert.deepEqual(withOverride({ 'git.push': 'Mod+Alt+P' }, push, 'Shift+Mod+K'), {})
    assert.deepEqual(withOverride({ 'git.push': 'Mod+Alt+P', 'git.pull': 'Mod+L' }, push, null), { 'git.pull': 'Mod+L' })
  })

  it('records removal as an empty override', () => {
    assert.deepEqual(withOverride({}, push, ''), { 'git.push': '' })
    assert.deepEqual(withOverride({ 'git.pull': 'Mod+L' }, pull, ''), {})
  })

  it('leaves the input map untouched', () => {
    const map = Object.freeze({ 'git.pull': 'Mod+L' })
    withOverride(map, push, 'Mod+Alt+P')
    assert.deepEqual(map, { 'git.pull': 'Mod+L' })
  })
})

describe('recording chords', () => {
  it('spells chords canonically', () => {
    assert.equal(canonicalKeys('shift+mod+p'), 'Shift+Mod+P')
    assert.equal(canonicalKeys('Mod+Alt+Ctrl+Shift+f7'), 'Ctrl+Alt+Shift+Mod+F7')
    assert.equal(canonicalKeys('Mod+arrowup'), 'Mod+ArrowUp')
    assert.equal(canonicalKeys('Mod++'), 'Mod++')
    assert.equal(canonicalKeys('Mod+,'), 'Mod+,')
    assert.ok(sameKeys('Cmd+K', 'mod+k'))
    assert.ok(sameKeys('', undefined))
    assert.ok(!sameKeys('Mod+K', undefined))
  })

  it('names keys by their physical key under ⌥ and ⇧', () => {
    assert.equal(eventKey(key('∫', { code: 'KeyB', altKey: true })), 'b')
    assert.equal(eventKey(key('@', { code: 'Digit2', shiftKey: true })), '2')
    assert.equal(eventKey(key('Enter', { code: 'Enter' })), 'enter')
    assert.equal(eventKey(key(' ', { code: 'Space' })), 'space')
  })

  it('turns key events into shortcuts', () => {
    assert.equal(keysFromEvent(key('k', { code: 'KeyK', metaKey: true, shiftKey: true })), 'Shift+Mod+K')
    assert.equal(keysFromEvent(key('∂', { code: 'KeyD', metaKey: true, altKey: true })), 'Alt+Mod+D')
    assert.equal(keysFromEvent(key('F8')), 'F8')
    assert.equal(keysFromEvent(key('F8', { shiftKey: true })), 'Shift+F8')
    assert.equal(keysFromEvent(key(',', { metaKey: true })), 'Mod+,')
  })

  it('ignores lone modifiers and keys that type text', () => {
    assert.equal(keysFromEvent(key('Meta', { metaKey: true })), null)
    assert.equal(keysFromEvent(key('Shift', { shiftKey: true })), null)
    assert.equal(keysFromEvent(key('a')), null)
    assert.equal(keysFromEvent(key('A', { code: 'KeyA', shiftKey: true })), null)
    assert.equal(keysFromEvent(key('Enter')), null)
  })
})
