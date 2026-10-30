import { afterEach, describe, expect, it, vi } from 'vitest'
import { createReorderer } from './reorder'

type Ev = { seq?: number; v: string }

const collect = (maxWait?: number) => {
  const out: string[] = []
  const push = createReorderer<Ev>((s, e) => out.push(`${s}:${e.v}`), maxWait)
  return { out, push }
}

afterEach(() => vi.useRealTimers())

describe('createReorderer', () => {
  it('passes in-order events straight through', () => {
    const { out, push } = collect()
    push('a', { seq: 1, v: 'x' })
    push('a', { seq: 2, v: 'y' })
    expect(out).toEqual(['a:x', 'a:y'])
  })

  it('holds early events until the gap is filled', () => {
    const { out, push } = collect()
    push('a', { seq: 3, v: 'c' })
    push('a', { seq: 2, v: 'b' })
    expect(out).toEqual([])
    push('a', { seq: 1, v: 'a' })
    expect(out).toEqual(['a:a', 'a:b', 'a:c'])
  })

  it('keeps streams independent and drops duplicates', () => {
    const { out, push } = collect()
    push('a', { seq: 1, v: '1' })
    push('b', { seq: 1, v: '1' })
    push('a', { seq: 1, v: 'dup' })
    expect(out).toEqual(['a:1', 'b:1'])
  })

  it('flushes after a lost event instead of stalling', () => {
    vi.useFakeTimers()
    const { out, push } = collect(100)
    push('a', { seq: 1, v: '1' })
    push('a', { seq: 3, v: '3' })
    push('a', { seq: 4, v: '4' })
    expect(out).toEqual(['a:1'])
    vi.advanceTimersByTime(150)
    expect(out).toEqual(['a:1', 'a:3', 'a:4'])
    push('a', { seq: 5, v: '5' })
    expect(out.at(-1)).toBe('a:5')
  })

  it('delivers unnumbered events immediately', () => {
    const { out, push } = collect()
    push('a', { v: 'plain' })
    expect(out).toEqual(['a:plain'])
  })
})
