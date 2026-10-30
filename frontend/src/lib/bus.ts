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
  const handlers = new Map<keyof E, Set<Handler<never>>>()
  const bus: Bus<E> = {
    on(event, fn) {
      const set = handlers.get(event) ?? new Set()
      set.add(fn as Handler<never>)
      handlers.set(event, set)
      return () => bus.off(event, fn)
    },
    off(event, fn) {
      const set = handlers.get(event)
      if (!set) return
      set.delete(fn as Handler<never>)
      if (set.size === 0) handlers.delete(event)
    },
    emit(event, ...args) {
      const set = handlers.get(event)
      if (!set) return
      // Snapshot: handlers may unsubscribe (or subscribe others) while we iterate.
      for (const fn of [...set]) (fn as Handler<E[typeof event]>)(...args)
    },
  }
  return bus
}
