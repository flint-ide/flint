import { Kbd, KbdGroup } from '@wunderui/react'

export type KeysProps = {
  /** One entry per key, in order: ['⌘', 'Shift', 'P']. */
  keys: readonly string[]
  className?: string
}

/** A keyboard shortcut drawn as key caps. */
export function Keys({ keys, className }: KeysProps) {
  return (
    <KbdGroup className={className}>
      {keys.map((key, i) => (
        <Kbd key={`${i}-${key}`}>{key}</Kbd>
      ))}
    </KbdGroup>
  )
}
