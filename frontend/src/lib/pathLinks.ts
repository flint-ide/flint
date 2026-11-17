export interface PathLink {
  start: number // index in the line
  end: number
  path: string
  line?: number
  column?: number
}

// Paths like `src/app.ts:42:7`, `./main.go:10`, `/abs/file.sql` or `internal/db/store.go`.
// A path needs a slash or an extension so plain words never become links.
const PATH_RE = /(?:^|[\s'"`(\[<])((?:\.{0,2}\/)?(?:[\w@.+-]+\/)*[\w@+-][\w@.+-]*\.[A-Za-z0-9]{1,10}|(?:\.{0,2}\/)?(?:[\w@.+-]+\/)+[\w@.+-]+)(?::(\d+))?(?::(\d+))?/g

/** Finds file-path references (optionally with :line[:col]) in a line of terminal output. */
export function findPathLinks(text: string): PathLink[] {
  const links: PathLink[] = []
  for (const m of text.matchAll(PATH_RE)) {
    const path = m[1]
    if (/^v?\d+(\.\d+)+$/.test(path) || path.includes('://')) continue // versions, URLs
    const start = (m.index ?? 0) + m[0].indexOf(path)
    const suffix = m[0].slice(m[0].indexOf(path) + path.length)
    links.push({
      start,
      end: start + path.length + suffix.length,
      path: path.replace(/[.,:;]+$/, ''),
      line: m[2] ? Number(m[2]) : undefined,
      column: m[3] ? Number(m[3]) : undefined,
    })
  }
  return links
}

/** Resolves a link path against the project root. */
export function resolvePath(root: string, path: string): string {
  if (path.startsWith('/')) return path
  const parts = `${root}/${path}`.split('/')
  const out: string[] = []
  for (const p of parts) {
    if (p === '' || p === '.') continue
    if (p === '..') out.pop()
    else out.push(p)
  }
  return `/${out.join('/')}`
}

const trimSlash = (p: string) => (p.length > 1 ? p.replace(/\/+$/, '') : p)

/**
 * Folders a relative path may be relative to, most likely first, without empty
 * entries and repeats (a trailing slash does not make a folder different).
 */
export function linkBases(bases: readonly (string | null | undefined)[]): string[] {
  const seen = new Set<string>()
  return bases.filter((b): b is string => {
    if (!b) return false
    const key = trimSlash(b)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

/** The files a link may mean: an absolute path as it is, a relative one under each base. */
export function linkCandidates(bases: readonly string[], path: string): string[] {
  if (path.startsWith('/')) return [path]
  return [...new Set(bases.map((b) => resolvePath(b, path)))]
}

/** The first candidate that exists; a check that throws counts as "not here". */
export async function firstExisting(candidates: readonly string[], exists: (path: string) => Promise<boolean>): Promise<string | null> {
  for (const c of candidates) {
    if (await exists(c).catch(() => false)) return c
  }
  return null
}
