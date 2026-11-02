// Actions of mounted SQL consoles, keyed by tab id, so palette commands and global
// shortcuts (Explain, Query History) reach the console of the active tab.
import { useEditor } from '../../../store/editor'

export interface SqlConsoleActions {
  /** EXPLAIN the statement under the cursor (analyze: ANALYZE / ClickHouse PIPELINE). */
  explain: (analyze: boolean) => void
  showHistory: () => void
}

let consoles = new Map<string, SqlConsoleActions>()

/** Registers a console's actions; returns the unregister function (an effect cleanup). */
export function registerSqlConsole(tabId: string, actions: SqlConsoleActions): () => void {
  consoles = new Map(consoles).set(tabId, actions)
  return () => {
    if (consoles.get(tabId) !== actions) return
    const next = new Map(consoles)
    next.delete(tabId)
    consoles = next
  }
}

/** The actions of the active tab's SQL console, if the active tab is one. */
export function activeSqlConsole(): SqlConsoleActions | undefined {
  const id = useEditor.getState().activeId
  return id ? consoles.get(id) : undefined
}
