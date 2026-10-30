// Minimal typed event bus: stores announce changes through it and anything can
// listen, without a state-management library.

type Handler<A extends unknown[]> = (...args: A) => void

export interface Bus<E extends Record<string, unknown[]>> {
  /** Subscribes and returns the matching unsubscribe. */
  on<K extends keyof E>(event: K, fn: Handler<E[K]>): () => void
  off<K extends keyof E>(event: K, fn: Handler<E[K]>): void
  emit<K extends keyof E>(event: K, ...args: E[K]): void
}

export function createBus<E extends Record<string, unknown[]>>(): Bus<E> {
  // Copy-on-write lists: on/off replace the array, so emit iterates a stable snapshot
  // (handlers may unsubscribe or subscribe others meanwhile) without copying per event.
  const handlers = new Map<keyof E, readonly Handler<never>[]>()
  const bus: Bus<E> = {
    on(event, fn) {
      const list = handlers.get(event) ?? []
      if (!list.includes(fn as Handler<never>)) handlers.set(event, [...list, fn as Handler<never>])
      return () => bus.off(event, fn)
    },
    off(event, fn) {
      const list = handlers.get(event)
      if (!list) return
      const rest = list.filter((h) => h !== fn)
      if (rest.length === 0) handlers.delete(event)
      else if (rest.length !== list.length) handlers.set(event, rest)
    },
    emit(event, ...args) {
      const list = handlers.get(event)
      if (!list) return
      for (const fn of list) (fn as Handler<E[typeof event]>)(...args)
    },
  }
  return bus
}
