import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { createBus } from './bus'
import { createStore, select } from './store'

describe('createBus', () => {
  it('delivers events to subscribers until they unsubscribe', () => {
    const bus = createBus<{ ping: [number] }>()
    const got: number[] = []
    const off = bus.on('ping', (n) => got.push(n))
    bus.emit('ping', 1)
    off()
    bus.emit('ping', 2)
    assert.deepEqual(got, [1])
  })

  it('lets a handler unsubscribe another during emit without skipping it', () => {
    const bus = createBus<{ ping: [] }>()
    const got: string[] = []
    const offB = bus.on('ping', () => got.push('b'))
    bus.on('ping', () => {
      got.push('a')
      offB()
    })
    bus.emit('ping')
    bus.emit('ping')
    assert.deepEqual(got, ['b', 'a', 'a'])
  })
})

describe('createStore', () => {
  const counter = () =>
    createStore<{ n: number; label: string; inc: () => void }>((set, get) => ({
      n: 0,
      label: 'x',
      inc: () => set({ n: get().n + 1 }),
    }))

  it('merges partial updates and keeps actions', () => {
    const store = counter()
    store.getState().inc()
    store.setState((s) => ({ n: s.n + 10 }))
    assert.equal(store.getState().n, 11)
    assert.equal(store.getState().label, 'x')
    assert.equal(typeof store.getState().inc, 'function')
  })

  it('emits change events with the previous state', () => {
    const store = counter()
    const seen: [number, number][] = []
    const off = store.subscribe((s, prev) => seen.push([s.n, prev.n]))
    store.getState().inc()
    store.getState().inc()
    off()
    store.getState().inc()
    assert.deepEqual(seen, [
      [1, 0],
      [2, 1],
    ])
  })

  it('gives later listeners the live state after a re-entrant setState', () => {
    const store = counter()
    store.subscribe((s) => {
      if (s.n === 1 && s.label !== 'from listener') store.setState({ label: 'from listener' })
    })
    const seen: string[] = []
    store.subscribe((s) => seen.push(s.label))
    store.getState().inc()
    assert.equal(seen.at(-1), 'from listener')
  })

  it('replaces the whole state when asked', () => {
    const store = counter()
    const inc = store.getState().inc
    store.setState({ n: 5, label: 'y', inc }, true)
    assert.deepEqual(Object.keys(store.getState()).sort(), ['inc', 'label', 'n'])
  })

  it('skips the event when the updater returns the same state', () => {
    const store = counter()
    let calls = 0
    store.subscribe(() => calls++)
    store.setState((s) => s)
    assert.equal(calls, 0)
  })
})

describe('select', () => {
  it('reuses the snapshot while state and selector are unchanged', () => {
    const state = { items: [1, 2, 3] }
    const sel = (s: typeof state) => s.items.filter((i) => i > 1) // new array each call
    const first = select(null, state, sel)
    assert.equal(select(first, state, sel), first)
  })

  it('keeps the previous value when a new state selects an Object.is-equal one', () => {
    const sel = (s: { n: number }) => s.n
    const first = select(null, { n: 1 }, sel)
    const next = select(first, { n: 1 }, sel)
    assert.equal(next.value, first.value)
    assert.notEqual(next, first)
  })

  it('recomputes for a new selector', () => {
    const state = { n: 2 }
    const first = select(null, state, (s) => s.n)
    assert.equal(select(first, state, (s) => s.n * 10).value, 20)
  })
})
