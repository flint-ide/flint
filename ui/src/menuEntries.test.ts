import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { contextMenuPoint, type ContextMenuEntry, underPoint, visibleEntries } from './menuEntries'

const noop = () => undefined
const a: ContextMenuEntry = { label: 'Copy', onSelect: noop }
const b: ContextMenuEntry = { label: 'Delete', onSelect: noop, danger: true }

describe('visibleEntries', () => {
  it('drops falsy items', () => {
    assert.deepEqual(visibleEntries([a, false, null, undefined, b]), [a, b])
  })
  it('trims leading, trailing and doubled separators', () => {
    assert.deepEqual(visibleEntries(['separator', a, 'separator', false, 'separator', b, 'separator']), [a, 'separator', b])
  })
  it('returns nothing when only separators are left', () => {
    assert.deepEqual(visibleEntries(['separator', false, 'separator']), [])
  })
})

describe('contextMenuPoint', () => {
  const box = { left: 10, top: 20, right: 110, bottom: 42 }
  it('opens at the pointer', () => {
    assert.deepEqual(contextMenuPoint({ x: 50, y: 30 }, box), { x: 50, y: 30 })
  })
  it('opens under the focused element when the keyboard fired it', () => {
    assert.deepEqual(contextMenuPoint({ x: 0, y: 0 }, box), { x: 18, y: 42 })
  })
  it('keeps (0, 0) when there is no target', () => {
    assert.deepEqual(contextMenuPoint({ x: 0, y: 0 }, null), { x: 0, y: 0 })
  })
})

describe('underPoint', () => {
  it('opens under the button, at its end', () => {
    assert.deepEqual(underPoint({ left: 80, top: 0, right: 100, bottom: 20 }), { x: 96, y: 20 })
  })
})
