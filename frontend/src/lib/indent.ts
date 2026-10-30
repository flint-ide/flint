import { EditorState, type Extension } from '@codemirror/state'
import { indentUnit } from '@codemirror/language'

const SAMPLE_LINES = 2000

// Languages whose formatter mandates tabs, whatever the file currently contains.
const TAB_LANGUAGES = new Set(['go', 'mod', 'work'])
const MAKEFILES = new Set(['Makefile', 'makefile', 'GNUmakefile'])

/**
 * Detects the indentation unit of a file: "\t" or a run of spaces (2, 4, 8).
 * Lines indented with tabs vs spaces are counted; for spaces the most common
 * indentation step between consecutive lines wins. Falls back to `fallback`.
 */
export function detectIndent(text: string, path = '', fallback = '  '): string {
  const base = path.split('/').pop() ?? ''
  const ext = base.includes('.') ? base.split('.').pop()!.toLowerCase() : ''
  if (TAB_LANGUAGES.has(ext) || MAKEFILES.has(base)) return '\t'

  let tabs = 0
  let spaceLines = 0
  const steps = new Map<number, number>()
  let prev = 0
  const lines = text.split('\n', SAMPLE_LINES)
  for (const line of lines) {
    if (!line.trim()) continue
    if (line[0] === '\t') {
      tabs++
      continue
    }
    const width = line.length - line.trimStart().length
    if (width > 0) spaceLines++
    const step = Math.abs(width - prev)
    if (step >= 2 && step <= 8) steps.set(step, (steps.get(step) ?? 0) + 1)
    prev = width
  }
  if (tabs > spaceLines) return '\t'
  if (spaceLines === 0) return fallback
  let best = 0
  let bestCount = 0
  for (const [step, count] of steps) {
    if (count > bestCount || (count === bestCount && step < best)) {
      best = step
      bestCount = count
    }
  }
  return best ? ' '.repeat(best) : fallback
}

/** Editor extensions so Enter / Tab keep the file's own indentation style. */
export function indentationFor(text: string, path: string): Extension {
  const unit = detectIndent(text, path)
  return [indentUnit.of(unit), EditorState.tabSize.of(unit === '\t' ? 4 : unit.length)]
}
