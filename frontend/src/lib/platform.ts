// The desktop conventions Flint follows: macOS (⌘ is the primary modifier,
// shortcuts are shown as glyphs, Finder, Keychain) or Linux / Windows (Ctrl is
// the primary modifier, shortcuts are spelled out). Read from the web view's
// user agent, which also makes server mode in a browser follow the browser's
// system. Framework-free, so it is testable with node:test; outside a browser
// (tests) it says macOS until a test switches it with setMac.

let mac = detectMac()

function detectMac(): boolean {
  const nav = (globalThis as { navigator?: { platform?: string; userAgent?: string } }).navigator
  const ua = nav?.userAgent ?? ''
  if (!nav || ua.startsWith('Node.js')) return true
  return /Mac|iPhone|iPad/.test(nav.platform || ua)
}

/** Whether Flint follows the macOS conventions. */
export function isMac(): boolean {
  return mac
}

/** Switches the conventions (tests only). */
export function setMac(value: boolean): void {
  mac = value
}

/** The platform's primary modifier is held: ⌘ on macOS, Ctrl elsewhere. */
export function modKey(e: { metaKey: boolean; ctrlKey: boolean }): boolean {
  return mac ? e.metaKey : e.ctrlKey
}

/** How a modifier is shown on its own, e.g. in "⌥-click" / "Alt-click". */
export function modifierName(mod: 'Mod' | 'Alt' | 'Shift' | 'Ctrl'): string {
  if (mac) return { Mod: '⌘', Alt: '⌥', Shift: '⇧', Ctrl: '⌃' }[mod]
  return mod === 'Mod' ? 'Ctrl' : mod
}

/** The file manager "Reveal in …" opens. */
export function fileManagerName(): string {
  return mac ? 'Finder' : 'File Manager'
}

/** Where secrets are stored (database passwords, API keys). */
export function keyringName(): string {
  return mac ? 'Keychain' : 'system keyring'
}
