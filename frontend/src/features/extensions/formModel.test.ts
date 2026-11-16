import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { fieldError, fieldLabel, initialValues, isDirty, parseNumber, submitValues, validateForm, type FormField } from './formModel'

const fields: FormField[] = [
  { property: { key: 'name', type: 'string', title: 'Name', default: '', maxLength: 10 }, value: 'Ada', required: true },
  { property: { key: 'token', type: 'string', default: '' }, value: 'should never show', secret: true },
  { property: { key: 'count', type: 'integer', default: 3, minimum: 1, maximum: 9 }, value: 99 },
  { property: { key: 'mode', type: 'string', default: 'fast', enum: ['fast', 'safe'] }, value: 'safe' },
  { property: { key: 'hosts', type: 'array', default: [], format: 'hostname' }, value: ['a.example'], required: true },
  { property: { key: 'notify', type: 'boolean', default: true }, value: null },
  { property: { key: 'notes', type: 'string', default: '' }, value: 'two\nlines', multiline: true },
]

describe('form values', () => {
  it('start from the fields’ valid values, defaults otherwise, and never a secret’s', () => {
    assert.deepEqual(initialValues(fields), { name: 'Ada', token: '', count: 3, mode: 'safe', hosts: ['a.example'], notify: true, notes: 'two\nlines' })
  })

  it('are checked with the settings schema’s rules, plus required', () => {
    const [name, , count, mode, hosts] = fields
    assert.equal(fieldError(name, 'Ada'), '')
    assert.equal(fieldError(name, ''), 'required')
    assert.equal(fieldError(name, 'much too long a name'), 'at most 10 characters')
    assert.equal(fieldError(count, 5), '')
    assert.equal(fieldError(count, 12), 'between 1 and 9')
    assert.equal(fieldError(count, 1.5), 'a whole number')
    assert.equal(fieldError(count, 'abc'), 'a number')
    assert.equal(fieldError(mode, 'reckless'), 'one of fast, safe')
    assert.equal(fieldError(hosts, []), 'required')
    assert.equal(fieldError(hosts, ['not a host']), 'not a host: a host name')
    assert.equal(fieldError(fields[1], ''), '', 'a secret may stay empty unless required')
  })

  it('report every refused field at once', () => {
    assert.deepEqual(validateForm(fields, initialValues(fields)), {})
    assert.deepEqual(validateForm(fields, { ...initialValues(fields), name: '', count: 0, hosts: [] }), {
      name: 'required',
      count: 'between 1 and 9',
      hosts: 'required',
    })
  })

  it('send only the fields, in order', () => {
    const sent = submitValues(fields, { ...initialValues(fields), extra: 'dropped', token: 'hunter2' })
    assert.deepEqual(Object.keys(sent), ['name', 'token', 'count', 'mode', 'hosts', 'notify', 'notes'])
    assert.equal(sent.token, 'hunter2')
    assert.equal('extra' in sent, false)
  })

  it('read a number field’s draft', () => {
    assert.equal(parseNumber(' 12 '), 12)
    assert.equal(parseNumber('1.5'), 1.5)
    assert.equal(parseNumber(''), '')
    assert.equal(parseNumber('abc'), 'abc')
    assert.equal(parseNumber('1e999'), '1e999')
  })

  it('know when something was typed', () => {
    const start = initialValues(fields)
    assert.equal(isDirty(fields, start), false)
    assert.equal(isDirty(fields, { ...start, hosts: ['a.example'] }), false)
    assert.equal(isDirty(fields, { ...start, token: 'x' }), true)
    assert.equal(fieldLabel(fields[0]), 'Name')
    assert.equal(fieldLabel(fields[1]), 'token')
  })
})
