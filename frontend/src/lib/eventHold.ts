/**
 * Holds the events of items that are not ready for them yet (a chat whose
 * folder is still being opened before the chat is adopted), so none is lost,
 * and hands them back in arrival order. Each held list is bounded.
 */
export interface EventHold<T> {
  /** Starts holding events for id. */
  begin: (id: string) => void
  /** Keeps e when id is being held and returns true; false means "not held: deliver it". */
  offer: (id: string, e: T) => boolean
  /** Stops holding id and returns what it kept. */
  end: (id: string) => T[]
}

export function createEventHold<T>(max: number): EventHold<T> {
  const held = new Map<string, T[]>()
  return {
    begin: (id) => {
      if (!held.has(id)) held.set(id, [])
    },
    offer: (id, e) => {
      const list = held.get(id)
      if (!list) return false
      if (list.length < max) list.push(e)
      return true
    },
    end: (id) => {
      const list = held.get(id) ?? []
      held.delete(id)
      return list
    },
  }
}
