import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  AgentRow,
  type AgentRowProps,
  Badge,
  Button,
  DiffStat,
  EmptyState,
  type Job,
  JobList,
  NativeSelect,
  NativeSelectContent,
  NativeSelectItem,
  NativeSelectTrigger,
  NativeSelectValue,
  SelectorItem,
  SelectorItemGroup,
  Switch,
} from '@flint-ide/ui'
import { FolderOpen, GitCommitHorizontal, RotateCw } from '@flint-ide/ui/icons'

/** Flint's panel components from @flint-ide/ui (ui/src). */
const meta = { title: 'Flint/Panels' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const surface = { border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-raised)' } as const

export const DiffStats: Story = {
  render: () => (
    <div className="sb-stack" style={{ ...surface, padding: 12, maxWidth: 420 }}>
      {[
        { file: 'frontend/src/style.css', a: 24, r: 18 },
        { file: 'cmd/wuicss/main.go', a: 142, r: 0 },
        { file: 'frontend/src/boot.ts', a: 1, r: 0 },
        { file: 'docs/DEVELOPMENT.md', a: 8, r: 31 },
      ].map(({ file, a, r }) => (
        <div key={file} className="sb-row" style={{ justifyContent: 'space-between' }}>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>{file}</span>
          <DiffStat added={a} removed={r} />
        </div>
      ))}
    </div>
  ),
}

const JOBS: readonly Job[] = [
  { id: 'go', name: 'go test ./...', state: 'success', meta: '1m 12s' },
  { id: 'fe', name: 'frontend: typecheck + test', state: 'failure', meta: '48s' },
  { id: 'pages', name: 'pages: build docs + storybook', state: 'running', meta: '2m 03s' },
  { id: 'rel', name: 'release: macOS arm64', state: 'queued' },
  { id: 'win', name: 'release: Windows', state: 'skipped' },
]

export const CiStatus: Story = {
  name: 'CI status (JobList)',
  render: function Render() {
    const [selected, setSelected] = useState('')
    return (
      <div style={{ ...surface, maxWidth: 420 }}>
        <div className="sb-row" style={{ padding: 12, borderBottom: '1px solid var(--border)', justifyContent: 'space-between' }}>
          <span className="sb-row">
            <GitCommitHorizontal size={14} /> <strong>5541e62</strong> Merge pull request #76
          </span>
          <Badge color="red" badgeStyle="light">
            1 failing
          </Badge>
        </div>
        <div style={{ padding: 8 }}>
          <JobList jobs={JOBS} onSelect={(j) => setSelected(j.name)} />
        </div>
        <div className="sb-row" style={{ padding: 12, borderTop: '1px solid var(--border)' }}>
          <Button size="sm" variant="plain">
            <RotateCw data-icon="inline-start" /> Re-run failed
          </Button>
          <span className="sb-label">{selected && `Opened ${selected}`}</span>
        </div>
      </div>
    )
  },
}

const AGENTS: readonly AgentRowProps[] = [
  { name: 'Claude', model: 'claude-opus-5-5', task: 'Map WunderUI tokens onto Flint', state: 'working', elapsed: '4m' },
  { name: 'Reviewer', model: 'claude-sonnet-5-5', task: 'Review feat/ui-kit', state: 'waiting', elapsed: '1m' },
  { name: 'Tests', model: 'claude-haiku-5-5', task: 'Fix TestRenameVars', state: 'done', elapsed: '12m' },
  { name: 'Release', model: 'claude-haiku-5-5', task: 'Notarize the macOS build', state: 'failed', elapsed: '3m' },
]

export const AgentRows: Story = {
  render: () => (
    <div className="sb-stack" style={{ ...surface, padding: 8, maxWidth: 500, gap: 2 }}>
      {AGENTS.map((a) => (
        <AgentRow key={a.name} {...a} onStop={() => undefined} onOpen={() => undefined} />
      ))}
    </div>
  ),
}

export const SettingsRows: Story = {
  render: function Render() {
    const [format, setFormat] = useState(true)
    return (
      <div style={{ maxWidth: 520 }}>
        <SelectorItemGroup>
          <SelectorItem
            title="Format on save"
            description="Run the language server's formatter before writing the file."
            control={<Switch checked={format} onCheckedChange={setFormat} aria-label="Format on save" />}
          />
          <SelectorItem
            title="Agent model"
            description="Used for new agent sessions."
            control={
              <NativeSelect items={{ opus: 'Opus 5.5', sonnet: 'Sonnet 5.5', haiku: 'Haiku 5.5' }} defaultValue="opus">
                <NativeSelectTrigger aria-label="Agent model">
                  <NativeSelectValue />
                </NativeSelectTrigger>
                <NativeSelectContent>
                  <NativeSelectItem value="opus">Opus 5.5</NativeSelectItem>
                  <NativeSelectItem value="sonnet">Sonnet 5.5</NativeSelectItem>
                  <NativeSelectItem value="haiku">Haiku 5.5</NativeSelectItem>
                </NativeSelectContent>
              </NativeSelect>
            }
          />
        </SelectorItemGroup>
      </div>
    )
  },
}

export const EmptyPanel: Story = {
  render: () => (
    <div style={{ ...surface, width: 300, height: 320, display: 'grid', placeItems: 'center' }}>
      <EmptyState
        icon={<FolderOpen />}
        title="No folder open"
        description="Open a folder to browse files, run tasks and talk to the agent about the code."
        action={<Button size="sm">Open folder…</Button>}
      />
    </div>
  ),
}
