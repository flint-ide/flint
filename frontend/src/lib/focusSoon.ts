/**
 * Focuses after the next paint. WebKit ignores focus() on an element that stopped
 * being display:none in the same frame (e.g. right after a tab switch).
 */
export function focusSoon(focus: () => void): () => void {
  let second = 0
  const first = requestAnimationFrame(() => {
    second = requestAnimationFrame(focus)
  })
  return () => {
    cancelAnimationFrame(first)
    cancelAnimationFrame(second)
  }
}
