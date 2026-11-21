import { addedBlocks } from './filter'

export type DiffStatProps = {
  added: number
  removed: number
  /** Number of squares in the bar (GitHub uses 5). */
  blocks?: number
}

/** +added −removed with a bar of green and red squares. */
export function DiffStat({ added, removed, blocks = 5 }: DiffStatProps) {
  const greens = addedBlocks(added, removed, blocks)
  return (
    <span className="fui-diffstat" aria-label={`${added} added, ${removed} removed`}>
      <span className="fui-added">+{added}</span>
      <span className="fui-removed">−{removed}</span>
      <span className="fui-diffstat-bar" aria-hidden>
        {Array.from({ length: blocks }, (_, i) => (
          <span key={i} data-added={i < greens || undefined} />
        ))}
      </span>
    </span>
  )
}
