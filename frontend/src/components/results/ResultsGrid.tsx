import { useMemo, useState } from 'react'
import { useVirtualList } from '../../lib/virtualList'
import type { QueryResult } from '../../api/backend'
import { cellText, columnWidths, toTSV } from '../../lib/format'
import { showToast } from '../../store/dialog'

const ROW_HEIGHT = 24
const INDEX_WIDTH = 56

interface Props {
  result: QueryResult
  /** Left side of the grid's header bar (e.g. row count and timing). */
  summary: React.ReactNode
}

/** Virtualized result table: only visible rows are rendered, so 100k rows stay smooth. */
export function ResultsGrid({ result, summary }: Props) {
  const [selected, setSelected] = useState<{ row: number; col: number } | null>(null)
  const widths = useMemo(() => columnWidths(result), [result])
  const totalWidth = INDEX_WIDTH + widths.reduce((a, b) => a + b, 0)
  const template = `${INDEX_WIDTH}px ${widths.map((w) => `${w}px`).join(' ')}`

  const { scrollRef, totalSize, items } = useVirtualList({ count: result.rows.length, itemSize: ROW_HEIGHT, overscan: 20 })

  const copy = async (text: string, what: string) => {
    await navigator.clipboard.writeText(text)
    showToast(`Copied ${what}`)
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!selected || !(e.metaKey || e.ctrlKey) || e.key !== 'c') return
    e.preventDefault()
    void copy(cellText(result.rows[selected.row][selected.col]), 'cell')
  }

  return (
    <div className="results-grid">
      <div className="result-summary">
        {summary}
        <span className="spacer" />
        <button className="link" onClick={() => copy(toTSV(result), 'as TSV')}>Copy TSV</button>
        <button className="link" onClick={() => copy(JSON.stringify(toObjects(result), null, 2), 'as JSON')}>
          Copy JSON
        </button>
      </div>
      <div className="grid-scroller" ref={scrollRef} tabIndex={0} onKeyDown={onKeyDown}>
        <div style={{ width: totalWidth, height: totalSize + ROW_HEIGHT, position: 'relative' }}>
          <div className="grid-row grid-header" style={{ gridTemplateColumns: template, width: totalWidth }}>
            <div className="grid-cell index">#</div>
            {result.columns.map((c, i) => (
              <div key={i} className="grid-cell" title={`${c.name} : ${c.type}`}>
                <span className="col-name">{c.name}</span>
                <span className="col-type">{c.type}</span>
              </div>
            ))}
          </div>
          {items.map((vr) => {
            const row = result.rows[vr.index]
            return (
              <div
                key={vr.index}
                className="grid-row"
                style={{
                  gridTemplateColumns: template,
                  width: totalWidth,
                  transform: `translateY(${vr.start + ROW_HEIGHT}px)`,
                }}
              >
                <div className="grid-cell index">{vr.index + 1}</div>
                {row.map((v, ci) => {
                  const isSel = selected?.row === vr.index && selected.col === ci
                  return (
                    <div
                      key={ci}
                      className={`grid-cell${v === null ? ' null' : ''}${isSel ? ' selected' : ''}`}
                      onClick={() => setSelected({ row: vr.index, col: ci })}
                      onDoubleClick={() => copy(cellText(v), 'cell')}
                    >
                      {v === null ? 'NULL' : cellText(v)}
                    </div>
                  )
                })}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function toObjects(result: QueryResult): Record<string, unknown>[] {
  return result.rows.map((row) => Object.fromEntries(result.columns.map((c, i) => [c.name, row[i]])))
}
