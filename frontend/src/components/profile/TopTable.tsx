import { useMemo, useState } from 'react'
import { formatValue, IconButton, percent, shortName, sortTop, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, type TopRow, type TopSortKey } from '@flint-ide/ui'
import { Search } from '@flint-ide/ui/icons'

interface Props {
  rows: TopRow[]
  total: number
  unit: string
  /** Filters rows by function name (the search box). */
  query: string
  onOpen: (row: TopRow) => void
  /** Shows the function in the flame graph (search for it). */
  onHighlight: (row: TopRow) => void
}

const COLUMNS: { key: TopSortKey | null; label: string; className: string; title?: string }[] = [
  { key: 'flat', label: 'Flat', className: 'num', title: 'Value spent in the function itself' },
  { key: null, label: 'Flat %', className: 'num' },
  { key: 'cum', label: 'Cum', className: 'num', title: 'Value of the function and everything it called' },
  { key: null, label: 'Cum %', className: 'num' },
  { key: 'name', label: 'Function', className: 'name' },
  { key: null, label: 'Location', className: 'loc' },
]

/** The top functions by flat or cumulative value (pprof's `top`), a few hundred rows at most. */
export function TopTable({ rows, total, unit, query, onOpen, onHighlight }: Props) {
  const [sort, setSort] = useState<TopSortKey>('flat')
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return sortTop(q ? rows.filter((r) => r.name.toLowerCase().includes(q)) : rows, sort)
  }, [rows, query, sort])

  return (
    <div className="profile-top">
      <Table>
        <TableHeader>
          <TableRow>
            {COLUMNS.map((c) => (
              <TableHead
                key={c.label}
                className={`${c.className}${c.key ? ' sortable' : ''}${c.key === sort ? ' sorted' : ''}`}
                title={c.title}
                onClick={c.key ? () => setSort(c.key!) : undefined}
              >
                {c.label}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {shown.map((r) => (
            <TableRow key={r.name} title={r.name} onClick={() => onOpen(r)} className={r.file ? 'has-source' : undefined}>
              <TableCell className="num">{formatValue(r.flat, unit)}</TableCell>
              <TableCell className="num muted">{percent(r.flat, total)}</TableCell>
              <TableCell className="num">{formatValue(r.cum, unit)}</TableCell>
              <TableCell className="num muted">{percent(r.cum, total)}</TableCell>
              <TableCell className="name">
                <div className="fn">
                  <span className="truncate">{shortName(r.name)}</span>
                  <IconButton
                    variant="ghost"
                    size="sm"
                    className="fn-action"
                    aria-label="Highlight in the flame graph"
                    title="Highlight in the flame graph"
                    onClick={(e) => {
                      e.stopPropagation()
                      onHighlight(r)
                    }}
                  >
                    <Search />
                  </IconButton>
                </div>
              </TableCell>
              <TableCell className="loc muted truncate">{r.file ? `${r.file.slice(r.file.lastIndexOf('/') + 1)}${r.line ? `:${r.line}` : ''}` : ''}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {shown.length === 0 && <div className="muted placeholder">{query ? 'No function matches' : 'No samples'}</div>}
    </div>
  )
}
