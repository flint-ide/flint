import { ICONS, type IconName } from './icons'

/** A line icon from icons.ts, in the current text colour; size in px (default 18). */
export function Icon({ name, size = 18, className }: { name: IconName; size?: number; className?: string }) {
  return (
    <svg
      className={className ? `icon-svg ${className}` : 'icon-svg'}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.3}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {ICONS[name].map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  )
}
