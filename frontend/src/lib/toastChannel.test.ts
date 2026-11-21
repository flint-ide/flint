import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { createToastChannel, type ToastRequest, toastDuration } from './toastChannel'

const t = (message: string): ToastRequest => ({ message, kind: 'info', ms: 2500 })

describe('createToastChannel', () => {
  it('delivers straight to a connected stack', () => {
    const ch = createToastChannel()
    const got: string[] = []
    ch.connect((x) => got.push(x.message))
    ch.send(t('saved'))
    assert.deepEqual(got, ['saved'])
  })

  it('keeps the last few toasts sent before the stack mounts', () => {
    const ch = createToastChannel(2)
    ch.send(t('a'))
    ch.send(t('b'))
    ch.send(t('c'))
    const got: string[] = []
    ch.connect((x) => got.push(x.message))
    assert.deepEqual(got, ['b', 'c'])
  })

  it('queues again after the stack detaches, and a stale detach does nothing', () => {
    const ch = createToastChannel()
    const first: string[] = []
    const second: string[] = []
    const detachFirst = ch.connect((x) => first.push(x.message))
    ch.connect((x) => second.push(x.message))
    detachFirst()
    ch.send(t('still the second'))
    assert.deepEqual(second, ['still the second'])
    assert.deepEqual(first, [])
  })
})

describe('toastDuration', () => {
  it('keeps errors longest', () => {
    assert.equal(toastDuration('error'), 6000)
    assert.equal(toastDuration('info'), 2500)
    assert.ok(toastDuration('warning') > toastDuration('success'))
  })
})
