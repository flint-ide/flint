// Small fuzzy path matcher for quick-open (replaces a third-party dependency).
// A query matches when its characters appear in order; the score rewards matches
// at word boundaries, consecutive runs and matches inside the file name.

export interface FuzzyResult {
  target: string
  score: number
  /** Indices of matched characters in target, for highlighting. */
  indices: number[]
}

const SEPARATORS = new Set(['/', '\\', '.', '_', '-', ' '])

export interface Prepared {
  target: string
  lower: string
  nameStart: number // index where the base name begins
}

export function prepare(target: string): Prepared {
  return { target, lower: target.toLowerCase(), nameStart: target.lastIndexOf('/') + 1 }
}

/** Scores one prepared target; null when the query isn't a subsequence. */
export function score(query: string, p: Prepared): FuzzyResult | null {
  const q = query.toLowerCase()
  if (!q) return { target: p.target, score: 0, indices: [] }
  // Prefer matching inside the base name: try from the name first, then the full path.
  return matchFrom(query, q, p, p.nameStart) ?? (p.nameStart > 0 ? matchFrom(query, q, p, 0) : null)
}

function matchFrom(query: string, q: string, p: Prepared, start: number): FuzzyResult | null {
  const { target, lower, nameStart } = p
  const indices: number[] = []
  let total = 0
  let prev = -2
  let ti = start
  for (let qi = 0; qi < q.length; qi++) {
    const ch = q[qi]
    // Greedy, but jump to a later word-boundary occurrence when it exists nearby.
    let found = lower.indexOf(ch, ti)
    if (found < 0) return null
    const boundary = nextBoundary(target, lower, ch, found)
    if (boundary >= 0 && boundary !== found && boundary - found < 16 && found !== prev + 1) found = boundary
    let s = 1
    if (found === prev + 1) s += 5 // consecutive run
    if (isBoundary(target, found)) s += 8
    if (found >= nameStart) s += 3
    if (target[found] === query[qi]) s += 1 // exact case
    total += s
    indices.push(found)
    prev = found
    ti = found + 1
  }
  // Shorter paths and earlier first matches rank higher.
  total -= target.length * 0.05 + (indices[0] - start) * 0.1
  return { target, score: total, indices }
}

function isBoundary(target: string, i: number): boolean {
  if (i === 0) return true
  const before = target[i - 1]
  if (SEPARATORS.has(before)) return true
  const c = target[i]
  return c >= 'A' && c <= 'Z' && before >= 'a' && before <= 'z' // camelCase hump
}

function nextBoundary(target: string, lower: string, ch: string, from: number): number {
  for (let i = lower.indexOf(ch, from); i >= 0; i = lower.indexOf(ch, i + 1)) {
    if (isBoundary(target, i)) return i
  }
  return -1
}

/** Returns the best `limit` matches, highest score first. */
export function search(query: string, targets: Prepared[], limit: number): FuzzyResult[] {
  const results: FuzzyResult[] = []
  for (const p of targets) {
    const r = score(query, p)
    if (r) results.push(r)
  }
  results.sort((a, b) => b.score - a.score || a.target.length - b.target.length)
  return results.slice(0, limit)
}

/** Wraps matched characters in <b>…</b>, escaping everything else. */
export function highlight(r: FuzzyResult): string {
  const marked = new Set(r.indices)
  let out = ''
  let open = false
  for (let i = 0; i < r.target.length; i++) {
    const hit = marked.has(i)
    if (hit && !open) out += '<b>'
    if (!hit && open) out += '</b>'
    open = hit
    out += escapeHtml(r.target[i])
  }
  return open ? out + '</b>' : out
}

function escapeHtml(c: string): string {
  return c === '&' ? '&amp;' : c === '<' ? '&lt;' : c === '>' ? '&gt;' : c === '"' ? '&quot;' : c
}
