// The text of "finished while you were away" notifications, pure so it is testable.
import type { TestCounts } from './runTree'

export interface RunOutcome {
  kind: 'config' | 'tests'
  exit: { code: number; stopped: boolean; elapsedMs: number; error?: string }
  tests: TestCounts
}

const seconds = (ms: number) => (ms < 10_000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms / 1000)} s`)

/** The notification body for a finished run, or null when there is nothing to tell (stopped by the user). */
export function runSummary({ kind, exit, tests }: RunOutcome): string | null {
  if (exit.stopped) return null
  if (exit.error) return `Failed: ${exit.error}`
  if (kind === 'tests' && tests.total > 0) {
    return tests.failed > 0 ? `${tests.failed} of ${tests.total} tests failed` : `${tests.passed} tests passed in ${seconds(exit.elapsedMs)}`
  }
  return exit.code === 0 ? `Finished in ${seconds(exit.elapsedMs)}` : `Exited with code ${exit.code} after ${seconds(exit.elapsedMs)}`
}
