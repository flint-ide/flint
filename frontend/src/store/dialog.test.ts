import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { chooseAction, confirmAction, useDialog } from './dialog'

describe('confirmAction', () => {
  it('resolves with the answer and closes the dialog', async () => {
    const answer = confirmAction('Delete it?', 'Delete')
    assert.equal(useDialog.getState().confirm?.confirmLabel, 'Delete')
    useDialog.getState().confirm?.resolve(true)
    assert.equal(await answer, true)
    assert.equal(useDialog.getState().confirm, null)
  })

  it('takes only the first answer: the close after a click does not cancel', async () => {
    const answer = confirmAction('Delete it?', 'Delete')
    const request = useDialog.getState().confirm
    request?.resolve(true)
    const next = confirmAction('Another?', 'OK')
    request?.resolve(false) // the stale close of the first dialog
    assert.equal(await answer, true)
    assert.equal(useDialog.getState().confirm?.message, 'Another?')
    useDialog.getState().confirm?.resolve(false)
    assert.equal(await next, false)
  })
})

describe('chooseAction', () => {
  it('tells the alternate action apart', async () => {
    const answer = chooseAction('Close the tab?', 'Save', "Don't Save")
    useDialog.getState().confirm?.resolve(false, true)
    assert.equal(await answer, 'alternate')
  })
})
