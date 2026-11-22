import { ICONS, type IconName } from './icons'

/** A named Lucide icon in the current text colour; size in px (default 18). */
export function Icon({ name, size = 18, className }: { name: IconName; size?: number; className?: string }) {
  const Glyph = ICONS[name]
  // Lucide's default stroke (2) is heavy in a dense IDE; 1.6 stays crisp from 12 to 18px.
  return <Glyph className={className ? `icon-svg ${className}` : 'icon-svg'} size={size} strokeWidth={1.6} aria-hidden="true" />
}
