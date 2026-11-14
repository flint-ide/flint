import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { EditorState } from '@codemirror/state'
import { clearDecorationsEffect, decorationField, documentVersion, hideDecorationsEffect, setDecorationsEffect } from './editorDecorations'
import { coerceDecorations, type EditorDecoration } from './decorationModel'

const text = ['one', 'two TODO here', '', 'four', 'five'].join('\n')
const fresh = () => EditorState.create({ doc: text, extensions: decorationField })

const decos = (...list: Record<string, unknown>[]): EditorDecoration[] => coerceDecorations(list)
const publish = (state: EditorState, set: string, version: number, decorations: EditorDecoration[]) =>
  state.update({ effects: setDecorationsEffect.of({ set, version, decorations }) }).state

/** The marks of a set as "kind from-to", in document order. */
function marks(state: EditorState, set: string): string[] {
  const out: string[] = []
  const rs = state.field(decorationField).sets.get(set)
  for (const cur = rs?.iter(); cur?.value; cur.next()) out.push(`${cur.value.d.kind} ${cur.from}-${cur.to}`)
  return out
}

/** The lines (1-based) with a gutter marker. */
function markerLines(state: EditorState): number[] {
  const out: number[] = []
  for (const cur = state.field(decorationField).markers.iter(); cur.value; cur.next()) out.push(state.doc.lineAt(cur.from).number)
  return out
}

const sample = () =>
  decos(
    { kind: 'diagnostic', line: 2, from: 4, to: 8, severity: 'warning', message: 'TODO', actions: [{ id: 'remove', label: 'Remove' }] },
    { kind: 'gutter', line: 4, severity: 'ok' },
    { kind: 'inline', line: 1, text: 'hint' },
    { kind: 'highlight', line: 4, endLine: 5 },
  )

describe('decorations in an editor', () => {
  it('counts document versions', () => {
    let state = fresh()
    assert.equal(documentVersion(state), 1)
    state = state.update({ changes: { from: 0, insert: 'x' } }).state
    state = state.update({ selection: { anchor: 1 } }).state
    assert.equal(documentVersion(state), 2, 'only changes to the text count')
    assert.equal(documentVersion(EditorState.create({ doc: '' })), 0, 'an editor without decorations has no version')
  })

  it('places a publish and draws it', () => {
    const state = publish(fresh(), 'a.b.marks', 1, sample())
    assert.deepEqual(marks(state, 'a.b.marks'), ['inline 0-0', 'diagnostic 8-12', 'gutter 19-19', 'highlight 19-28'])
    assert.deepEqual(markerLines(state), [2, 4], 'a diagnostic and a gutter decoration mark their lines')
    // An underline, an inline widget and two highlighted lines.
    assert.equal(state.field(decorationField).decorations.size, 4)
  })

  it('drops a publish made for another version, keeps one without a version', () => {
    let state = fresh().update({ changes: { from: 0, insert: 'x' } }).state // version 2
    state = publish(state, 'a.b.marks', 1, sample())
    assert.deepEqual(marks(state, 'a.b.marks'), [], 'stale')
    state = publish(state, 'a.b.marks', 3, sample())
    assert.deepEqual(marks(state, 'a.b.marks'), [], 'a version the editor never had')
    state = publish(state, 'a.b.marks', 2, sample())
    assert.equal(marks(state, 'a.b.marks').length, 4)
    state = publish(state, 'a.b.marks', 0, decos({ kind: 'gutter', line: 1 }))
    assert.deepEqual(marks(state, 'a.b.marks'), ['gutter 0-0'], 'a publish replaces the set')
    // A stale publish leaves what is shown alone.
    state = publish(state, 'a.b.marks', 1, [])
    assert.deepEqual(marks(state, 'a.b.marks'), ['gutter 0-0'])
  })

  it('clamps positions beyond the document', () => {
    const state = publish(fresh(), 'a.b.marks', 1, decos({ kind: 'gutter', line: 99 }, { kind: 'diagnostic', line: 1, from: 50, to: 90, message: 'm' }, { kind: 'highlight', line: 4, endLine: 400 }))
    // The diagnostic at the end of line 1 underlines the character before it.
    assert.deepEqual(marks(state, 'a.b.marks'), ['diagnostic 2-3', 'highlight 19-28', 'gutter 24-24'])
    assert.deepEqual(markerLines(state), [1, 5])
  })

  it('moves marks with later edits until the next publish', () => {
    let state = publish(fresh(), 'a.b.marks', 1, sample())
    // A new first line: everything moves down one line.
    state = state.update({ changes: { from: 0, insert: 'zero\n' } }).state
    assert.deepEqual(marks(state, 'a.b.marks'), ['inline 5-5', 'diagnostic 13-17', 'gutter 24-24', 'highlight 24-33'])
    assert.deepEqual(markerLines(state), [3, 5])
    // Typing inside the underlined word widens it; typing right after it doesn't.
    state = state.update({ changes: { from: 15, insert: '--' } }).state
    state = state.update({ changes: { from: 19, insert: '!' } }).state
    assert.equal(marks(state, 'a.b.marks')[1], 'diagnostic 13-19')
    // Deleting the underlined text removes its mark, and the line's marker with it.
    state = state.update({ changes: { from: 12, to: 20 } }).state
    assert.deepEqual(marks(state, 'a.b.marks').map((m) => m.split(' ')[0]), ['inline', 'gutter', 'highlight'])
    assert.deepEqual(markerLines(state), [5])
    assert.equal(documentVersion(state), 5)
    // The next publish, for the text as it is now, replaces them.
    state = publish(state, 'a.b.marks', 5, decos({ kind: 'gutter', line: 2 }))
    assert.deepEqual(marks(state, 'a.b.marks'), ['gutter 5-5'])
  })

  it('keeps sets apart, clears them and hides them', () => {
    let state = publish(fresh(), 'a.b.marks', 1, sample())
    state = publish(state, 'c.d.cover', 1, decos({ kind: 'gutter', line: 3, severity: 'error' }, { kind: 'gutter', line: 4, severity: 'error' }))
    assert.deepEqual(markerLines(state), [2, 3, 4], 'one marker per line, whoever marks it')
    state = state.update({ effects: hideDecorationsEffect.of(true) }).state
    assert.deepEqual([state.field(decorationField).decorations.size, markerLines(state).length], [0, 0])
    state = state.update({ effects: hideDecorationsEffect.of(false) }).state
    assert.deepEqual(markerLines(state), [2, 3, 4])
    state = state.update({ effects: clearDecorationsEffect.of((set) => set.startsWith('a.b.')) }).state
    assert.deepEqual([...state.field(decorationField).sets.keys()], ['c.d.cover'])
    assert.deepEqual(markerLines(state), [3, 4])
    assert.equal(state.field(decorationField).decorations.size, 0)
    // An empty publish removes the set.
    state = publish(state, 'c.d.cover', 0, [])
    assert.equal(state.field(decorationField).sets.size, 0)
  })
})
