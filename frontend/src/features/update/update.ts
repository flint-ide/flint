// What the UI says about new Flint releases (internal/update decides whether
// one exists). Pure, so it is testable with node:test; store.ts wires it.

/** A release check's outcome, as update.Result in Go. */
export interface UpdateResult {
  current: string
  latest: { tag: string; name: string; url: string; publishedAt: string }
  available: boolean
  dev: boolean
  skipped: boolean
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const str = (v: unknown) => (typeof v === 'string' ? v : '')

/** A result from an event or a binding, or null when it is not one. */
export function coerceUpdate(v: unknown): UpdateResult | null {
  if (!isObject(v) || !isObject(v.latest) || typeof v.latest.tag !== 'string' || !v.latest.tag) return null
  return {
    current: str(v.current),
    latest: { tag: v.latest.tag, name: str(v.latest.name), url: str(v.latest.url), publishedAt: str(v.latest.publishedAt) },
    available: v.available === true,
    dev: v.dev === true,
    skipped: v.skipped === true,
  }
}

/** Whether the notice should offer this release (a manual check shows a skipped one too). */
export const offerable = (r: UpdateResult): boolean => r.available && !r.dev

/** The notice's text: "Flint v1.3.0 is available (you have v1.2.0)". */
export function offerText(r: UpdateResult): string {
  return `Flint ${r.latest.tag} is available (you have ${r.current})`
}

/** The answer to "Check for Updates…" when there is nothing to offer. */
export function upToDateText(r: UpdateResult): string {
  if (r.dev) return `This is a development build (${r.current}); the latest release is ${r.latest.tag}`
  return `You're up to date (${r.current})`
}
