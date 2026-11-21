import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  ActionIconsCell,
  ActionItemsCell,
  BadgeCell,
  ButtonCell,
  CTAButtonCell,
  CellSelect,
  CellSwitch,
  CheckItemCell,
  DragItemCell,
  HeadingCell,
  IconButtonTextCell,
  IconTextCell,
  PerformanceCell,
  ProgressCell,
  RadioItemCell,
  SwitchItemCell,
  TABLE_ACTION_ICONS,
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
  TagCell,
  TextCell,
  UserCell,
  type BadgeColor,
} from '@wunderui/react'
import { Database, FileCode, GitBranch, GitPullRequest, Play, RefreshCw, Square, Terminal } from 'lucide-react'
import { useState } from 'react'

type RunStatus = 'passed' | 'failed' | 'running' | 'queued'

const STATUS_COLOR: Record<RunStatus, BadgeColor> = {
  passed: 'green',
  failed: 'red',
  running: 'yellow',
  queued: 'grey',
}

const CI_RUNS = [
  { id: 4182, workflow: 'go test ./...', branch: 'main', status: 'passed', progress: 100, author: 'Ada Lovelace', initials: 'AL', duration: '3m 12s' },
  { id: 4181, workflow: 'frontend: vitest', branch: 'feat/ui-kit', status: 'running', progress: 64, author: 'Grace Hopper', initials: 'GH', duration: '1m 48s' },
  { id: 4180, workflow: 'golangci-lint', branch: 'fix/symlink-review', status: 'failed', progress: 100, author: 'Rob Pike', initials: 'RP', duration: '0m 52s' },
  { id: 4179, workflow: 'pages: build storybook', branch: 'feat/ui-kit', status: 'queued', progress: 0, author: 'Ken Thompson', initials: 'KT', duration: '-' },
] as const satisfies readonly {
  id: number
  workflow: string
  branch: string
  status: RunStatus
  progress: number
  author: string
  initials: string
  duration: string
}[]

const meta = {
  title: 'Data Display/Table',
  component: Table,
} satisfies Meta<typeof Table>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <Table {...args}>
      <TableCaption>Tables in the flint database</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead>Table</TableHead>
          <TableHead>Rows</TableHead>
          <TableHead style={{ textAlign: 'right' }}>Size</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {[
          ['workspaces', '1,204', '3.2 MB'],
          ['agent_sessions', '48,917', '412 MB'],
          ['review_drafts', '6,330', '28 MB'],
          ['settings', '12', '16 kB'],
        ].map(([name, rows, size]) => (
          <TableRow key={name}>
            <TableCell style={{ fontFamily: 'var(--font-mono)' }}>{name}</TableCell>
            <TableCell>{rows}</TableCell>
            <TableCell style={{ textAlign: 'right' }}>{size}</TableCell>
          </TableRow>
        ))}
      </TableBody>
      <TableFooter>
        <TableRow>
          <TableCell colSpan={2}>Total</TableCell>
          <TableCell style={{ textAlign: 'right' }}>443 MB</TableCell>
        </TableRow>
      </TableFooter>
    </Table>
  ),
}

export const CIRuns: Story = {
  name: 'CI runs',
  render: () => (
    <Table>
      <TableCaption>Recent workflow runs</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead>
            <HeadingCell>Run</HeadingCell>
          </TableHead>
          <TableHead>
            <HeadingCell>Workflow</HeadingCell>
          </TableHead>
          <TableHead>
            <HeadingCell>Branch</HeadingCell>
          </TableHead>
          <TableHead>
            <HeadingCell>Status</HeadingCell>
          </TableHead>
          <TableHead>
            <HeadingCell>Progress</HeadingCell>
          </TableHead>
          <TableHead>
            <HeadingCell>Triggered by</HeadingCell>
          </TableHead>
          <TableHead>
            <HeadingCell>Duration</HeadingCell>
          </TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {CI_RUNS.map((run) => (
          <TableRow key={run.id}>
            <TableCell>
              <TextCell>#{run.id}</TextCell>
            </TableCell>
            <TableCell>
              <IconTextCell icon={<Terminal />}>{run.workflow}</IconTextCell>
            </TableCell>
            <TableCell>
              <IconTextCell icon={<GitBranch />}>{run.branch}</IconTextCell>
            </TableCell>
            <TableCell>
              <BadgeCell label={run.status} color={STATUS_COLOR[run.status]} badgeStyle="light" />
            </TableCell>
            <TableCell>
              <ProgressCell value={run.progress} />
            </TableCell>
            <TableCell>
              <UserCell name={run.author} initials={run.initials} avatarSize="sm" />
            </TableCell>
            <TableCell>
              <TextCell>{run.duration}</TextCell>
            </TableCell>
            <TableCell>
              <ActionIconsCell
                actions={[
                  { icon: <RefreshCw />, label: 'Re-run' },
                  { icon: <Square />, label: 'Cancel' },
                  { icon: TABLE_ACTION_ICONS.delete, label: 'Delete logs' },
                ]}
              />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  ),
}

const PULL_REQUESTS = [
  { id: 76, title: 'feat: Storybook for WunderUI', author: 'Ada Lovelace', email: 'ada@flint.dev', initials: 'AL', label: 'ui', color: 'indigo', diff: '+1,204', good: true },
  { id: 75, title: 'chore(deps): bump go minor group', author: 'dependabot', email: 'bot@github.com', initials: 'DB', label: 'deps', color: 'grey', diff: '+38', good: true },
  { id: 74, title: 'fix: review drafts through symlinks', author: 'Rob Pike', email: 'rob@flint.dev', initials: 'RP', label: 'bug', color: 'red', diff: '-212', good: false },
] as const satisfies readonly {
  id: number
  title: string
  author: string
  email: string
  initials: string
  label: string
  color: BadgeColor
  diff: string
  good: boolean
}[]

function PullRequestsTable() {
  const [selected, setSelected] = useState<ReadonlySet<number>>(new Set([76]))
  const [labels, setLabels] = useState<ReadonlySet<number>>(new Set(PULL_REQUESTS.map((pr) => pr.id)))

  const toggle = (id: number, checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })

  const removeLabel = (id: number) =>
    setLabels((prev) => new Set([...prev].filter((x) => x !== id)))

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>
            <CheckItemCell
              checked={selected.size === PULL_REQUESTS.length}
              onCheckedChange={(checked) => setSelected(new Set(checked ? PULL_REQUESTS.map((pr) => pr.id) : []))}
            >
              Pull request
            </CheckItemCell>
          </TableHead>
          <TableHead>Author</TableHead>
          <TableHead>Label</TableHead>
          <TableHead>Diff</TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {PULL_REQUESTS.map((pr) => (
          <TableRow key={pr.id} data-state={selected.has(pr.id) ? 'selected' : undefined}>
            <TableCell>
              <CheckItemCell checked={selected.has(pr.id)} onCheckedChange={(checked) => toggle(pr.id, checked)}>
                #{pr.id} {pr.title}
              </CheckItemCell>
            </TableCell>
            <TableCell>
              <UserCell name={pr.author} email={pr.email} initials={pr.initials} avatarSize="default" />
            </TableCell>
            <TableCell>
              {labels.has(pr.id) ? <TagCell label={pr.label} color={pr.color} onRemove={() => removeLabel(pr.id)} /> : null}
            </TableCell>
            <TableCell>
              <PerformanceCell value={pr.diff} delta={pr.good ? 'lines' : 'lines removed'} good={pr.good} />
            </TableCell>
            <TableCell>
              <div className="sb-row" style={{ flexWrap: 'nowrap' }}>
                <ButtonCell>Review</ButtonCell>
                <CTAButtonCell>Merge</CTAButtonCell>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
      <TableCaption>{selected.size} selected</TableCaption>
    </Table>
  )
}

export const PullRequests: Story = {
  name: 'Pull requests',
  render: () => <PullRequestsTable />,
}

const PODS = [
  { name: 'flint-api-7d9f8c6b5-x2kqz', restart: true, phase: 'Running' },
  { name: 'flint-worker-5c4b7d9f-mp8rw', restart: false, phase: 'Running' },
  { name: 'postgres-0', restart: false, phase: 'Pending' },
]

function InteractiveCellsTable() {
  const [autoRestart, setAutoRestart] = useState<Record<string, boolean>>(
    Object.fromEntries(PODS.map((p) => [p.name, p.restart])),
  )
  const [logLevel, setLogLevel] = useState<Record<string, string>>(Object.fromEntries(PODS.map((p) => [p.name, 'info'])))
  const [model, setModel] = useState('claude-code')
  const [watch, setWatch] = useState(true)

  return (
    <div className="sb-stack">
      <Table>
        <TableCaption>Kubernetes pods in namespace flint</TableCaption>
        <TableHeader>
          <TableRow>
            <TableHead>Pod</TableHead>
            <TableHead>Phase</TableHead>
            <TableHead>Auto-restart</TableHead>
            <TableHead>Log level</TableHead>
            <TableHead>Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {PODS.map((pod) => (
            <TableRow key={pod.name}>
              <TableCell>
                <DragItemCell>{pod.name}</DragItemCell>
              </TableCell>
              <TableCell>
                <BadgeCell label={pod.phase} color={pod.phase === 'Running' ? 'green' : 'yellow'} />
              </TableCell>
              <TableCell>
                <CellSwitch
                  aria-label={`Auto-restart ${pod.name}`}
                  checked={autoRestart[pod.name] ?? false}
                  onCheckedChange={(checked) => setAutoRestart((prev) => ({ ...prev, [pod.name]: checked }))}
                />
              </TableCell>
              <TableCell>
                <CellSelect
                  aria-label={`Log level for ${pod.name}`}
                  value={logLevel[pod.name] ?? 'info'}
                  options={['debug', 'info', 'warn', 'error']}
                  onValueChange={(value) => setLogLevel((prev) => ({ ...prev, [pod.name]: value }))}
                />
              </TableCell>
              <TableCell>
                <ActionItemsCell actions={[{ label: 'Logs' }, { label: 'Exec' }, { label: 'Delete', muted: true }]} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Agent</TableHead>
            <TableHead>Task</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow>
            <TableCell>
              <RadioItemCell value="claude-code" selected={model} onSelect={setModel}>
                Claude Code
              </RadioItemCell>
            </TableCell>
            <TableCell>
              <IconButtonTextCell icon={<Play />} label="Start task">
                Refactor settings store
              </IconButtonTextCell>
            </TableCell>
          </TableRow>
          <TableRow>
            <TableCell>
              <RadioItemCell value="codex" selected={model} onSelect={setModel}>
                Codex
              </RadioItemCell>
            </TableCell>
            <TableCell>
              <IconButtonTextCell icon={<GitPullRequest />} label="Open PR">
                Add FileTree keyboard tests
              </IconButtonTextCell>
            </TableCell>
          </TableRow>
          <TableRow>
            <TableCell>
              <SwitchItemCell checked={watch} onCheckedChange={setWatch}>
                Watch mode
              </SwitchItemCell>
            </TableCell>
            <TableCell>
              <IconTextCell icon={watch ? <FileCode /> : <Database />}>
                {watch ? 'Re-runs on file save' : 'Manual runs only'}
              </IconTextCell>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  )
}

export const InteractiveCells: Story = {
  name: 'Interactive cells',
  render: () => <InteractiveCellsTable />,
}
