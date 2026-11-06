// `func main` of a Go `package main` file: where the editor shows its ▶ (like
// IntelliJ's run marker), and which run configuration that marker starts.
// Pure, so it is testable; the gutter (mainGutter.ts) and store (mainRun.ts) use it.

/** A Go file that can hold a program's entry point (tests run from the test gutter instead). */
export function isGoProgramFile(path: string): boolean {
  return path.endsWith('.go') && !path.endsWith('_test.go')
}

const PACKAGE_MAIN = /^package\s+main\b/m
const FUNC_MAIN = /^func\s+main\s*\(\s*\)/m

/** 1-based line of `func main()` in a `package main` file, or null. */
export function goMainLine(content: string): number | null {
  if (!PACKAGE_MAIN.test(content)) return null
  const m = FUNC_MAIN.exec(content)
  if (!m) return null
  let line = 1
  for (let i = 0; i < m.index; i++) if (content.charCodeAt(i) === 10) line++
  return line
}

/** Minimal shape of a run configuration (runconfig.Config) for matching. */
export interface GoConfigLike {
  id: string
  name: string
  type: string
  package?: string
  cwd?: string
}

/** The folder of path relative to root ('' for root itself); null when path is outside root. */
export function relativeDir(root: string, path: string): string | null {
  const dir = path.slice(0, Math.max(path.lastIndexOf('/'), 0))
  if (dir === root) return ''
  return dir.startsWith(`${root}/`) ? dir.slice(root.length + 1) : null
}

/** The Go package path of a folder relative to the project, as configurations write it. */
export const goPackageOf = (rel: string) => (rel ? `./${rel}` : '.')

/**
 * The configuration ▶ next to `func main` runs: a saved or detected Go configuration
 * of the file's package when there is one, else a new one built in the file's folder
 * (`go build .` there finds the module even when it isn't at the project root).
 */
export function mainConfigFor<C extends GoConfigLike>(root: string, path: string, configs: readonly C[]): GoConfigLike | null {
  const rel = relativeDir(root, path)
  if (rel === null) return null
  const pkg = goPackageOf(rel)
  const existing = configs.find((c) => c.type === 'go' && !c.cwd && normalizePackage(c.package) === pkg)
  if (existing) return existing
  const dir = rel ? rel.slice(rel.lastIndexOf('/') + 1) : root.slice(root.lastIndexOf('/') + 1)
  return { id: `detected:go-main:${pkg}`, name: dir || 'main', type: 'go', package: '.', cwd: rel || undefined }
}

function normalizePackage(pkg: string | undefined): string {
  if (!pkg) return ''
  const trimmed = pkg.replace(/\/+$/, '')
  if (trimmed === '.' || trimmed === './') return '.'
  return trimmed.startsWith('./') ? trimmed : `./${trimmed}`
}
