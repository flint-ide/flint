import { createStore } from '../../lib/store'
import * as TodoAPI from '../../../bindings/flint/internal/features/todo/api'
import { errorMessage } from '../../api/backend'
import { useSettings } from '../../store/settings'
import type { TodoItem } from './todos'

interface TodoState {
  items: TodoItem[]
  truncated: boolean
  /** Dependency folders the scan left out (absolute). */
  skipped: string[]
  scanning: boolean
  error: string | null
  /** Tags shown (empty = all). */
  tags: ReadonlySet<string>
  query: string
}

export const useTodo = createStore<TodoState>(() => ({
  items: [], truncated: false, skipped: [], scanning: false, error: null, tags: new Set(), query: '',
}))

let pending: Promise<void> | null = null

/** Scans the workspace; a scan already running is shared. */
export function scanTodos(): Promise<void> {
  pending ??= (async () => {
    useTodo.setState({ scanning: true })
    try {
      const res = await TodoAPI.Scan(useSettings.getState().settings.todoExclude)
      useTodo.setState({ items: res.items ?? [], truncated: res.truncated, skipped: res.skipped ?? [], error: null })
    } catch (err) {
      useTodo.setState({ error: errorMessage(err) })
    } finally {
      useTodo.setState({ scanning: false })
      pending = null
    }
  })()
  return pending
}

export function toggleTodoTag(tag: string): void {
  useTodo.setState((s) => {
    const tags = new Set(s.tags)
    if (!tags.delete(tag)) tags.add(tag)
    return { tags }
  })
}

export const setTodoQuery = (query: string) => useTodo.setState({ query })
