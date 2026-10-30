// Test files and marks, without CodeMirror: stores and the title bar use these
// at startup, while the gutter itself loads with the editor.
import type { TestStatus } from './runTree'

/** A runnable spot (testrun.Mark): the file itself, a suite or a test. */
export interface TestMark {
  line: number
  kind: 'file' | 'suite' | 'test' | 'subtest'
  /** Full test name; '' for the file mark. */
  name: string
}

export interface TestGutterHooks {
  /** Runs a mark; alt: the wider scope (the whole package for the file mark). */
  run: (path: string, mark: TestMark, alt: boolean) => void
  status: (path: string, name: string) => TestStatus | undefined
  /** Calls back when results change; returns the unsubscribe. */
  onResults: (listener: () => void) => () => void
  /** ⇧-click: debug the mark instead (when the language has a debugger). */
  debug?: (path: string, mark: TestMark) => void
  /** Right click: a menu of what can be done with the mark (run, debug, profile). */
  menu?: (path: string, mark: TestMark, x: number, y: number) => void
}

/** Mirrors testrun.Lang: which test runner a file belongs to. */
export function testLang(path: string): 'go' | 'js' | null {
  const base = path.slice(path.lastIndexOf('/') + 1)
  if (base.endsWith('_test.go')) return 'go'
  return /\.(test|spec)\.(ts|tsx|mts|cts|js|jsx|mjs|cjs)$/.test(base) ? 'js' : null
}
