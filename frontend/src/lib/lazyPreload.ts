// Components that are not parsed at startup but are fetched in the background
// once the app is idle, so the first click on a panel, dialog or tab kind does
// not wait for its code (React.lazy alone shows nothing until it arrives).
import { lazy, type ComponentType, type LazyExoticComponent } from 'react'

type Module<T> = { default: T }

/** A component's loader that fetches once and, once done, answers synchronously. */
export function preloadable<T>(load: () => Promise<Module<T>>) {
  let loaded: Module<T> | undefined
  let pending: Promise<Module<T>> | undefined
  const fetch = (): Promise<Module<T>> =>
    (pending ??= load().then(
      (m) => (loaded = m),
      (err: unknown) => {
        pending = undefined // a failed fetch is tried again on first use
        throw err
      },
    ))
  // React.lazy renders without suspending when the factory's thenable resolves
  // synchronously, so a preloaded component appears in the same frame as the click.
  const factory = (): PromiseLike<Module<T>> => {
    const m = loaded
    if (!m) return fetch()
    const done: PromiseLike<Module<T>> = { then: (resolve) => resolve!(m) as never }
    return done
  }
  return { fetch, factory }
}

const queue: Array<() => Promise<unknown>> = []

/**
 * React.lazy whose code is also fetched by preloadInBackground; early ones
 * (what people open first: ⌘P, the command palette, files) go first.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function lazyPreload<T extends ComponentType<any>>(load: () => Promise<Module<T>>, opts: { early?: boolean } = {}): LazyExoticComponent<T> {
  const p = preloadable(load)
  if (opts.early) queue.unshift(p.fetch)
  else queue.push(p.fetch)
  return lazy(p.factory as () => Promise<Module<T>>)
}

/**
 * Runs the loaders one after another, waiting between them so input stays first.
 * Loaders added meanwhile (components registered by a fetched chunk) run too.
 */
export async function preloadAll(loaders: ReadonlyArray<() => Promise<unknown>>, wait: () => Promise<void>): Promise<void> {
  for (const load of loaders) {
    await wait()
    await load().catch(() => undefined) // best effort: first use fetches it again
  }
}

/** The pause before each chunk: an idle period where the WebView has one, else a short timeout. */
function idle(): Promise<void> {
  return new Promise((resolve) => {
    if (typeof requestIdleCallback === 'function') requestIdleCallback(() => resolve(), { timeout: 500 })
    else setTimeout(resolve, 16)
  })
}

let started = false

/** Fetches every lazyPreload component after startup has settled (once per page). */
export function preloadInBackground(delayMs = 1500): void {
  if (started) return
  started = true
  setTimeout(() => void preloadAll(queue, idle), delayMs)
}
