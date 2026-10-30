// Tiny state container on top of the event bus: setState emits a `change` event,
// React components read it through useSyncExternalStore. The API mirrors the
// subset of zustand the app used: useX(selector), useX.getState/setState/subscribe.
import { useRef, useSyncExternalStore } from 'react'
import { createBus } from './bus'

export type SetState<T> = (partial: Partial<T> | ((state: T) => Partial<T>), replace?: boolean) => void
export type Listener<T> = (state: T, prev: T) => void

export interface StoreApi<T> {
  getState: () => T
  setState: SetState<T>
  subscribe: (listener: Listener<T>) => () => void
}

export interface UseStore<T> extends StoreApi<T> {
  (): T
  <U>(selector: (state: T) => U): U
}

type Init<T> = (set: SetState<T>, get: () => T, api: StoreApi<T>) => T

export function createStore<T extends object>(init: Init<T>): UseStore<T> {
  const bus = createBus<{ change: [prev: T] }>()
  let state: T
  const api: StoreApi<T> = {
    getState: () => state,
    setState: (partial, replace) => {
      const next = typeof partial === 'function' ? partial(state) : partial
      if (Object.is(next, state)) return
      const prev = state
      state = replace ? (next as T) : Object.assign({}, state, next)
      bus.emit('change', prev)
    },
    // Listeners get the live state, as in zustand: a listener that sets state again
    // must not leave the ones after it with an outdated snapshot.
    subscribe: (listener) => bus.on('change', (prev) => listener(state, prev)),
  }
  state = init(api.setState, api.getState, api)

  function useBoundStore<U>(selector?: (state: T) => U): U | T {
    const memo = useRef<Memo<T, unknown> | null>(null)
    const getSnapshot = () => {
      memo.current = select(memo.current, state, selector ?? identity)
      return memo.current.value
    }
    return useSyncExternalStore(api.subscribe, getSnapshot, getSnapshot) as U | T
  }
  return Object.assign(useBoundStore as UseStore<T>, api)
}

export interface Memo<T, U> {
  state: T
  selector: (state: T) => U
  value: U
}

/**
 * Runs the selector only when the state or selector changed, and keeps the previous
 * value when the new one is Object.is-equal. useSyncExternalStore needs a stable
 * snapshot between calls; a selector building a new array must not loop renders.
 */
export function select<T, U>(prev: Memo<T, U> | null, state: T, selector: (state: T) => U): Memo<T, U> {
  if (prev && prev.state === state && prev.selector === selector) return prev
  const value = selector(state)
  return { state, selector, value: prev && Object.is(prev.value, value) ? prev.value : value }
}

const identity = <T,>(s: T): T => s
