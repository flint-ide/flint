// The tool check's grouping, labels and summary, pure so they are testable.
// Go decides what is installed (internal/tools); this only arranges it.
import type { tools } from '../wails/models'

export type ToolStatus = tools.Status
type Row = Pick<ToolStatus, 'name' | 'group' | 'required' | 'state' | 'binary' | 'binaries' | 'version'>

export type Tone = 'ok' | 'warn' | 'bad' | 'muted'

/** Groups in display order; Go's tools.Group* constants. */
const GROUPS: [id: string, title: string][] = [
  ['core', 'Essentials'],
  ['agents', 'AI agents'],
  ['languages', 'Languages and debugging'],
  ['infra', 'Containers and cloud'],
]

export interface ToolGroup<T> {
  id: string
  title: string
  tools: T[]
}

/** Installed is fine; a missing optional tool is just not used, a missing required one is a problem. */
export function toolTone(t: Pick<Row, 'state' | 'required'>): Tone {
  if (t.state === 'ok') return 'ok'
  if (t.state === 'error') return 'warn'
  return t.required ? 'bad' : 'muted'
}

/** "2.43.0", "vtsls 0.2.3" (which alternative was found), "Installed", "Not working", "Not installed". */
export function statusLabel(t: Pick<Row, 'state' | 'binary' | 'binaries' | 'version'>): string {
  if (t.state === 'missing') return 'Not installed'
  if (t.state === 'error') return 'Not working'
  if (t.binaries.length > 1) return `${t.binary} ${t.version}`.trim()
  return t.version || 'Installed'
}

/** "vtsls or typescript-language-server", "a, b or c". */
export function lookedFor(binaries: string[]): string {
  if (binaries.length < 2) return binaries[0] ?? ''
  return `${binaries.slice(0, -1).join(', ')} or ${binaries[binaries.length - 1]}`
}

/** Problems first (a missing required tool, then broken ones); otherwise Go's order. */
const rank = (t: Pick<Row, 'state' | 'required'>) => (t.state === 'missing' && t.required ? 0 : t.state === 'error' ? 1 : 2)

/** Tools by group, in display order; empty groups are left out, unknown ones come last. */
export function groupTools<T extends Pick<Row, 'group' | 'state' | 'required'>>(list: T[]): ToolGroup<T>[] {
  const known = new Set(GROUPS.map(([id]) => id))
  const groups: [string, string][] = [...GROUPS, ['', 'Other']]
  return groups
    .map(([id, title]) => ({
      id: id || 'other',
      title,
      tools: list.filter((t) => (id ? t.group === id : !known.has(t.group))).sort((a, b) => rank(a) - rank(b)),
    }))
    .filter((g) => g.tools.length > 0)
}

export interface ToolSummary {
  installed: number
  total: number
  tone: Tone
  text: string
}

/** One line for the footer: how many are installed and what needs attention. */
export function summarize(list: Pick<Row, 'name' | 'state' | 'required'>[]): ToolSummary {
  const installed = list.filter((t) => t.state === 'ok').length
  const total = list.length
  const missingRequired = list.filter((t) => t.required && t.state === 'missing').map((t) => t.name)
  const broken = list.filter((t) => t.state === 'error').map((t) => t.name)
  const parts = [installed === total ? `All ${total} tools installed` : `${installed} of ${total} installed`]
  if (missingRequired.length) parts.push(`${missingRequired.join(', ')} required`)
  if (broken.length) parts.push(`${broken.join(', ')} not working`)
  const tone: Tone = missingRequired.length ? 'bad' : broken.length ? 'warn' : 'ok'
  return { installed, total, tone, text: parts.join(' · ') }
}
