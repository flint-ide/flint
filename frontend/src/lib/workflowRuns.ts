// What the Projects and Workflows features share about a workflow run: the words and colours of its state, and
// the id of the run tab. Features must not import each other, so this lives here (pure, no DOM, unit tested).

/** The run tab's view id (the Workflows feature's TabKind); Projects opens that tab by this id. */
export const RUN_VIEW = 'workflow-run'

/** The kit Badge colors the run states use. */
export type RunTone = 'blue' | 'indigo' | 'red' | 'yellow' | 'green' | 'grey'

/** A run state in the user's words. `queued` is a running run that waits for a free slot. */
export function runStateText(state: string, queued = false): string {
  if (queued && state === 'running') return 'Queued'
  switch (state) {
    case 'running': return 'Running'
    case 'waiting': return 'Waiting for you'
    case 'failed': return 'Failed'
    case 'paused': return 'Paused'
    case 'done': return 'Done'
    case 'stopped': return 'Stopped'
    default: return state || 'Unknown'
  }
}

export function runStateTone(state: string, queued = false): RunTone {
  if (queued && state === 'running') return 'yellow'
  switch (state) {
    case 'running': return 'blue'
    case 'waiting': return 'indigo'
    case 'failed': return 'red'
    case 'paused': return 'yellow'
    case 'done': return 'green'
    default: return 'grey'
  }
}

/** The data of a run tab, as the Workflows feature reads it back (runIdOf). */
export const runTabData = (runId: string): { runId: string } => ({ runId })
