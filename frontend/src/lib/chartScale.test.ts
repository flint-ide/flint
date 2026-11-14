import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  MAX_CATEGORIES,
  bandAxis,
  bands,
  chartGeometry,
  chartRows,
  chartSummary,
  formatDateTick,
  formatTick,
  niceStep,
  niceTicks,
  xText,
  yTickValues,
  type ChartData,
} from './chartScale'

const day = 86400e3
const t0 = Date.UTC(2026, 9, 1)

const line: ChartData = {
  type: 'line',
  x: { type: 'date', label: 'Week' },
  y: { label: 'Issues', min: 0 },
  series: [
    { id: 'open', label: 'Open', points: [{ x: t0 + 7 * day, y: 11 }, { x: t0, y: 14 }] },
    { id: 'blocked', label: 'Blocked', points: [{ x: t0, y: 2 }, { x: t0 + 7 * day, y: 3 }] },
  ],
}

const bar: ChartData = {
  type: 'bar',
  x: { type: 'category' },
  y: {},
  series: [
    { id: 'files', label: 'Files', points: [{ x: '.go', y: 40 }, { x: '.ts', y: 25 }, { x: '.md', y: 5 }] },
    { id: 'tests', label: 'Tests', points: [{ x: '.ts', y: 10 }, { x: '.go', y: 30 }] },
  ],
}

describe('chart ticks', () => {
  it('picks steps of 1, 2 or 5 times a power of ten', () => {
    assert.equal(niceStep(100, 5), 20)
    assert.equal(niceStep(7, 5), 2)
    assert.equal(niceStep(0.9, 5), 0.2)
    assert.equal(niceStep(48, 5), 10)
    assert.equal(niceStep(0, 5), 1)
    assert.equal(niceStep(NaN, 5), 1)
  })

  it('covers the range with round ticks', () => {
    assert.deepEqual(niceTicks(0, 14), [0, 5, 10, 15])
    assert.deepEqual(niceTicks(3, 97), [0, 20, 40, 60, 80, 100])
    assert.deepEqual(niceTicks(-4, 4), [-4, -2, 0, 2, 4])
    assert.deepEqual(niceTicks(0, 0.5), [0, 0.1, 0.2, 0.3, 0.4, 0.5])
    assert.deepEqual(niceTicks(10, 0, 2), [0, 5, 10])
  })

  it('widens a flat range and survives nonsense', () => {
    const flat = niceTicks(5, 5)
    assert.ok(flat[0] < 5 && flat[flat.length - 1] > 5)
    assert.deepEqual(niceTicks(0, 0), [-1, -0.5, 0, 0.5, 1])
    assert.deepEqual(niceTicks(NaN, 3), [0, 1])
    assert.ok(niceTicks(0, 1e9, 1000).length <= 50)
  })

  it('labels ticks briefly', () => {
    assert.deepEqual([0, 950, 1200, 3400000, 2e9, 0.25, -1500].map(formatTick), ['0', '950', '1.2k', '3.4M', '2B', '0.25', '-1.5k'])
    assert.equal(formatDateTick(t0, 30 * day, true), '10-01')
    assert.equal(formatDateTick(t0, 400 * day, true), '2026-10-01')
    assert.equal(formatDateTick(t0 + 9.5 * 3600e3, day, true), '09:30')
    assert.equal(xText('date', t0, true), '2026-10-01')
    assert.equal(xText('date', t0 + 9.5 * 3600e3, true), '2026-10-01 09:30')
    assert.equal(xText('number', 1500), '1,500')
    assert.equal(xText('category', '.go'), '.go')
    assert.equal(xText('category', null), '')
  })

  it('starts bars and areas at zero, lines at the data, and honors the chart’s bounds', () => {
    assert.deepEqual(yTickValues(bar), [0, 10, 20, 30, 40])
    const high = { ...line, y: {}, series: [{ id: 's', label: 'S', points: [{ x: t0, y: 102 }, { x: t0 + day, y: 114 }] }] }
    assert.deepEqual(yTickValues(high), [100, 105, 110, 115])
    assert.deepEqual(yTickValues({ ...high, type: 'area' }), [0, 50, 100, 150])
    assert.deepEqual(yTickValues(line), [0, 5, 10, 15])
    assert.deepEqual(yTickValues({ ...bar, y: { min: 0, max: 100 } }), [0, 20, 40, 60, 80, 100])
    assert.deepEqual(yTickValues({ ...bar, y: { max: 33 } }), [0, 10, 20, 30, 33])
    assert.deepEqual(yTickValues({ ...bar, series: [] }), [0, 0.2, 0.4, 0.6, 0.8, 1])
    const odd = yTickValues({ ...bar, y: { min: 500 } })
    assert.equal(odd[0], 500)
    assert.ok(odd.length >= 2)
  })
})

describe('chart axes', () => {
  it('uses bands for bars and categories, a continuous axis for numbers and dates', () => {
    assert.equal(bandAxis(bar), true)
    assert.equal(bandAxis(line), false)
    assert.equal(bandAxis({ ...line, type: 'bar' }), true)
    assert.equal(bandAxis({ ...bar, type: 'line' }), true)
    assert.equal(bandAxis({ ...line, x: { type: 'number' }, type: 'area' }), false)
  })

  it('orders bands: categories as they appear, numbers and dates ascending, bounded', () => {
    assert.deepEqual(bands(bar), ['.go', '.ts', '.md'])
    assert.deepEqual(bands({ ...line, type: 'bar' }), [t0, t0 + 7 * day])
    const many: ChartData = { ...bar, series: [{ id: 's', label: 'S', points: Array.from({ length: 800 }, (_, i) => ({ x: `c${i}`, y: i })) }] }
    assert.equal(bands(many).length, MAX_CATEGORIES)
  })
})

describe('chart geometry', () => {
  it('draws grouped bars from the baseline inside the plot', () => {
    const g = chartGeometry(bar, 400, 200)
    assert.equal(g.empty, false)
    assert.equal(g.series.length, 2)
    assert.equal(g.series[0].marks.length, 3)
    assert.equal(g.series[1].marks.length, 2)
    const base = g.yTicks[0].at
    for (const s of g.series) {
      for (const m of s.marks) {
        assert.ok(m.x >= g.plot.left && m.x + m.width <= g.plot.right + 0.5, `bar at ${m.x} within the plot`)
        assert.ok(Math.abs(m.y + m.height - base) < 0.2, 'a bar ends on the baseline')
        assert.ok(m.width > 0 && m.height > 0)
      }
    }
    // .go: files 40 is the tallest bar and reaches the top tick; tests' .go bar is beside it.
    const [go, ts] = g.series[0].marks
    assert.ok(Math.abs(go.y - g.yTicks[g.yTicks.length - 1].at) < 0.2)
    assert.ok(ts.height < go.height)
    const tests = g.series[1].marks
    assert.equal(tests[1].title, 'Tests, .go: 30')
    assert.ok(tests[1].x > go.x && tests[1].x < ts.x)
    assert.deepEqual(g.xTicks.map((t) => t.label), ['.go', '.ts', '.md'])
    assert.equal(g.series[0].line, '')
  })

  it('draws bars below zero downwards', () => {
    const g = chartGeometry({ ...bar, series: [{ id: 's', label: 'S', points: [{ x: 'a', y: -5 }, { x: 'b', y: 5 }] }] }, 300, 200)
    const zero = g.yTicks.find((t) => t.label === '0')?.at ?? NaN
    const [down, up] = g.series[0].marks
    assert.ok(Math.abs(down.y - zero) < 0.2)
    assert.ok(Math.abs(up.y + up.height - zero) < 0.2)
  })

  it('draws a line per series, points in x order, with dots when there are few', () => {
    const g = chartGeometry(line, 500, 240, true)
    const open = g.series[0]
    assert.match(open.line, /^M[\d.]+ [\d.]+L[\d.]+ [\d.]+$/)
    assert.equal(open.area, '')
    assert.equal(open.marks.length, 2)
    assert.ok(open.marks[0].x < open.marks[1].x, 'sorted by x')
    assert.equal(open.marks[0].title, 'Open, 2026-10-01: 14')
    assert.ok(open.marks[0].y < open.marks[1].y, '14 is above 11')
    assert.ok(Math.abs(open.marks[0].x - g.plot.left) < 0.2 && Math.abs(open.marks[1].x - g.plot.right) < 0.2)
    assert.equal(g.xTicks[0].label, '10-01')
    assert.equal(g.xTicks[g.xTicks.length - 1].label, '10-08')
    assert.deepEqual(g.yTicks.map((t) => t.label), ['0', '5', '10', '15'])
    assert.ok(g.yTicks[0].at > g.yTicks[3].at, 'y grows upwards')
  })

  it('closes an area down to the baseline', () => {
    const g = chartGeometry({ ...line, type: 'area' }, 500, 240)
    assert.match(g.series[0].area, /Z$/)
    assert.ok(g.series[0].area.startsWith(g.series[0].line))
  })

  it('puts a line over categories at the band centers', () => {
    const g = chartGeometry({ ...bar, type: 'line' }, 400, 200)
    assert.equal(g.series[0].marks.length, 3)
    assert.ok(g.series[0].line.startsWith('M'))
    assert.equal(g.series[0].marks[0].x, g.xTicks[0].at)
  })

  it('leaves the dots out of long lines and thins the labels of many bands', () => {
    const long: ChartData = { type: 'line', x: { type: 'number' }, y: {}, series: [{ id: 's', label: 'S', points: Array.from({ length: 2000 }, (_, i) => ({ x: i, y: Math.sin(i / 50) })) }] }
    const g = chartGeometry(long, 600, 240)
    assert.equal(g.series[0].marks.length, 0)
    assert.ok(g.series[0].line.length > 2000)
    const wide: ChartData = { ...bar, series: [{ id: 's', label: 'S', points: Array.from({ length: 200 }, (_, i) => ({ x: `category ${i}`, y: i })) }] }
    const w = chartGeometry(wide, 400, 200)
    assert.equal(w.series[0].marks.length, 200)
    assert.ok(w.xTicks.length <= 6, `${w.xTicks.length} labels`)
  })

  it('clamps values outside the chart’s own bounds and survives odd input', () => {
    const g = chartGeometry({ ...bar, y: { min: 0, max: 20 } }, 400, 200)
    for (const m of g.series[0].marks) assert.ok(m.y >= g.plot.top - 0.2 && m.y + m.height <= g.plot.bottom + 0.2)
    const none = chartGeometry({ type: 'line', x: { type: 'number' }, y: {}, series: [] }, 300, 150)
    assert.equal(none.empty, true)
    const one = chartGeometry({ type: 'line', x: { type: 'number' }, y: {}, series: [{ id: 's', label: 'S', points: [{ x: 5, y: 5 }] }] }, 300, 150)
    assert.equal(one.empty, false)
    assert.equal(one.xTicks.length, 1)
    const bad = chartGeometry({ type: 'line', x: { type: 'number' }, y: {}, series: [{ id: 's', label: 'S', points: [{ x: 'text', y: 1 }, { x: 1, y: NaN }] }] }, 300, 150)
    assert.equal(bad.empty, true)
    const tiny = chartGeometry(bar, 20, 10)
    assert.ok(tiny.plot.right > tiny.plot.left && tiny.plot.bottom > tiny.plot.top)
    assert.ok(JSON.stringify([g, none, one, bad, tiny]).indexOf('NaN') < 0 && JSON.stringify(tiny).indexOf('null') < 0)
  })
})

describe('chart in words', () => {
  it('summarizes the chart', () => {
    assert.equal(chartSummary(line, 'Open issues'), 'Line chart: Open issues. 2 series (Open, Blocked), 4 points.')
    assert.equal(chartSummary({ ...bar, series: [] }, ''), 'Bar chart. 0 series, 0 points.')
  })

  it('lists the values as a table', () => {
    assert.deepEqual(chartRows(bar), {
      header: ['x', 'Files', 'Tests'],
      rows: [['.go', '40', '30'], ['.ts', '25', '10'], ['.md', '5', '']],
      more: 0,
    })
    const t = chartRows(line, 1, true)
    assert.deepEqual(t, { header: ['Week', 'Open', 'Blocked'], rows: [['2026-10-01', '14', '2']], more: 1 })
  })
})
