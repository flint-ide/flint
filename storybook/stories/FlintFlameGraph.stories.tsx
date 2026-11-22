import type { Meta, StoryObj } from '@storybook/react-vite'
import { useMemo, useState } from 'react'
import { Button, FlameGraph, frameAt, Input, searchFrames, ViewToolbar, type FlameFrame } from '@flint-ide/ui'

/** FlameGraph: the profiler's icicle graph on a canvas (ui/src/FlameGraph.tsx); the layout and colors are pure code in ui/src/flame.ts. */
const meta = { title: 'Flint/Flame Graph', component: FlameGraph } satisfies Meta<typeof FlameGraph>

export default meta
type Story = StoryObj<typeof meta>

const f = (name: string, total: number, children: FlameFrame[] = [], file?: string): FlameFrame => ({
  name,
  file,
  line: file ? 42 : undefined,
  total,
  self: total - children.reduce((s, c) => s + c.total, 0),
  children: children.length ? children : undefined,
})

const sample = f('root', 1000, [
  f('main.main', 1000, [
    f('flint/internal/git.(*Repo).Status', 520, [
      f('os/exec.(*Cmd).Run', 380, [f('os/exec.(*Cmd).Wait', 300, [f('syscall.wait4', 280)]), f('os/exec.(*Cmd).Start', 60)]),
      f('flint/internal/git.parseStatus', 110, [f('strings.Split', 60)], '/src/internal/git/status.go'),
    ], '/src/internal/git/repo.go'),
    f('flint/internal/search.(*Index).Query', 300, [
      f('flint/internal/search.score', 180, [f('math.Log', 70)]),
      f('sort.Slice', 90, [f('runtime.memmove', 40)]),
    ], '/src/internal/search/index.go'),
    f('runtime.gcBgMarkWorker', 120, [f('runtime.scanobject', 100)]),
  ]),
])

const frame = { width: 640, height: 280, display: 'flex', flexDirection: 'column', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden', background: 'var(--bg-editor)' } as const

function Demo({ unit }: { unit: string }) {
  const [focus, setFocus] = useState<number[]>([])
  const [query, setQuery] = useState('')
  const search = useMemo(() => searchFrames(sample, query), [query])
  const zoomed = frameAt(sample, focus)
  return (
    <div style={frame}>
      <ViewToolbar aria-label="Profile">
        <Input size="sm" type="search" value={query} placeholder="Search functions" aria-label="Search functions" style={{ width: 200 }} onChange={(e) => setQuery(e.target.value)} />
        <Button variant="ghost" size="sm" disabled={focus.length === 0} onClick={() => setFocus([])}>
          Reset Zoom
        </Button>
        <span className="fui-grow" />
        <span className="fui-muted">{zoomed?.name}</span>
      </ViewToolbar>
      <FlameGraph
        root={sample}
        unit={unit}
        focus={focus}
        onZoom={setFocus}
        search={search}
        onOpen={() => undefined}
        openHint="Click to zoom · Ctrl-click or double-click opens the source"
      />
    </div>
  )
}

/** Click a frame to zoom, Escape or Reset Zoom to go back; type to highlight matches. */
export const Default: Story = {
  args: { root: sample, unit: 'nanoseconds', focus: [], onZoom: () => undefined, search: { matched: new Set(), total: 0 }, onOpen: () => undefined },
  render: () => <Demo unit="nanoseconds" />,
}

export const Bytes: Story = {
  args: Default.args,
  render: () => <Demo unit="bytes" />,
}
