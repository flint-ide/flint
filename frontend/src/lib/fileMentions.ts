// The @-mention picker's rows for the files of a workspace folder (pure: no React, no backend).
import { matchPath, type FileMatch } from './highlight'

/** The picker's only group so far. */
export const FILES_GROUP = 'Files'

/** How many files one query asks the backend for. */
export const FILE_MENTION_LIMIT = 20

/** A row of the @ picker: `id` is the file's absolute path, what gets attached to the message. */
export interface FileMention {
  id: string
  label: string
  detail: string | undefined
  group: string
}

function toMention(match: FileMatch, root: string): FileMention {
  const id = matchPath(match, root)
  const base = match.root || root
  const rel = id.startsWith(`${base}/`) ? id.slice(base.length + 1) : id
  const cut = rel.lastIndexOf('/')
  return { id, label: rel.slice(cut + 1), detail: cut > 0 ? rel.slice(0, cut) : undefined, group: FILES_GROUP }
}

/** Fuzzy file matches as picker rows, in the backend's order (best first), each file once. */
export function fileMentionItems(matches: readonly FileMatch[], root: string): FileMention[] {
  const seen = new Set<string>()
  return matches.map((m) => toMention(m, root)).filter((item) => (seen.has(item.id) ? false : (seen.add(item.id), true)))
}
