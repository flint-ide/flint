import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  filterModels,
  flattenGroups,
  formatContextWindow,
  groupModels,
  type ModelOption,
  nextEnabledIndex,
  triggerAccessibleName,
  triggerText,
} from './modelPickerLogic'

const models: ModelOption[] = [
  { id: 'opus', label: 'Opus 5.5', description: 'Deepest reasoning', group: 'Claude Code' },
  { id: 'gpt', label: 'GPT-5', group: 'Codex' },
  { id: 'sonnet', label: 'Sonnet 5.5', description: 'Balanced', group: 'Claude Code' },
  { id: 'solo', label: 'Solo' },
]

describe('groupModels', () => {
  it('keeps the order of groups and of models inside them', () => {
    const groups = groupModels(models)
    assert.deepEqual(
      groups.map((g) => [g.group, g.models.map((m) => m.id)]),
      [
        ['Claude Code', ['opus', 'sonnet']],
        ['Codex', ['gpt']],
        [undefined, ['solo']],
      ],
    )
  })
  it('returns nothing for no models', () => {
    assert.deepEqual(groupModels([]), [])
  })
  it('flattens in display order', () => {
    assert.deepEqual(flattenGroups(groupModels(models)).map((m) => m.id), ['opus', 'sonnet', 'gpt', 'solo'])
  })
})

describe('filterModels', () => {
  it('returns everything for a blank query', () => {
    assert.equal(filterModels(models, ' ').length, 4)
  })
  it('matches label, description and group, every word', () => {
    assert.deepEqual(filterModels(models, 'deepest').map((m) => m.id), ['opus'])
    assert.deepEqual(filterModels(models, 'codex').map((m) => m.id), ['gpt'])
    assert.deepEqual(filterModels(models, 'claude 5.5').map((m) => m.id), ['opus', 'sonnet'])
  })
  it('returns nothing when no model matches', () => {
    assert.deepEqual(filterModels(models, 'zzz'), [])
  })
})

describe('nextEnabledIndex', () => {
  const list = [{ disabled: true }, {}, { disabled: true }, {}, {}, { disabled: true }]
  it('skips disabled rows', () => {
    assert.equal(nextEnabledIndex(list, 1, 1), 3)
    assert.equal(nextEnabledIndex(list, 3, -1), 1)
  })
  it('starts from -1 to find the first and from length to find the last', () => {
    assert.equal(nextEnabledIndex(list, -1, 1), 1)
    assert.equal(nextEnabledIndex(list, list.length, -1), 4)
  })
  it('clamps at the ends instead of wrapping', () => {
    assert.equal(nextEnabledIndex(list, 4, 1), 4)
    assert.equal(nextEnabledIndex(list, 1, -1), 1)
  })
  it('returns -1 when nothing is enabled', () => {
    assert.equal(nextEnabledIndex([{ disabled: true }], -1, 1), -1)
    assert.equal(nextEnabledIndex([], -1, 1), -1)
  })
})

describe('trigger text', () => {
  it('shows the label and the effort', () => {
    assert.equal(triggerText(models, 'opus', 'high'), 'Opus 5.5 · high')
    assert.equal(triggerText(models, 'opus'), 'Opus 5.5')
  })
  it('falls back to the raw id for an unknown model', () => {
    assert.equal(triggerText(models, 'gone-1', 'low'), 'gone-1 · low')
  })
  it('builds the accessible name', () => {
    assert.equal(triggerAccessibleName(models, 'opus', 'high'), 'Model: Opus 5.5, effort high')
    assert.equal(triggerAccessibleName(models, 'opus'), 'Model: Opus 5.5')
    assert.equal(triggerAccessibleName(models, 'gone-1'), 'Model: gone-1')
  })
})

describe('formatContextWindow', () => {
  it('writes thousands and millions compactly', () => {
    assert.equal(formatContextWindow(200_000), '200k')
    assert.equal(formatContextWindow(1_000_000), '1M')
    assert.equal(formatContextWindow(1_500_000), '1.5M')
    assert.equal(formatContextWindow(128_000), '128k')
    assert.equal(formatContextWindow(8_192), '8.2k')
    assert.equal(formatContextWindow(512), '512')
  })
  it('rolls over at the k/M boundary', () => {
    assert.equal(formatContextWindow(999), '999')
    assert.equal(formatContextWindow(999_949), '999.9k')
    assert.equal(formatContextWindow(999_950), '1M')
    assert.equal(formatContextWindow(999_999), '1M')
    assert.equal(formatContextWindow(1_049_999), '1M')
    assert.equal(formatContextWindow(1_050_000), '1.1M')
  })
})
