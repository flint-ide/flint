import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Pagination,
  PaginationJump,
  PaginationPageSize,
  PaginationSimple,
  PaginationSummary,
  type PaginationCurrentStyle,
  type PaginationShape,
} from '@wunderui/react'

const SHAPES: PaginationShape[] = ['rounded', 'pill']
const CURRENT_STYLES: PaginationCurrentStyle[] = ['primary', 'subtle']

type Args = { shape: PaginationShape; currentStyle: PaginationCurrentStyle; showEdges: boolean; pageCount: number }

/** Newer Pagination options (shape, currentStyle, showEdges) and the table-footer parts: summary, page size, jump. */
const meta = {
  title: 'Navigation/Pagination Extras',
  args: { shape: 'rounded', currentStyle: 'primary', showEdges: false, pageCount: 24 },
  argTypes: {
    shape: { control: 'inline-radio', options: SHAPES },
    currentStyle: { control: 'inline-radio', options: CURRENT_STYLES },
    pageCount: { control: { type: 'range', min: 1, max: 100, step: 1 } },
  },
  render: function Render(args) {
    const [page, setPage] = useState(5)
    return <Pagination {...args} page={Math.min(page, args.pageCount)} onPageChange={setPage} />
  },
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const ShapesAndCurrentStyles: Story = {
  render: function Render({ showEdges, pageCount }) {
    const [page, setPage] = useState(5)
    return (
      <div className="sb-stack">
        {SHAPES.flatMap((shape) =>
          CURRENT_STYLES.map((currentStyle) => (
            <div key={`${shape}-${currentStyle}`} className="sb-row">
              <span className="sb-label" style={{ width: 120 }}>
                {shape} · {currentStyle}
              </span>
              <Pagination shape={shape} currentStyle={currentStyle} showEdges={showEdges} page={page} pageCount={pageCount} onPageChange={setPage} />
            </div>
          )),
        )}
        <div className="sb-row">
          <span className="sb-label" style={{ width: 120 }}>simple · pill</span>
          <PaginationSimple shape="pill" page={page} pageCount={pageCount} onPageChange={setPage} />
        </div>
      </div>
    )
  },
}

export const WithEdges: Story = { args: { showEdges: true, currentStyle: 'subtle' } }

/** Every PaginationSummary kind: range of total, total only, unknown total, loading, error. */
export const SummaryKinds: Story = {
  render: function Render() {
    const [status, setStatus] = useState<'error' | 'loading'>('error')
    const retry = () => {
      setStatus('loading')
      window.setTimeout(() => setStatus('error'), 1500)
    }
    const rows: [string, React.ReactNode][] = [
      ['range of total', <PaginationSummary page={1} pageSize={25} total={1248} noun="commit" />],
      ['total only', <PaginationSummary total={248} noun="job" />],
      ['unknown total', <PaginationSummary page={3} pageSize={50} noun="log line" />],
      ['short last page', <PaginationSummary page={5} pageSize={10} total={42} itemCount={2} noun="pull request" />],
      ['loading', <PaginationSummary status="loading" />],
      ['error', <PaginationSummary status={status} onRetry={retry} errorMessage="Couldn't load CI runs." />],
    ]
    return (
      <div className="sb-stack">
        {rows.map(([label, node]) => (
          <div key={label} className="sb-row">
            <span className="sb-label" style={{ width: 120 }}>{label}</span>
            {node}
          </div>
        ))}
      </div>
    )
  },
}

/** PaginationPageSize and PaginationJump on their own. */
export const PageSizeAndJump: Story = {
  render: function Render() {
    const [size, setSize] = useState(25)
    const [jumped, setJumped] = useState<number | null>(null)
    return (
      <div className="sb-stack">
        <div className="sb-row">
          <PaginationPageSize value={size} onValueChange={setSize} />
          <PaginationPageSize value={size} onValueChange={setSize} options={[25, 50, 200]} label="Runs per page" />
          <PaginationPageSize value={size} onValueChange={setSize} disabled />
        </div>
        <div className="sb-row" style={{ minHeight: 56, alignItems: 'flex-start' }}>
          <PaginationJump pageCount={18} onPageChange={setJumped} />
          <PaginationJump pageCount={18} onPageChange={setJumped} label="Page" buttonLabel="Open" disabled />
          <span className="sb-label">{jumped ? `Jumped to page ${jumped}` : 'Try 0 or 19 to see the error'}</span>
        </div>
      </div>
    )
  },
}

/** A full table footer for the CI runs list: summary, page size, pages and jump. */
export const TableFooter: Story = {
  render: function Render({ shape, currentStyle }) {
    const total = 1248
    const [pageSize, setPageSize] = useState(25)
    const [page, setPage] = useState(1)
    const pageCount = Math.ceil(total / pageSize)
    const changeSize = (s: number) => {
      setPageSize(s)
      setPage(1)
    }
    return (
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 16,
          padding: '8px 12px 20px',
          border: '1px solid var(--border)',
          background: 'var(--bg-raised)',
          borderRadius: 8,
        }}
      >
        <div className="sb-row">
          <PaginationSummary page={page} pageSize={pageSize} total={total} noun="run" />
          <PaginationPageSize value={pageSize} onValueChange={changeSize} />
        </div>
        <Pagination shape={shape} currentStyle={currentStyle} page={page} pageCount={pageCount} onPageChange={setPage} />
        <PaginationJump pageCount={pageCount} onPageChange={setPage} />
      </div>
    )
  },
}
