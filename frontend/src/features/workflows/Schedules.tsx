import { useWorkflows, openRun, openWorkflowSettings } from './store'
import type { ScheduleView } from '../../../bindings/flint/internal/features/workflows/models'
import { SCHEDULE_CHANGED_NOTE, scheduleLines, scheduleWhen } from './workflows'
import { ReviewButton } from './ScheduleReview'
import { SectionCount } from './StepParts'
import { Button, TaskCard, TreeRow } from '@flint-ide/ui'

const basename = (p: string) => p.replace(/\/+$/, '').slice(p.replace(/\/+$/, '').lastIndexOf('/') + 1)

/** The schedules (Settings ▸ Workflows) with their next start. Nothing is drawn while there are none. */
export function Schedules() {
  const schedules = useWorkflows((s) => s.schedules)
  if (!schedules || schedules.length === 0) return null
  return (
    <div className="panel-section-divided">
      <TreeRow emphasis="group" label="Schedules" badge={<SectionCount n={schedules.length} />} />
      {schedules.map((s) => <ScheduleRow key={s.name} schedule={s} />)}
      <div className="placeholder muted">
        <span>
          <Button variant="link" size="sm" onClick={openWorkflowSettings}>Edit schedules…</Button>
        </span>
      </div>
    </div>
  )
}

function ScheduleRow({ schedule }: { schedule: ScheduleView }) {
  const [next, ...rest] = scheduleLines(schedule)
  return (
    <TaskCard
      tooltip={`${schedule.task}\n${schedule.folder}`}
      label={schedule.name}
      meta={[schedule.workflow, scheduleWhen(schedule), basename(schedule.folder)]}
      notes={[
        schedule.changed && { text: SCHEDULE_CHANGED_NOTE, tone: 'warn' },
        { text: next, tone: schedule.next > 0 ? undefined : 'warn' },
        ...rest.map((text) => ({ text })),
      ]}
      actions={
        <>
          {schedule.changed && <ReviewButton schedule={schedule} />}
          {schedule.lastRun && (
            <Button variant="ghost" size="sm" onClick={() => openRun(schedule.lastRun)} title="Open the run it started last">Last run</Button>
          )}
        </>
      }
    />
  )
}
