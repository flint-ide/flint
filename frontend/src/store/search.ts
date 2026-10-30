import { create } from 'zustand'
import { Search, onSearchBatch, onSearchDone, type SearchMatch, type SearchSummary } from '../api/backend'
import { search } from '../../wailsjs/go/models'

export interface SearchQuery {
  query: string
  regex: boolean
  caseSensitive: boolean
  wholeWord: boolean
  include: string
  exclude: string
}

interface SearchState {
  params: SearchQuery
  runningId: string | null
  matches: SearchMatch[]
  summary: SearchSummary | null
  error: string | null
  setParams: (p: Partial<SearchQuery>) => void
  run: (root: string) => Promise<void>
  cancel: () => void
}

export const useSearch = create<SearchState>((set, get) => ({
  params: { query: '', regex: false, caseSensitive: false, wholeWord: false, include: '', exclude: '' },
  runningId: null,
  matches: [],
  summary: null,
  error: null,

  setParams: (p) => set((s) => ({ params: { ...s.params, ...p } })),

  run: async (root) => {
    const { params } = get()
    if (!params.query) {
      void Search.Cancel()
      set({ runningId: null, matches: [], summary: null, error: null })
      return
    }
    const id = crypto.randomUUID()
    set({ runningId: id, matches: [], summary: null, error: null })
    await Search.Start(id, search.Options.createFrom({ ...params, root, maxResults: 5000 }))
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
