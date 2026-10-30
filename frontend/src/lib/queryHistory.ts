// Formatting for the query history popup. Framework-free for node:test.

/** "just now", "5 min ago", "3 h ago", "yesterday 14:05", "2024-03-01 09:12". */
export function relativeTime(at: number, now: number): string {
  const diff = Math.max(0, now - at)
  if (diff < 45_000) return 'just now'
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)} min ago`
  const then = new Date(at)
  const today = new Date(now)
  const sameDay = then.toDateString() === today.toDateString()
  if (sameDay) return `${Math.floor(diff / 3_600_000)} h ago`
  const hhmm = `${pad(then.getHours())}:${pad(then.getMinutes())}`
  const yesterday = new Date(now - 86_400_000).toDateString() === then.toDateString()
  if (yesterday) return `yesterday ${hhmm}`
  return `${then.getFullYear()}-${pad(then.getMonth() + 1)}-${pad(then.getDate())} ${hhmm}`
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** Badge for entries that weren't plain console runs. */
export function historyKindLabel(kind: string | undefined, driver: string): string {
  switch (kind) {
    case 'explain':
      return 'EXPLAIN'
    case 'explain-analyze':
      return driver === 'clickhouse' ? 'PIPELINE' : 'ANALYZE'
    case 'edit':
      return 'EDIT'
  }
  return ''
}

/** One line of SQL for the list, whitespace collapsed and cut at max characters. */
export function sqlPreview(sql: string, max = 200): string {
  const flat = sql.replace(/\s+/g, ' ').trim()
  return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat
}
