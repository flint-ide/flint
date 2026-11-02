// Small line icons drawn as SVG paths (16×16, stroked with the text colour), in
// place of Unicode symbols that many Linux fonts lack (⑂ ⌕ ⎇ ⛴ rendered as
// empty boxes there). Each one keeps the shape of the symbol it replaces, so
// macOS looks the same. Pure data: Icon.tsx draws them.

const circle = (cx: number, cy: number, r: number) => `M${cx - r} ${cy}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0`

/** Spokes of the helm (☸): from radius 2 to 6.5 every 45°. */
const spokes = Array.from({ length: 8 }, (_, i) => {
  const a = (i * Math.PI) / 4
  const p = (r: number) => `${(8 + r * Math.cos(a)).toFixed(2)} ${(8 + r * Math.sin(a)).toFixed(2)}`
  return `M${p(2)}L${p(6.5)}`
}).join('')

export const ICONS = {
  files: ['M3 2.5h10a.5.5 0 0 1 .5.5v10a.5.5 0 0 1-.5.5H3a.5.5 0 0 1-.5-.5V3a.5.5 0 0 1 .5-.5z', 'M2.5 5.75h11M2.5 8.25h11M2.5 10.75h11'], // ▤
  search: [circle(6.75, 6.75, 4.25), 'M9.9 9.9 14 14'], // ⌕
  branch: ['M1.5 11h4l5-6h4', 'M8.5 11h6'], // ⎇
  fork: ['M4.5 2.5V6a3.5 3.5 0 0 0 7 0V2.5', 'M8 9.5v4'], // ⑂
  updown: ['M5 13.5v-11M2.5 5 5 2.5 7.5 5', 'M11 2.5v11M8.5 11l2.5 2.5 2.5-2.5'], // ⇅
  database: [
    'M3 4.25c0-1 2.24-1.75 5-1.75s5 .75 5 1.75S10.76 6 8 6 3 5.25 3 4.25z',
    'M3 4.25v7.5c0 1 2.24 1.75 5 1.75s5-.75 5-1.75v-7.5',
    'M3 8c0 1 2.24 1.75 5 1.75S13 9 13 8',
  ], // ⛁
  pencil: ['M10.75 2.75 13.25 5.25 5.5 13H3v-2.5z', 'M9.25 4.25l2.5 2.5'], // ✎
  checkbox: ['M3 2.5h10a.5.5 0 0 1 .5.5v10a.5.5 0 0 1-.5.5H3a.5.5 0 0 1-.5-.5V3a.5.5 0 0 1 .5-.5z', 'M5 8.25l2 2 4-4.5'], // ☑
  ship: ['M1.5 10h13l-2 3.5h-9z', 'M4 10V7h8v3', 'M6.5 7V4.5h3V7'], // ⛴
  cloud: ['M4.75 12.5h6.5a2.75 2.75 0 0 0 .4-5.47A3.75 3.75 0 0 0 4.4 7.6 2.5 2.5 0 0 0 4.75 12.5z'], // ☁
  helm: [circle(8, 8, 4.5), circle(8, 8, 1.25), spokes], // ☸
  terminal: ['M3 4.5 6.5 8 3 11.5', 'M8 12.25h5'], // ❯_
  sum: ['M12 3.5H4L8.5 8 4 12.5h8'], // ∑
  // A puzzle piece: extensions, and their panels whose icon Flint doesn't know.
  puzzle: ['M2.5 5.5h3v-1a1.5 1.5 0 0 1 3 0v1h3v3h-1a1.5 1.5 0 0 0 0 3h1v2h-9z'],
} as const satisfies Record<string, readonly string[]>

export type IconName = keyof typeof ICONS

export const isIconName = (name: string): name is IconName => Object.prototype.hasOwnProperty.call(ICONS, name)
