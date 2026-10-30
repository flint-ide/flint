/**
 * Restores per-stream order of sequence-numbered events (seq starts at 1). Events that
 * arrive early wait for the missing ones; if a gap persists (an event was lost),
 * buffered events are flushed in order after `maxWaitMs` so the stream never stalls.
 */
export function createReorderer<T extends { seq?: number }>(
  apply: (stream: string, event: T) => void,
  maxWaitMs = 250,
): (stream: string, event: T) => void {
  const next = new Map<string, number>()
  const buffers = new Map<string, Map<number, T>>()
  const timers = new Map<string, ReturnType<typeof setTimeout>>()

  const drain = (stream: string) => {
    const buf = buffers.get(stream)
    let n = next.get(stream) ?? 1
    while (buf?.has(n)) {
      apply(stream, buf.get(n)!)
      buf.delete(n)
      n++
    }
    next.set(stream, n)
    if (buf && buf.size === 0) {
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
    if (buf.size > 0) timers.set(stream, setTimeout(() => flushGap(stream), maxWaitMs))
  }

  return (stream, event) => {
    if (event.seq === undefined || event.seq === 0) {
      apply(stream, event) // unnumbered: deliver as-is
      return
    }
    const expected = next.get(stream) ?? 1
    if (event.seq < expected) return // duplicate or already skipped
    if (event.seq === expected) {
      apply(stream, event)
      next.set(stream, expected + 1)
      drain(stream)
      return
    }
    let buf = buffers.get(stream)
    if (!buf) buffers.set(stream, (buf = new Map()))
    buf.set(event.seq, event)
    if (!timers.has(stream)) timers.set(stream, setTimeout(() => flushGap(stream), maxWaitMs))
  }
}
