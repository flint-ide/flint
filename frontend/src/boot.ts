// The boot script: a classic script in <head> that runs before the first paint
// (built to assets/boot.js by cmd/webbuild). It sets the cached theme and sidebar
// width, so the skeleton in index.html already looks like the Flint the user
// left. store/settings applies the full settings once main.js runs.

import { bootSidebarWidth, bootTheme } from './lib/bootTheme'

try {
  const root = document.documentElement
  const systemDark = matchMedia('(prefers-color-scheme: dark)').matches
  const theme = bootTheme(localStorage.getItem('flint.settings'), systemDark)
  root.dataset.theme = theme
  root.classList.toggle('dark', theme === 'dark') // WunderUI's dark tokens (ui/css/wunderui.css)
  const width = bootSidebarWidth(localStorage.getItem('flint.sidebarWidth'))
  if (width !== null) root.style.setProperty('--boot-sidebar', `${width}px`)
} catch {
  // Storage blocked: the skeleton stays dark.
}
