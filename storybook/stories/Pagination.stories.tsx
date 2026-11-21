import type { Meta, StoryObj } from '@storybook/react-vite'
import { useEffect, useState } from 'react'
import { Pagination, PaginationSimple } from '@wunderui/react'

type Args = { page: number; pageCount: number }

const PULL_REQUESTS = Array.from({ length: 42 }, (_, i) => ({
  number: 76 - i,
  title: ['Fix symlinked review drafts', 'Bump go-minor group', 'Add run configurations', 'Terminal split view'][i % 4],
}))
const PAGE_SIZE = 5

/** Numbered pagination: controlled, so the story keeps the page in state and mirrors the `page` arg. */
const meta = {
  title: 'Navigation/Pagination',
  args: { page: 1, pageCount: 12 },
  argTypes: {
    page: { control: { type: 'number', min: 1 } },
    pageCount: { control: { type: 'range', min: 1, max: 50, step: 1 } },
  },
  render: function Render({ page: initial, pageCount }) {
    const [page, setPage] = useState(initial)
    useEffect(() => setPage(initial), [initial])
    return <Pagination page={Math.min(page, pageCount)} pageCount={pageCount} onPageChange={setPage} />
  },
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const MiddlePage: Story = { args: { page: 6, pageCount: 12 } }

export const FewPages: Story = { args: { page: 2, pageCount: 3 } }

/** PaginationSimple: previous / next with the current position. */
export const Simple: Story = {
  render: function Render({ page: initial, pageCount }) {
    const [page, setPage] = useState(initial)
    useEffect(() => setPage(initial), [initial])
    return <PaginationSimple page={Math.min(page, pageCount)} pageCount={pageCount} onPageChange={setPage} />
  },
}

/** Paging through a list of pull requests. */
export const WithList: Story = {
  render: function Render() {
    const [page, setPage] = useState(1)
    const pageCount = Math.ceil(PULL_REQUESTS.length / PAGE_SIZE)
    const visible = PULL_REQUESTS.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
    return (
      <div className="sb-stack" style={{ maxWidth: 520 }}>
        <ul style={{ margin: 0, padding: 0, listStyle: 'none', fontSize: 13 }}>
          {visible.map((pr) => (
            <li key={pr.number} style={{ padding: '6px 0', borderBottom: '1px solid var(--border, transparent)' }}>
              <span style={{ color: 'var(--fg-muted)', marginRight: 8 }}>#{pr.number}</span>
              {pr.title}
            </li>
          ))}
        </ul>
        <Pagination page={page} pageCount={pageCount} onPageChange={setPage} />
      </div>
    )
  },
}
