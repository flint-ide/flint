// Breakpoint bookkeeping, pure: lines are 1-based, kept sorted and unique per file.

export type BreakpointMap = Record<string, number[]>

/** Adds or removes a breakpoint on line. Files left without any drop out. */
export function toggleLine(map: BreakpointMap, path: string, line: number): BreakpointMap {
  const lines = map[path] ?? []
  const next = lines.includes(line) ? lines.filter((l) => l !== line) : [...lines, line].sort((a, b) => a - b)
  return withLines(map, path, next)
}

/** Replaces a file's lines (e.g. moved by edits); unchanged input keeps the same map. */
export function withLines(map: BreakpointMap, path: string, lines: number[]): BreakpointMap {
  const next = [...new Set(lines.filter((l) => Number.isInteger(l) && l > 0))].sort((a, b) => a - b)
  if (sameLines(map[path] ?? [], next)) return map
  if (next.length === 0) {
    const { [path]: _gone, ...rest } = map
    return rest
  }
  return { ...map, [path]: next }
}

export function sameLines(a: readonly number[], b: readonly number[]): boolean {
  return a.length === b.length && a.every((l, i) => l === b[i])
}

/** Validates breakpoints read back from a saved session. */
export function parseBreakpoints(raw: unknown): BreakpointMap {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {}
  let map: BreakpointMap = {}
  for (const [path, lines] of Object.entries(raw)) {
    if (path.startsWith('/') && Array.isArray(lines)) map = withLines(map, path, lines.filter((l): l is number => typeof l === 'number'))
  }
  return map
}

/** Breakpoints of the files inside root (a session saves its own project's only). */
export function breakpointsUnder(map: BreakpointMap, root: string): BreakpointMap {
  const prefix = root.endsWith('/') ? root : `${root}/`
  return Object.fromEntries(Object.entries(map).filter(([path]) => path.startsWith(prefix)))
}
