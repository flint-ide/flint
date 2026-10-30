// Which files get a preview, and the small path/heading helpers the previews use.

const IMAGE_EXT = /\.(png|jpe?g|gif|webp|svg|ico|bmp)$/i
const MARKDOWN_EXT = /\.(md|markdown|mdx)$/i

export const isImage = (path: string) => IMAGE_EXT.test(path)
export const isMarkdown = (path: string) => MARKDOWN_EXT.test(path)

/** Resolves a link or image reference of a file to an absolute path; null for URLs, anchors and mail links. */
export function resolveRelative(fromFile: string, ref: string): string | null {
  if (!ref || ref.startsWith('#') || /^[a-z][a-z0-9+.-]*:/i.test(ref) || ref.startsWith('//')) return null
  const clean = decodeURIComponent(ref.split(/[?#]/)[0])
  if (!clean) return null
  const parts = (clean.startsWith('/') ? clean : `${fromFile.slice(0, fromFile.lastIndexOf('/'))}/${clean}`).split('/')
  const out: string[] = []
  for (const p of parts) {
    if (p === '' || p === '.') continue
    if (p === '..') out.pop()
    else out.push(p)
  }
  return `/${out.join('/')}`
}

/**
 * 1-based source lines of the Markdown headings (ATX `#` and setext `===`/`---`),
 * skipping fenced code; their order matches the rendered <h1>…<h6> elements.
 */
export function headingLines(source: string): number[] {
  const lines = source.split('\n')
  const out: number[] = []
  let fence: string | null = null
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const f = /^\s{0,3}(`{3,}|~{3,})/.exec(line)
    if (f) {
      if (!fence) fence = f[1][0]
      else if (f[1][0] === fence) fence = null
      continue
    }
    if (fence) continue
    if (/^\s{0,3}#{1,6}(\s|$)/.test(line)) out.push(i + 1)
    else if (i > 0 && /^\s{0,3}(=+|-+)\s*$/.test(line) && lines[i - 1].trim() && !/^\s{0,3}([-*+]|#)/.test(lines[i - 1]) && !/^\s*$/.test(lines[i - 1])) {
      // A setext underline; "---" after a blank line is a thematic break, not a heading.
      if (/^\s{0,3}-+\s*$/.test(line) && /^\s{0,3}(\||[-*+]\s)/.test(lines[i - 1])) continue
      out.push(i)
    }
  }
  return out
}

/** Index of the last heading at or above line (-1 when none). */
export function headingAt(headings: readonly number[], line: number): number {
  let lo = 0
  let hi = headings.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (headings[mid] <= line) {
      found = mid
      lo = mid + 1
    } else hi = mid - 1
  }
  return found
}
