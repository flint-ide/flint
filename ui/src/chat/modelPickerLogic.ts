import { filterByLabel } from '../filter'

/** One model the picker offers. */
export type ModelOption = {
  id: string
  /** "Opus 5.5" */
  label: string
  /** "Deepest reasoning" */
  description?: string
  /** A group header in the list: "Claude Code", "Codex". */
  group?: string
  /** Tokens; shown compactly ("200k"). */
  contextWindow?: number
  /** Already formatted by the caller, e.g. "$5 / $25 per MTok". */
  price?: string
  /** e.g. ['low', 'medium', 'high']; absent means no effort control. */
  efforts?: readonly string[]
  disabled?: boolean
  disabledReason?: string
}

export type ModelGroup = { group: string | undefined; models: readonly ModelOption[] }

/** Models under their group, groups and models in the order they first appear in the input. */
export function groupModels(models: readonly ModelOption[]): readonly ModelGroup[] {
  const order: (string | undefined)[] = []
  for (const m of models) if (!order.includes(m.group)) order.push(m.group)
  return order.map((group) => ({ group, models: models.filter((m) => m.group === group) }))
}

/** The models of the groups, in the order the list shows them. */
export function flattenGroups(groups: readonly ModelGroup[]): readonly ModelOption[] {
  return groups.flatMap((g) => g.models)
}

/** Models whose label, description and group together contain every word of the query. */
export function filterModels(models: readonly ModelOption[], query: string): readonly ModelOption[] {
  const haystacks = models.map((model) => ({ model, label: [model.label, model.description, model.group].filter(Boolean).join(' ') }))
  return filterByLabel(haystacks, query).map((h) => h.model)
}

/**
 * The nearest enabled row from `from` in the direction of `delta` (1 or -1), without wrapping.
 * Pass -1 (or the length) as `from` to find the first (or last) enabled row. Stays on `from`
 * when no enabled row lies ahead, and returns -1 when nothing is enabled at all.
 */
export function nextEnabledIndex(items: readonly { disabled?: boolean }[], from: number, delta: 1 | -1): number {
  for (let i = from + delta; i >= 0 && i < items.length; i += delta) {
    if (!items[i].disabled) return i
  }
  return items.some((item) => !item.disabled) && from >= 0 && from < items.length ? from : -1
}

function modelName(models: readonly ModelOption[], value: string): string {
  return models.find((m) => m.id === value)?.label ?? value
}

/** The chip's text: "Opus 5.5 · high"; the raw id for a model that is no longer listed. */
export function triggerText(models: readonly ModelOption[], value: string, effort?: string): string {
  const name = modelName(models, value)
  return effort ? `${name} · ${effort}` : name
}

/** The chip's accessible name: "Model: Opus 5.5, effort high". */
export function triggerAccessibleName(models: readonly ModelOption[], value: string, effort?: string): string {
  const name = `Model: ${modelName(models, value)}`
  return effort ? `${name}, effort ${effort}` : name
}

function compact(n: number): string {
  return String(Math.round(n * 10) / 10)
}

/** 200000 -> "200k", 1000000 -> "1M", 512 -> "512". */
export function formatContextWindow(tokens: number): string {
  if (tokens >= 1_000_000) return `${compact(tokens / 1_000_000)}M`
  if (tokens >= 1_000) {
    const thousands = compact(tokens / 1_000)
    return Number(thousands) >= 1_000 ? '1M' : `${thousands}k`
  }
  return String(tokens)
}
