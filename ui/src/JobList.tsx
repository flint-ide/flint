import { Spinner } from '@wunderui/react'
import { CheckCircle2, CircleDashed, MinusCircle, XCircle } from 'lucide-react'
import type { ReactElement } from 'react'

export type JobState = 'success' | 'failure' | 'running' | 'queued' | 'skipped'

export type Job = {
  id: string
  name: string
  state: JobState
  /** Free text on the right: a duration, a time, a count. */
  meta?: string
}

const ICONS: Record<JobState, ReactElement> = {
  success: <CheckCircle2 className="fui-ok" aria-label="succeeded" />,
  failure: <XCircle className="fui-danger" aria-label="failed" />,
  running: <Spinner aria-label="running" />,
  queued: <CircleDashed className="fui-muted" aria-label="queued" />,
  skipped: <MinusCircle className="fui-muted" aria-label="skipped" />,
}

/** CI jobs, pods, tasks: anything with a name and a run state. */
export function JobList({ jobs, onSelect }: { jobs: readonly Job[]; onSelect?: (job: Job) => void }) {
  return (
    <ul className="fui-joblist">
      {jobs.map((job) => (
        <li key={job.id} className="fui-job" data-clickable={onSelect ? true : undefined} onClick={onSelect && (() => onSelect(job))}>
          <span className="fui-job-icon">{ICONS[job.state]}</span>
          <span className="fui-grow fui-ellipsis">{job.name}</span>
          {job.meta && <span className="fui-muted fui-mono">{job.meta}</span>}
        </li>
      ))}
    </ul>
  )
}
