import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { modalDismiss, type NodeLike } from './modalDismiss'

/** A tiny tree: each node knows its parent; contains() walks up from the other node. */
function node(parent: NodeLike | null = null): NodeLike {
  const self: NodeLike = {
    parentElement: parent,
    contains: (other) => {
      for (let n = other as NodeLike | null; n; n = n.parentElement ?? null) if (n === self) return true
      return false
    },
  }
  return self
}

const body = node()
const portal = node(body)
const backdrop = node(portal)
const popup = node(portal)
const input = node(popup)
const elsewhere = node(body) // a toast, a menu portaled to the body, another dialog
const key = (defaultPrevented = false) => ({ defaultPrevented, target: input })

describe('modalDismiss', () => {
  it('closes on Escape while focus is inside', () => {
    assert.equal(modalDismiss('escape-key', key(), popup, input, body, true), true)
  })
  it('closes on Escape when nothing has focus', () => {
    assert.equal(modalDismiss('escape-key', key(), popup, body, body, true), true)
    assert.equal(modalDismiss('escape-key', key(), popup, null, body, true), true)
  })
  it('stays on Escape that something inside already used', () => {
    assert.equal(modalDismiss('escape-key', key(true), popup, input, body, true), false)
  })
  it('stays on Escape while focus is in a menu, toast or dialog over it', () => {
    assert.equal(modalDismiss('escape-key', key(), popup, elsewhere, body, true), false)
  })
  it('stays on Escape while another dialog is over it, wherever focus is', () => {
    assert.equal(modalDismiss('escape-key', key(), popup, body, body, false), false)
    assert.equal(modalDismiss('escape-key', key(), popup, input, body, false), false)
  })
  it('closes on a press on its own backdrop', () => {
    assert.equal(modalDismiss('outside-press', { defaultPrevented: false, target: backdrop }, popup, input, body, true), true)
  })
  it('stays on a press anywhere else outside it', () => {
    assert.equal(modalDismiss('outside-press', { defaultPrevented: false, target: elsewhere }, popup, input, body, true), false)
    assert.equal(modalDismiss('outside-press', { defaultPrevented: false, target: input }, popup, input, body, true), false)
    assert.equal(modalDismiss('outside-press', undefined, popup, input, body, true), false)
  })
  it('stays when it cannot tell where it is', () => {
    assert.equal(modalDismiss('outside-press', { defaultPrevented: false, target: backdrop }, null, input, body, true), false)
  })
  it('closes for a close button or a programmatic close', () => {
    assert.equal(modalDismiss('close-press', undefined, popup, input, body, true), true)
    assert.equal(modalDismiss('imperative-action', undefined, popup, input, body, true), true)
  })
  it('stays when focus moves out', () => {
    assert.equal(modalDismiss('focus-out', undefined, popup, elsewhere, body, true), false)
  })
})
