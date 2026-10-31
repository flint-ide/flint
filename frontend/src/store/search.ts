import { createStore } from '../lib/store'
import { errorMessage } from '../api/backend'
import { replaceTargets } from '../lib/replaceTargets'
import { showToast } from './dialog'
import { useEditor } from './editor'
import { Search, onSearchBatch, onSearchDone, type SearchMatch, type SearchSummary } from '../api/backend'
import { search } from '../wails/models'

export interface SearchQuery {
  query: string
  regex: boolean
  caseSensitive: boolean
  wholeWord: boolean
  include: string
  exclude: string
  /** Workspace folder the search is limited to ("" = all of them), set by Find in Folder. */
  scope: string
  /** Replace mode: matches come back with what they turn into (computed in Go). */
  replaceOn: boolean
  replace: string
}

/** The last replace, for its summary and "Undo replace". */
export interface ReplaceOutcome {
  files: number
  replacements: number
  undoable: boolean
}

interface SearchState {
  params: SearchQuery
  runningId: string | null
  matches: SearchMatch[]
  summary: SearchSummary | null
  error: string | null
  /** Unticked files (fileKey) and lines (matchKey) left out of a replace. */
  excluded: ReadonlySet<string>
  replacing: boolean
  lastReplace: ReplaceOutcome | null
  setParams: (p: Partial<SearchQuery>) => void
  toggleExcluded: (key: string) => void
  /** Replaces every ticked match, then searches again. */
  replaceAll: (roots: string[]) => Promise<void>
  undoReplace: (roots: string[]) => Promise<void>
  /** Searches roots (or just the scope folder when it is one of them). */
  run: (roots: string[]) => Promise<void>
  cancel: () => void
}

export const useSearch = createStore<SearchState>((set, get) => ({
  params: { query: '', regex: false, caseSensitive: false, wholeWord: false, include: '', exclude: '', scope: '', replaceOn: false, replace: '' },
  runningId: null,
  matches: [],
  summary: null,
  error: null,
  excluded: new Set(),
  replacing: false,
  lastReplace: null,

  toggleExcluded: (key) =>
    set((s) => {
      const excluded = new Set(s.excluded)
      if (!excluded.delete(key)) excluded.add(key)
      return { excluded }
    }),

  replaceAll: async (roots) => {
    const { params, matches, replacing } = get()
    if (replacing || !params.replaceOn) return
    const dirty = new Set(useEditor.getState().tabs.flatMap((t) => (t.kind === 'file' && t.dirty ? [t.path] : [])))
    const { targets, skippedDirty, count } = replaceTargets(matches, get().excluded, dirty)
    if (targets.length === 0) {
      showToast(skippedDirty.length ? 'Save or revert the edited files first' : 'Nothing to replace')
      return
    }
    set({ replacing: true })
    try {
      const { scope: _scope, ...options } = params
      const res = await Search.Replace(search.Options.createFrom({ ...options, root: '', maxResults: 0 }), targets)
      set({ lastReplace: { files: res.files, replacements: res.replacements, undoable: res.undoable } })
      const skipped = res.skipped.length + skippedDirty.length
      showToast(
        `Replaced ${res.replacements} of ${count} in ${res.files} files${skipped ? ` · ${skipped} skipped (edited or changed since the search)` : ''}`,
        skipped ? 'error' : 'info',
      )
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      set({ replacing: false, excluded: new Set() })
      void get().run(roots)
    }
  },

  undoReplace: async (roots) => {
    try {
      const res = await Search.UndoReplace()
      set({ lastReplace: null })
      showToast(`Restored ${res.restored} files${res.skipped.length ? ` · ${res.skipped.length} edited since, left as they are` : ''}`)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
    void get().run(roots)
  },

  setParams: (p) => set((s) => ({ params: { ...s.params, ...p } })),

  run: async (roots) => {
    const { params } = get()
    const { scope, ...options } = params
    const targets = scope && roots.includes(scope) ? [scope] : roots
    if (!params.query) {
      void Search.Cancel()
      set({ runningId: null, matches: [], summary: null, error: null })
      return
    }
    const id = crypto.randomUUID()
    set({ runningId: id, matches: [], summary: null, error: null, excluded: new Set() })
    await Search.Start(id, targets, search.Options.createFrom({ ...options, root: '', maxResults: 5000 }))
  },

  cancel: () => void Search.Cancel(),
}))

// Events from the backend carry the search id; anything stale is dropped.
let subscribed = false
export function subscribeSearchEvents(): void {
  if (subscribed) return
  subscribed = true
  onSearchBatch((b) => {
    const s = useSearch.getState()
    if (b.id === s.runningId) useSearch.setState({ matches: s.matches.concat(b.matches) })
  })
  onSearchDone((d) => {
    if (d.id !== useSearch.getState().runningId) return
    useSearch.setState({ runningId: null, summary: d.summary, error: d.error || null })
  })
}
