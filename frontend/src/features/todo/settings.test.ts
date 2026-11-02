import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceTodoExclude, todoSettings } from './settings'
import { updateSettings } from '../update/settings'

describe('feature settings sections', () => {
  it('todoExclude keeps valid patterns only', () => {
    assert.deepEqual(coerceTodoExclude(['docs', '!x', 'a,b', 3, '*.min.js']), ['docs', '*.min.js'])
    assert.deepEqual(coerceTodoExclude('docs'), [])
    assert.equal(todoSettings.key, 'todoExclude')
  })

  it('checkForUpdates defaults to on for anything but a boolean', () => {
    assert.equal(updateSettings.coerce(false), false)
    assert.equal(updateSettings.coerce(0), true)
    assert.equal(updateSettings.defaults, true)
  })
})
