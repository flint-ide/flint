// Languages added by extensions (manifest contributes.languages): which
// files they cover, their comment syntax and a highlighting grammar in
// CodeMirror's simple-mode format. Everything is data checked by Go
// (internal/extensions/manifest/languages.go); here a grammar is only turned
// into regular expressions, and one that doesn't compile highlights nothing.
// Flint's own languages (lib/language.ts) always come first.

export interface GrammarRuleSpec {
  readonly regex: string
  readonly token?: string
  readonly next?: string
  readonly push?: string
  readonly pop?: boolean
  readonly sol?: boolean
  readonly indent?: boolean
  readonly dedent?: boolean
}

export interface LanguageSpec {
  readonly id: string
  readonly label: string
  readonly extensions?: readonly string[] | null
  readonly filenames?: readonly string[] | null
  readonly lineComment?: string
  readonly blockComment?: readonly string[] | null
  readonly grammar?: { readonly states: Readonly<Record<string, readonly GrammarRuleSpec[] | null | undefined>> } | null
}

/** A rule ready for simpleMode: the regex compiled, the rest copied as is. */
export interface CompiledRule {
  regex: RegExp
  token?: string
  next?: string
  push?: string
  pop?: boolean
  sol?: boolean
  indent?: boolean
  dedent?: boolean
}

export type CompiledStates = Record<string, CompiledRule[]> & { start: CompiledRule[] }

/** Compiles a grammar's regular expressions; null when one doesn't compile or "start" is missing. */
export function compileGrammar(grammar: LanguageSpec['grammar']): CompiledStates | null {
  const states = grammar?.states
  if (!states?.start?.length) return null
  const out: Record<string, CompiledRule[]> = {}
  for (const [name, rules] of Object.entries(states)) {
    if (name === 'languageData') return null // reserved by simpleMode
    const compiled: CompiledRule[] = []
    for (const r of rules ?? []) {
      let regex: RegExp
      try {
        regex = new RegExp(r.regex)
      } catch {
        return null
      }
      if (regex.test('')) return null // an empty match never advances: simpleMode would loop
      compiled.push({ ...r, regex })
    }
    out[name] = compiled
  }
  return out as CompiledStates
}

/** The comment syntax as CodeMirror language data (toggle comment). */
export function commentTokens(spec: LanguageSpec): { line?: string; block?: { open: string; close: string } } {
  const tokens: { line?: string; block?: { open: string; close: string } } = {}
  if (spec.lineComment) tokens.line = spec.lineComment
  const [open, close] = spec.blockComment ?? []
  if (open && close) tokens.block = { open, close }
  return tokens
}

let registered: readonly LanguageSpec[] = []

/** Replaces the extensions' languages (the enabled extensions, in id order). */
export function setExtensionLanguages(list: readonly LanguageSpec[]): void {
  registered = list
}

/** The extension language for a path: an exact file name first, then the extension; the first one listed wins. */
export function extensionLanguageFor(path: string): LanguageSpec | undefined {
  const base = path.split('/').pop() ?? ''
  const dot = base.lastIndexOf('.')
  const ext = dot > 0 ? base.slice(dot).toLowerCase() : ''
  return (
    registered.find((l) => l.filenames?.includes(base)) ??
    (ext ? registered.find((l) => l.extensions?.some((e) => e.toLowerCase() === ext)) : undefined)
  )
}
