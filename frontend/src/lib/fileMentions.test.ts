import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { fileMentionItems, FILES_GROUP } from './fileMentions'

describe('fileMentionItems', () => {
  it('shows the file name with its folder as the detail, and keeps the absolute path as the id', () => {
    assert.deepEqual(fileMentionItems([{ path: 'src/chat/App.tsx', indices: [] }], '/w'), [
      { id: '/w/src/chat/App.tsx', label: 'App.tsx', detail: 'src/chat', group: FILES_GROUP },
    ])
  })
  it('leaves the detail out for a file at the top of the folder', () => {
    assert.deepEqual(fileMentionItems([{ path: 'go.mod', indices: [] }], '/w'), [{ id: '/w/go.mod', label: 'go.mod', detail: undefined, group: FILES_GROUP }])
  })
  it('uses the match’s own folder and skips its label when several folders are open', () => {
    const [item] = fileMentionItems([{ path: 'api/cmd/main.go', indices: [], root: '/work/api', offset: 4 }], '/w')
    assert.deepEqual(item, { id: '/work/api/cmd/main.go', label: 'main.go', detail: 'cmd', group: FILES_GROUP })
  })
  it('keeps the order of the matches and drops a file found twice', () => {
    const items = fileMentionItems([{ path: 'b.ts', indices: [] }, { path: 'a.ts', indices: [] }, { path: 'b.ts', indices: [] }], '/w')
    assert.deepEqual(items.map((i) => i.label), ['b.ts', 'a.ts'])
  })
  it('is empty for no matches', () => {
    assert.deepEqual(fileMentionItems([], '/w'), [])
  })
})
