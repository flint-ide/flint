import { describe, expect, it } from 'vitest'
import { Text } from '@codemirror/state'
import { lineChanges } from './gitGutter'

const doc = (s: string) => Text.of(s.split('\n'))

describe('lineChanges', () => {
  it('reports nothing for identical text', () => {
    expect(lineChanges(doc('a\nb\nc'), doc('a\nb\nc')).size).toBe(0)
  })

  it('marks added lines', () => {
    const changes = lineChanges(doc('a\nc'), doc('a\nb1\nb2\nc'))
    expect([...changes]).toEqual([
      [2, 'added'],
      [3, 'added'],
    ])
  })

  it('marks modified lines', () => {
    const changes = lineChanges(doc('a\nb\nc'), doc('a\nB\nc'))
    expect([...changes]).toEqual([[2, 'modified']])
  })

  it('marks a deletion on the following line', () => {
    const changes = lineChanges(doc('a\nb\nc'), doc('a\nc'))
    expect([...changes]).toEqual([[2, 'deleted']])
  })

  it('marks a deletion at the end on the last line', () => {
    const changes = lineChanges(doc('a\nb\nc'), doc('a\nb'))
    expect(changes.get(2)).toBeDefined()
  })
})
