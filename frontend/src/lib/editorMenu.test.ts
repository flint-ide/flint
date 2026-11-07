import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { editorContextMenu, type EditorMenuInput } from './editorMenu'

const labels = (items: ReturnType<typeof editorContextMenu>) => items.map((i) => ('label' in i ? i.label : '—'))

function input(available: string[], over: Partial<EditorMenuInput> = {}, ran: string[] = []): EditorMenuInput {
  const noop = () => undefined
  return {
    hasSelection: false,
    command: (id) => (available.includes(id) ? { title: id } : null),
    runCommand: (id) => ran.push(id),
    clipboard: { cut: noop, copy: noop, paste: noop, selectAll: noop },
    program: [],
    file: null,
    ...over,
  }
}

describe('editorContextMenu', () => {
  it('keeps clipboard entries and drops unavailable commands and empty groups', () => {
    assert.deepEqual(labels(editorContextMenu(input([]))), ['Cut', 'Copy', 'Paste', 'Select All'])
  })

  it('separates groups in order', () => {
    const items = editorContextMenu(
      input(['nav.findUsages', 'agent.inlineEdit', 'git.fileHistory'], {
        program: [{ label: "Run 'app'", onSelect: () => undefined }],
        file: { copyPath: () => undefined, copyRelativePath: () => undefined, reveal: () => undefined },
      }),
    )
    assert.deepEqual(labels(items), [
      'nav.findUsages', '—', 'Cut', 'Copy', 'Paste', 'Select All', '—', "Run 'app'", '—', 'agent.inlineEdit', '—', 'git.fileHistory',
      '—', 'Copy Path', 'Copy Relative Path', 'Reveal in Finder',
    ])
  })

  it('disables cut and copy without a selection', () => {
    const without = editorContextMenu(input([]))
    const withSel = editorContextMenu(input([], { hasSelection: true }))
    const disabled = (items: typeof without, label: string) => items.some((i) => 'label' in i && i.label === label && 'disabled' in i && i.disabled)
    assert.equal(disabled(without, 'Copy'), true)
    assert.equal(disabled(withSel, 'Copy'), false)
  })

  it('offers Run Test at Cursor only in test files', () => {
    assert.equal(labels(editorContextMenu(input(['run.context']))).includes('run.context'), false)
    assert.equal(labels(editorContextMenu(input(['run.context', 'test.file']))).includes('run.context'), true)
  })

  it('runs the picked command', () => {
    const ran: string[] = []
    const item = editorContextMenu(input(['git.blame'], {}, ran)).find((i) => 'label' in i && i.label === 'git.blame')
    if (!item || !('onSelect' in item)) throw new Error('missing')
    item.onSelect({ altKey: false, metaKey: false })
    assert.deepEqual(ran, ['git.blame'])
  })
})
