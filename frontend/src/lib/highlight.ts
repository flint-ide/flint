// Renders quick-open matches; the ranking itself runs in Go (internal/fuzzy).

export interface FileMatch {
  path: string
  /** Matched characters as code-point indices (Array.from(path)). */
  indices: number[]
  /** Workspace folder of the file (set by the backend). */
  root?: string
  /** Code points of path before the part relative to root ("label/" with several roots). */
  offset?: number
}

/** Absolute path of a quick-open match. */
export function matchPath(m: FileMatch, fallbackRoot: string): string {
  const rel = m.offset ? Array.from(m.path).slice(m.offset).join('') : m.path
  return `${m.root || fallbackRoot}/${rel}`
}

/** Wraps matched characters in <b>…</b>, escaping everything else. */
export function highlight(m: FileMatch): string {
  const marked = new Set(m.indices)
  let out = ''
  let open = false
  Array.from(m.path).forEach((ch, i) => {
    const hit = marked.has(i)
    if (hit && !open) out += '<b>'
    if (!hit && open) out += '</b>'
    open = hit
    out += escapeHtml(ch)
  })
  return open ? out + '</b>' : out
}

function escapeHtml(c: string): string {
  return c === '&' ? '&amp;' : c === '<' ? '&lt;' : c === '>' ? '&gt;' : c === '"' ? '&quot;' : c
}
