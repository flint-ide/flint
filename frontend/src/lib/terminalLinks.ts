// Clickable http(s) URLs in the terminal (what @xterm/addon-web-links did),
// including URLs soft-wrapped across rows, e.g. long login links printed by agents.
import type { IBufferCell, ILink, Terminal } from '@xterm/xterm'

const URL_RE = /(https?|HTTPS?):[/]{2}[^\s"'!*(){}|\\^<>`]*[^\s"':,.!?{}|\\^~[\]`()<>]/g
const MAX_LOGICAL_LINE = 4096

export interface UrlMatch {
  start: number
  end: number // exclusive
  url: string
}

export function findUrls(text: string): UrlMatch[] {
  const out: UrlMatch[] = []
  for (const m of text.matchAll(URL_RE)) {
    if (isUrl(m[0])) out.push({ start: m.index, end: m.index + m[0].length, url: m[0] })
  }
  return out
}

function isUrl(s: string): boolean {
  try {
    const u = new URL(s)
    return s.toLowerCase().startsWith(`${u.protocol}//`.toLowerCase())
  } catch {
    return false
  }
}

/** One buffer cell: its text and how many columns it spans (0 = right half of a wide char). */
export interface Cell {
  chars: string
  width: number
}

/** 1-based buffer position of the cell a string unit came from. */
export interface CellPos {
  x: number
  y: number
  width: number
}

/**
 * Joins rows of cells into one string and records, for every UTF-16 unit, the cell
 * it came from. Wide (CJK, emoji) and multi-unit characters keep links aligned
 * with what is drawn. Trailing blanks of the last row are dropped.
 */
export function layoutLine(rows: Cell[][], firstRow: number): { text: string; cells: CellPos[] } {
  let text = ''
  const cells: CellPos[] = []
  rows.forEach((row, r) => {
    row.forEach((cell, x) => {
      if (cell.width === 0) return
      const chars = cell.chars || ' '
      text += chars
      for (let i = 0; i < chars.length; i++) cells.push({ x: x + 1, y: firstRow + r + 1, width: cell.width })
    })
  })
  const trimmed = text.trimEnd()
  return { text: trimmed, cells: cells.slice(0, trimmed.length) }
}

function readRow(term: Terminal, row: number): Cell[] {
  const line = term.buffer.active.getLine(row)
  if (!line) return []
  const cells: Cell[] = []
  let reuse: IBufferCell | undefined
  for (let x = 0; x < line.length; x++) {
    reuse = line.getCell(x, reuse)
    cells.push({ chars: reuse?.getChars() ?? '', width: reuse?.getWidth() ?? 1 })
  }
  return cells
}

/** The soft-wrapped rows around `row` (0-based), laid out as one string. */
function logicalLine(term: Terminal, row: number) {
  const buffer = term.buffer.active
  let first = row
  while (first > 0 && buffer.getLine(first)?.isWrapped) first--
  const rows: Cell[][] = []
  for (let r = first, units = 0; buffer.getLine(r) && units <= MAX_LOGICAL_LINE; r++) {
    rows.push(readRow(term, r))
    units += term.cols
    if (!buffer.getLine(r + 1)?.isWrapped) break
  }
  return layoutLine(rows, first)
}

export function registerUrlLinks(term: Terminal, open: (url: string) => void) {
  return term.registerLinkProvider({
    provideLinks: (lineNo, callback) => {
      const { text, cells } = logicalLine(term, lineNo - 1)
      const links: ILink[] = findUrls(text).map((m) => {
        const start = cells[m.start]
        const end = cells[m.end - 1]
        return {
          range: { start: { x: start.x, y: start.y }, end: { x: end.x + end.width - 1, y: end.y } },
          text: m.url,
          decorations: { underline: true, pointerCursor: true },
          activate: (e: MouseEvent) => e.metaKey && open(m.url),
        }
      })
      callback(links)
    },
  })
}
