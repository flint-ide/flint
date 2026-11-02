import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceExtensions, extensionEnabled, storedValues, toolsOffered, withFlag, withValue } from './settings'
import { checkValue, currentValue, linesToList, type SchemaProperty } from './schema'

describe('extensions settings', () => {
  it('keeps flags and the objects of valid ids, like Go', () => {
    const got = coerceExtensions({
      enabled: { 'acme.jira': false, 'Not An Id': false, 'acme.x': 'yes' },
      agentTools: {},
      'acme.jira': { site: 'a' },
      'gone.ext': { kept: true },
      'acme.list': [1],
      plain: { x: 1 },
    })
    assert.deepEqual(got, { enabled: { 'acme.jira': false }, 'acme.jira': { site: 'a' }, 'gone.ext': { kept: true } })
    assert.deepEqual(coerceExtensions('nope'), {})
  })

  it('reads and writes flags', () => {
    let s = coerceExtensions({})
    assert.equal(extensionEnabled(s, 'acme.jira'), true)
    s = withFlag(s, 'enabled', 'acme.jira', false)
    assert.deepEqual(s, { enabled: { 'acme.jira': false } })
    assert.equal(extensionEnabled(s, 'acme.jira'), false)
    s = withFlag(s, 'enabled', 'acme.jira', true)
    assert.deepEqual(s, {}, 'on is the default, so the entry goes')
    assert.equal(toolsOffered(withFlag(s, 'agentTools', 'acme.jira', false), 'acme.jira'), false)
  })

  it('replaces one value and keeps the rest', () => {
    const s = withValue({ 'acme.jira': { site: 'a', jql: 'x' }, 'other.ext': { y: 1 } }, 'acme.jira', 'site', 'b')
    assert.deepEqual(storedValues(s, 'acme.jira'), { site: 'b', jql: 'x' })
    assert.deepEqual(storedValues(s, 'other.ext'), { y: 1 })
  })
})

describe('extension settings schema', () => {
  const minutes: SchemaProperty = { key: 'minutes', type: 'integer', default: 10, minimum: 1, maximum: 60 }
  const site: SchemaProperty = { key: 'site', type: 'string', default: '', format: 'hostname' }
  const mode: SchemaProperty = { key: 'mode', type: 'string', default: 'mine', enum: ['mine', 'all'] }
  const labels: SchemaProperty = { key: 'labels', type: 'array', default: [], maxLength: 3 }

  it('checks values with Go rules', () => {
    assert.equal(checkValue(minutes, 5), '')
    assert.match(checkValue(minutes, 5.5), /whole/)
    assert.match(checkValue(minutes, 61), /between 1 and 60/)
    assert.equal(checkValue(site, ''), '')
    assert.match(checkValue(site, 'a b'), /host name/)
    assert.match(checkValue(mode, 'some'), /one of mine, all/)
    assert.match(checkValue(labels, ['ab', 'abcd']), /abcd: at most 3/)
    assert.equal(checkValue({ key: 'b', type: 'boolean', default: false }, true), '')
  })

  it('shows the default for missing or invalid values', () => {
    assert.equal(currentValue(minutes, { minutes: 99 }), 10)
    assert.equal(currentValue(minutes, { minutes: 30 }), 30)
    assert.equal(currentValue(mode, {}), 'mine')
  })

  it('edits lists as lines', () => {
    assert.deepEqual(linesToList(' a \n\nb\n'), ['a', 'b'])
  })
})
