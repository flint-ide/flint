import { createStore } from '../lib/store'
import { Search, onSearchBatch, onSearchDone, type SearchMatch, type SearchSummary } from '../api/backend'
import { search } from '../../wailsjs/go/models'

export interface SearchQuery {
  query: string
  regex: boolean
  caseSensitive: boolean
  wholeWord: boolean
  include: string
  exclude: string
  /** Workspace folder the search is limited to ("" = all of them), set by Find in Folder. */
  scope: string
}

interface SearchState {
  params: SearchQuery
  runningId: string | null
  matches: SearchMatch[]
  summary: SearchSummary | null
  error: string | null
  setParams: (p: Partial<SearchQuery>) => void
  /** Searches roots (or just the scope folder when it is one of them). */
  run: (roots: string[]) => Promise<void>
  cancel: () => void
}

export const useSearch = createStore<SearchState>((set, get) => ({
  params: { query: '', regex: false, caseSensitive: false, wholeWord: false, include: '', exclude: '', scope: '' },
  runningId: null,
  matches: [],
  summary: null,
  error: null,

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
    set({ runningId: id, matches: [], summary: null, error: null })
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
