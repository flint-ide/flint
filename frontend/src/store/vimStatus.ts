import { createStore } from '../lib/store'

// The Vim mode of the focused editor ('NORMAL', 'INSERT', 'VISUAL', …), shown in
// the status bar while Vim mode is on; '' when no editor reported one yet.
export const useVimStatus = createStore<{ mode: string }>(() => ({ mode: '' }))
