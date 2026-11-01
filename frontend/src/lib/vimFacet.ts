// Which editors Vim's :w and :q may act on. Those commands run Save and Close
// Tab for the active tab, so only an editor that is that tab's own document
// (a file or a scratchpad) opts in; diffs and response views don't.
// Kept apart from lib/vim.ts so editors can opt in before Vim is loaded.
import { Facet } from '@codemirror/state'

export const vimFileCommands = Facet.define<boolean, boolean>({ combine: (values) => values.some(Boolean) })
