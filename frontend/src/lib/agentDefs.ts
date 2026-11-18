// Agent definitions (Go: internal/agentdefs): Markdown files that name an agent made of
// an engine (claude or codex), a model (or a model tier) and instructions. A defined agent is used wherever
// an agent name is, so everything that depends on the *program* behind a name (prompts
// limited to an agent, which chat a prompt goes to) asks for its engine here.

/** The fields of a defined agent the UI needs (the Go binding's DefinedAgent fits). */
export interface AgentDefInfo {
  name: string
  description: string
  /** The pinned engine; '' runs on the default engine. */
  engine: string
  /** The engine a new conversation would use now; '' when none is installed. */
  resolvedEngine: string
  /** The model as the file names it: a tier (fast, balanced, strong), a model name, or ''. */
  model: string
  /** The model that means on resolvedEngine now; '' for none (no model, an unmapped tier). */
  resolvedModel: string
}

/** Model tiers (Go: agent.Tiers): a definition's model that each engine maps to a model in settings. */
export const MODEL_TIERS = ['fast', 'balanced', 'strong'] as const
export type ModelTier = (typeof MODEL_TIERS)[number]

export const isTier = (model: string): model is ModelTier => (MODEL_TIERS as readonly string[]).includes(model)

/** Claude Code's built-in tier models (Go: agent.DefaultTiers); Codex has none. */
export const DEFAULT_TIERS: Readonly<Record<string, Readonly<Partial<Record<ModelTier, string>>>>> = Object.freeze({
  claude: Object.freeze({ fast: 'haiku', balanced: 'sonnet', strong: 'opus' }),
})

/** "strong (opus)" for a tier that maps to a model now, "strong" for one that doesn't, a model name as it is; '' for none. */
export function modelLabel(d: Pick<AgentDefInfo, 'model' | 'resolvedModel'>): string {
  if (!isTier(d.model)) return d.model
  return d.resolvedModel ? `${d.model} (${d.resolvedModel})` : d.model
}

/** Same rule as Go's agentdefs.ValidName: lowercase letters, digits and dashes, starting with a letter, at most 32. */
export function validAgentName(name: string): boolean {
  return name.length <= 32 && /^[a-z][a-z0-9-]*$/.test(name)
}

/** "claude" for a pinned engine, "default (claude)" for one that follows the default agent, "default" when none is installed. */
export function engineLabel(d: Pick<AgentDefInfo, 'engine' | 'resolvedEngine'>): string {
  if (d.engine) return d.engine
  return d.resolvedEngine ? `default (${d.resolvedEngine})` : 'default'
}

/** The definition behind an agent name, if it is a defined agent. */
export const definitionOf = <T extends { name: string }>(name: string, defs: readonly T[]): T | undefined => defs.find((d) => d.name === name)

/** How pickers show an agent: "reviewer · claude", with its model when it sets one ("reviewer · default (claude) · strong (opus)"); the plain name otherwise. */
export function agentLabel(name: string, defs: readonly AgentDefInfo[]): string {
  const d = definitionOf(name, defs)
  if (!d) return name
  const model = modelLabel(d)
  return [name, engineLabel(d), ...(model ? [model] : [])].join(' · ')
}

/** A defined agent's description for a picker's hint ('' for any other agent). */
export const agentDescription = (name: string, defs: readonly AgentDefInfo[]): string => definitionOf(name, defs)?.description ?? ''

/**
 * The program behind an agent name, for engine-specific UI. A session knows its
 * engine; with only a name, a defined agent resolves like a new conversation would,
 * and any other name is its own engine (claude, codex, a custom agent's id).
 */
export function engineOf(name: string, defs: readonly AgentDefInfo[], sessionEngine = ''): string {
  if (sessionEngine) return sessionEngine
  const d = definitionOf(name, defs)
  return d ? d.engine || d.resolvedEngine : name
}

/** A saved chat's engine: chats saved before agent definitions have none (''), and so does bad data. */
export function savedEngine(saved: { engine?: unknown }): string {
  return typeof saved.engine === 'string' ? saved.engine : ''
}

/** Trusted workspace folders, in order: where a project definition can be created. */
export const trustedRoots = (roots: readonly string[], trusted: Readonly<Record<string, boolean>>): string[] => roots.filter((r) => trusted[r])

/** Notes under the definitions list: files that failed to load and folders left unread in Restricted Mode. */
export function ignoredNote(i: { dir: string; count: number }): string {
  return `${i.dir}: ${i.count} definition${i.count === 1 ? '' : 's'} not loaded (Restricted Mode: trust the folder to use them)`
}

/** A picker option for an agent name: defined agents show "name · engine · model" and their description. */
export function agentOption(name: string, defs: readonly AgentDefInfo[], hint?: string): { value: string; label: string; hint?: string } {
  const text = [agentDescription(name, defs), hint].filter(Boolean).join(' · ')
  return { value: name, label: agentLabel(name, defs), ...(text ? { hint: text } : {}) }
}
