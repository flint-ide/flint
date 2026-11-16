// The "What's New" page: the release notes Go fetched and sorted
// (internal/update/notes.go). Pure, so it is testable with node:test.

/** One entry of the notes, as update.NoteItem in Go. */
export interface NoteItem {
  text: string
  /** Its pull request, or '' when there is none to link. */
  url: string
}

/** What changed in a release, as update.Notes in Go. */
export interface ReleaseNotes {
  tag: string
  name: string
  url: string
  publishedAt: string
  features: NoteItem[]
  fixes: NoteItem[]
  other: NoteItem[]
}

/** The tab kind that shows release notes (ContributedTab.view). */
export const RELEASE_NOTES_VIEW = 'release-notes'

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const str = (v: unknown) => (typeof v === 'string' ? v : '')

const items = (v: unknown): NoteItem[] =>
  Array.isArray(v) ? v.filter(isObject).flatMap((i) => (str(i.text) ? [{ text: str(i.text), url: str(i.url) }] : [])) : []

/** Notes from a binding or a saved tab, or null when they are not notes. */
export function coerceNotes(v: unknown): ReleaseNotes | null {
  if (!isObject(v) || typeof v.tag !== 'string' || !v.tag) return null
  return {
    tag: v.tag,
    name: str(v.name),
    url: str(v.url),
    publishedAt: str(v.publishedAt),
    features: items(v.features),
    fixes: items(v.fixes),
    other: items(v.other),
  }
}

/** The tab's title: "What's New in v1.3.0". */
export const notesTitle = (n: Pick<ReleaseNotes, 'tag'>): string => `What's New in ${n.tag}`

/** A section of the page; empty ones are left out. */
export interface NotesSection {
  id: 'features' | 'fixes' | 'other'
  title: string
  items: NoteItem[]
}

/** The page's sections in reading order: what is new, what was fixed, the rest. */
export function notesSections(n: ReleaseNotes): NotesSection[] {
  const all: NotesSection[] = [
    { id: 'features', title: 'New Features', items: n.features },
    { id: 'fixes', title: 'Bug Fixes', items: n.fixes },
    { id: 'other', title: 'Other Changes', items: n.other },
  ]
  return all.filter((s) => s.items.length > 0)
}

/** What an entry's link says: "#80" for a pull request or an issue, "link" for anything else. */
export function linkLabel(url: string): string {
  const number = /\/(?:pull|issues)\/(\d+)$/.exec(url)?.[1]
  return number ? `#${number}` : 'link'
}

/** "October 30, 2026", or '' when GitHub gave no (valid) date. */
export function releaseDate(publishedAt: string): string {
  const t = Date.parse(publishedAt)
  // Go's zero time means "not said".
  if (Number.isNaN(t) || t <= 0) return ''
  return new Date(t).toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric', timeZone: 'UTC' })
}
