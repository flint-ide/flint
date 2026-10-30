import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { highlight } from './highlight'
import { visibleRange } from './virtualList'
import { findUrls, layoutLine, type Cell } from './terminalLinks'
import { geometryFor } from './terminalFit'

describe('highlight', () => {
  it('wraps runs of matches and escapes HTML', () => {
    assert.equal(highlight({ path: '<a>b', indices: [1, 3] }), '&lt;<b>a</b>&gt;<b>b</b>')
  })

  it('counts indices in code points', () => {
    assert.equal(highlight({ path: '😀ab', indices: [1] }), '😀<b>a</b>b')
  })
})

describe('visibleRange', () => {
  it('covers the viewport plus overscan, clamped to the list', () => {
    assert.deepEqual(visibleRange(220, 100, 1000, 22, 5), [5, 20])
    assert.deepEqual(visibleRange(0, 100, 3, 22, 30), [0, 3])
  })

  it('stays inside a list that shrank below the scroll position', () => {
    // Scrolled to row 1000 of a list that now has 10 rows (viewport shows 5).
    const [from, to] = visibleRange(22_000, 110, 10, 22, 3)
    assert.ok(from <= to && to === 10 && from === 2)
  })

  it('is empty for an empty list', () => {
    assert.deepEqual(visibleRange(0, 500, 0, 22, 10), [0, 0])
  })
})

describe('findUrls', () => {
  it('finds http(s) URLs and trims trailing punctuation', () => {
    const urls = findUrls('see https://example.com/a?b=1. and (http://x.io/path), done').map((m) => m.url)
    assert.deepEqual(urls, ['https://example.com/a?b=1', 'http://x.io/path'])
  })

  it('reports string offsets', () => {
    const [m] = findUrls('go: https://a.b/c')
    assert.deepEqual([m.start, m.end], [4, 17])
  })

  it('ignores things that only look like URLs', () => {
    assert.deepEqual(findUrls('ftp://x and https:// alone'), [])
  })
})

describe('layoutLine', () => {
  const row = (s: string, cols: number): Cell[] =>
    Array.from({ length: cols }, (_, i) => ({ chars: s[i] ?? '', width: 1 }))

  it('maps offsets across wrapped rows and trims the last row', () => {
    const { text, cells } = layoutLine([row('abcd', 4), row('ef', 4)], 10)
    assert.equal(text, 'abcdef')
    assert.deepEqual(cells[0], { x: 1, y: 11, width: 1 })
    assert.deepEqual(cells[5], { x: 2, y: 12, width: 1 })
  })

  it('keeps cells aligned after wide and surrogate-pair characters', () => {
    // "漢" spans two cells (the second is a width-0 placeholder); "😀" is two UTF-16 units.
    const cells: Cell[] = [
      { chars: '漢', width: 2 },
      { chars: '', width: 0 },
      { chars: '😀', width: 2 },
      { chars: '', width: 0 },
      { chars: 'h', width: 1 },
    ]
    const line = layoutLine([cells], 0)
    assert.equal(line.text, '漢😀h')
    const h = line.text.indexOf('h')
    assert.deepEqual(line.cells[h], { x: 5, y: 1, width: 1 })
    assert.deepEqual(line.cells[1], { x: 3, y: 1, width: 2 }) // first unit of the emoji
  })
})

describe('geometryFor', () => {
  it('fits whole cells after the scrollbar', () => {
    assert.deepEqual(geometryFor(814, 240, 8, 16, 14), { cols: 100, rows: 15 })
  })

  it('keeps a minimum size and rejects unmeasured cells', () => {
    assert.deepEqual(geometryFor(0, 0, 8, 16, 14), { cols: 2, rows: 1 })
    assert.equal(geometryFor(100, 100, 0, 16, 14), null)
  })
})
