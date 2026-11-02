export interface Statement {
  from: number
  to: number
  sql: string
}

/**
 * Splits SQL into statements on `;`, ignoring semicolons inside quotes,
 * comments and Postgres dollar-quoted bodies. Offsets index into `text`.
 */
export function splitStatements(text: string): Statement[] {
  const statements: Statement[] = []
  let start = 0
  let i = 0

  const push = (end: number) => {
    const raw = text.slice(start, end)
    const lead = raw.length - raw.trimStart().length
    const sql = raw.trim()
    if (sql && !isOnlyComments(sql)) {
      statements.push({ from: start + lead, to: start + lead + sql.length, sql })
    }
  }

  while (i < text.length) {
    const ch = text[i]
    const next = text[i + 1]
    if (ch === '-' && next === '-') {
      i = skipUntil(text, i + 2, '\n')
    } else if (ch === '/' && next === '*') {
      i = skipUntil(text, i + 2, '*/')
    } else if (ch === "'" || ch === '"' || ch === '`') {
      i = skipQuoted(text, i + 1, ch)
    } else if (ch === '$') {
      const tag = dollarTag(text, i)
      i = tag ? skipUntil(text, i + tag.length, tag) : i + 1
    } else if (ch === ';') {
      push(i)
      start = i + 1
      i++
    } else {
      i++
    }
  }
  push(text.length)
  return statements
}

/**
 * Returns the statement containing `pos`. A cursor right after a `;` or on a blank
 * line between statements resolves to the nearest preceding statement.
 */
export function statementAt(text: string, pos: number): Statement | null {
  const all = splitStatements(text)
  if (all.length === 0) return null
  let best: Statement | null = null
  for (const st of all) {
    if (pos >= st.from && pos <= st.to + 1) return st
    if (st.to < pos) best = st
  }
  return best ?? all[0]
}

function skipUntil(text: string, from: number, end: string): number {
  const idx = text.indexOf(end, from)
  return idx < 0 ? text.length : idx + end.length
}

function skipQuoted(text: string, from: number, quote: string): number {
  let i = from
  while (i < text.length) {
    if (text[i] === '\\' && quote !== '"') {
      i += 2
      continue
    }
    if (text[i] === quote) {
      if (text[i + 1] === quote) {
        i += 2 // doubled quote escape
        continue
      }
      return i + 1
    }
    i++
  }
  return text.length
}

/** Matches $$ or $tag$ at position i (Postgres dollar quoting). */
function dollarTag(text: string, i: number): string | null {
  const m = /^\$[A-Za-z_]?[A-Za-z0-9_]*\$/.exec(text.slice(i, i + 64))
  return m ? m[0] : null
}

function isOnlyComments(sql: string): boolean {
  return sql.replace(/--[^\n]*/g, '').replace(/\/\*[\s\S]*?\*\//g, '').trim() === ''
}
