/** Delivers an event of a stream; `forget` drops a finished stream's bookkeeping. */
export interface Reorderer<T> {
  (stream: string, event: T): void
  /**
   * Clears what is kept for stream (expected seq, buffered events, gap timer). Call it
   * once the stream is over (process exited, tab closed); safe from inside `apply`.
   * Events arriving later start the stream afresh.
   */
  forget: (stream: string) => void
}

/**
 * Restores per-stream order of sequence-numbered events (seq starts at 1). Events that
 * arrive early wait for the missing ones; if a gap persists (an event was lost),
 * buffered events are flushed in order after `maxWaitMs` so the stream never stalls.
 */
export function createReorderer<T extends { seq?: number }>(
  apply: (stream: string, event: T) => void,
  maxWaitMs = 250,
): Reorderer<T> {
  const next = new Map<string, number>()
  const buffers = new Map<string, Map<number, T>>()
  const timers = new Map<string, ReturnType<typeof setTimeout>>()

  const forget = (stream: string) => {
    next.delete(stream)
    buffers.delete(stream)
    clearTimeout(timers.get(stream))
    timers.delete(stream)
  }

  const drain = (stream: string) => {
    const buf = buffers.get(stream)
    if (!buf) return
    let n = next.get(stream) ?? 1
    while (buf.has(n)) {
      const event = buf.get(n)!
      buf.delete(n)
      next.set(stream, ++n)
      apply(stream, event)
      if (buffers.get(stream) !== buf) return // apply forgot the stream
    }
    if (buf.size === 0) {
      buffers.delete(stream)
      clearTimeout(timers.get(stream))
      timers.delete(stream)
    }
  }

  const flushGap = (stream: string) => {
    timers.delete(stream)
    const buf = buffers.get(stream)
    if (!buf || buf.size === 0) return
    next.set(stream, Math.min(...buf.keys())) // give up on the missing event(s)
    drain(stream)
    if (buffers.get(stream) === buf && buf.size > 0) timers.set(stream, setTimeout(() => flushGap(stream), maxWaitMs))
  }

  const deliver = (stream: string, event: T) => {
    if (event.seq === undefined || event.seq === 0) {
      apply(stream, event) // unnumbered: deliver as-is
      return
    }
    const expected = next.get(stream) ?? 1
    if (event.seq < expected) return // duplicate or already skipped
    if (event.seq === expected) {
      next.set(stream, expected + 1)
      apply(stream, event)
      drain(stream)
      return
    }
    let buf = buffers.get(stream)
    if (!buf) buffers.set(stream, (buf = new Map()))
    buf.set(event.seq, event)
    if (!timers.has(stream)) timers.set(stream, setTimeout(() => flushGap(stream), maxWaitMs))
  }
  return Object.assign(deliver, { forget })
}
