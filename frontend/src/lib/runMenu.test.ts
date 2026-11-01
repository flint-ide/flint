import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { runWidgetMenu, type RunMenuActions } from './runMenu'

function actions(calls: string[]): RunMenuActions {
  return {
    run: () => calls.push('run'),
    debug: () => calls.push('debug'),
    stop: () => calls.push('stop'),
    edit: () => calls.push('edit'),
    add: () => calls.push('add'),
  }
}

const labels = (items: ReturnType<typeof runWidgetMenu>) => items.map((i) => ('label' in i ? i.label : '—'))

describe('runWidgetMenu', () => {
  it('offers editing the selected configuration', () => {
    const calls: string[] = []
    const items = runWidgetMenu({ name: 'flint', running: false, debuggable: true, stoppable: false }, actions(calls))
    assert.deepEqual(labels(items), ["Run 'flint'", "Debug 'flint'", 'Stop', '—', "Edit 'flint'…", 'Add Configuration…'])
    const edit = items.find((i) => 'label' in i && i.label === "Edit 'flint'…")
    assert.ok(edit && 'onSelect' in edit && edit.onSelect)
    edit.onSelect!({ altKey: false, metaKey: false })
    assert.deepEqual(calls, ['edit'])
  })

  it('says Rerun while it runs and disables what does not apply', () => {
    const items = runWidgetMenu({ name: 'web', running: true, debuggable: false, stoppable: true }, actions([]))
    assert.equal(labels(items)[0], "Rerun 'web'")
    const debug = items[1]
    assert.ok('disabled' in debug && debug.disabled)
    const stop = items[2]
    assert.ok(!('disabled' in stop && stop.disabled))
  })

  it('only offers adding when nothing is selected', () => {
    const calls: string[] = []
    const items = runWidgetMenu({ name: null, running: false, debuggable: false, stoppable: false }, actions(calls))
    assert.deepEqual(labels(items), ['Add Configuration…'])
  })
})
