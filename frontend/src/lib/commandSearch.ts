// Palette filtering: subsequence fuzzy matching with IntelliJ-ish preferences
// (word starts and runs of consecutive letters beat scattered hits). A few hundred
// commands at most, so plain TS is plenty; file matching for ⌘P stays in Go.

export interface FuzzyMatch {
  score: number
  /** Indexes into the text of the matched characters (for highlighting). */
  positions: number[]
}

const WORD_START = 8
const CONSECUTIVE = 6
const FIRST_CHAR = 3

function isWordStart(text: string, i: number): boolean {
  if (i === 0) return true
  const prev = text[i - 1]
  if (/[\s\-_./:()…]/.test(prev)) return true
  // camelCase / PascalCase boundary
  return prev === prev.toLowerCase() && text[i] !== text[i].toLowerCase()
}

/**
 * Subsequence match of `q` (lower-case, no spaces) in `text` starting at `from`;
 * with `jump`, a letter that doesn't continue a run prefers the next word start.
 */
function matchFrom(q: string, text: string, lower: string, from: number, jump: boolean): FuzzyMatch | null {
  const positions: number[] = []
  let score = 0
  let at = from
  for (const ch of q) {
    // Prefer the next word start holding this letter, unless the letter follows right away.
    let i = lower.indexOf(ch, at)
    if (i < 0) return null
    if (jump && positions.length && i !== positions[positions.length - 1] + 1) {
      let j = i
      while (j >= 0 && !isWordStart(text, j)) j = lower.indexOf(ch, j + 1)
      if (j >= 0) i = j
    }
    const prev = positions[positions.length - 1]
    score += 1
    if (isWordStart(text, i)) score += WORD_START
    if (prev !== undefined && i === prev + 1) score += CONSECUTIVE
    if (prev !== undefined) score -= Math.min(i - prev - 1, 6) * 0.5
    positions.push(i)
    at = i + 1
  }
  if (positions[0] === 0) score += FIRST_CHAR
  return { score, positions }
}

/** Case-insensitive subsequence match; spaces in the query are ignored. Null when it doesn't match. */
export function fuzzyMatch(query: string, text: string): FuzzyMatch | null {
  const q = query.toLowerCase().replace(/\s+/g, '')
  if (!q) return { score: 0, positions: [] }
  const lower = text.toLowerCase()
  let best: FuzzyMatch | null = null
  // Try every start of the first letter: "push" should prefer "Git: Push" over a stray p.
  for (let start = lower.indexOf(q[0]); start >= 0; start = lower.indexOf(q[0], start + 1)) {
    const greedy = matchFrom(q, text, lower, start, false)
    if (!greedy) break // a later start cannot match if this one did not
    // Jumping to word starts can strand later letters, so the plain greedy run stays a candidate.
    for (const m of [greedy, matchFrom(q, text, lower, start, true)]) {
      if (m && (!best || m.score > best.score)) best = m
    }
  }
  return best
}

export interface Rankable {
  id: string
  title: string
  category?: string
  keywords?: string
}

export interface Ranked<T extends Rankable> {
  item: T
  /** Matched character indexes in item.title. */
  positions: number[]
}

/**
 * Orders palette entries. Empty query: recently used first, then the rest in their
 * given order. Otherwise only matches, best first; the title counts most, then
 * "category title", then keywords, with a small boost for recent use.
 */
export function rankCommands<T extends Rankable>(items: readonly T[], query: string, recent: readonly string[]): Ranked<T>[] {
  const recency = (id: string) => {
    const i = recent.indexOf(id)
    return i < 0 ? 0 : Math.max(0.5, 3 - i * 0.5)
  }
  if (!query.trim()) {
    const byId = new Map(items.map((it) => [it.id, it]))
    const first = recent.map((id) => byId.get(id)).filter((it): it is T => !!it)
    const rest = items.filter((it) => !recent.includes(it.id))
    return [...first, ...rest].map((item) => ({ item, positions: [] }))
  }
  const scored: { item: T; positions: number[]; score: number; order: number }[] = []
  items.forEach((item, order) => {
    const best = bestMatch(item, query)
    if (best) scored.push({ item, positions: best.positions, score: best.score + recency(item.id), order })
  })
  // Ties go to the shorter title (the tighter match: "Find in File" before "Find in Files").
  scored.sort((a, b) => b.score - a.score || a.item.title.length - b.item.title.length || a.order - b.order)
  return scored.map(({ item, positions }) => ({ item, positions }))
}

function bestMatch(item: Rankable, query: string): FuzzyMatch | null {
  const candidates: FuzzyMatch[] = []
  const title = fuzzyMatch(query, item.title)
  if (title) candidates.push(title)
  if (item.category) {
    const prefix = item.category.length + 1
    const both = fuzzyMatch(query, `${item.category} ${item.title}`)
    if (both) {
      const positions = both.positions.filter((p) => p >= prefix).map((p) => p - prefix)
      candidates.push({ score: both.score - 2, positions })
    }
  }
  if (item.keywords) {
    const kw = fuzzyMatch(query, item.keywords)
    if (kw) candidates.push({ score: kw.score - 4, positions: [] })
  }
  return candidates.reduce<FuzzyMatch | null>((a, b) => (!a || b.score > a.score ? b : a), null)
}

/** Moves `id` to the front of the most-recently-used list. */
export function pushRecent(recent: readonly string[], id: string, max = 8): string[] {
  return [id, ...recent.filter((x) => x !== id)].slice(0, max)
}
