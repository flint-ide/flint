import * as AlertsAPI from '../../bindings/flint/alertsapi'
import { EventsOn } from '../wails/runtime'
import { useSettings } from './settings'
import { useEditor } from './editor'

/** Things that finished since the window last had focus (the Dock badge). */
let missed = 0

/**
 * Tells about work that finished while Flint was in the background: a macOS
 * notification (clicking it shows tab) and a count on the Dock icon. Does
 * nothing while the window has focus, or when turned off in Settings.
 */
export function alertDone(title: string, body: string, tab: string): void {
  if (document.hasFocus() || !useSettings.getState().settings.notifyInBackground) return
  missed++
  AlertsAPI.SetBadge(missed).catch(() => {})
  AlertsAPI.Notify(title, body, tab).catch(() => {}) // no bundle in dev builds, or not allowed
}

let subscribed = false
export function subscribeAlerts(): void {
  if (subscribed) return
  subscribed = true
  window.addEventListener('focus', () => {
    if (missed === 0) return
    missed = 0
    AlertsAPI.SetBadge(0).catch(() => {})
  })
  EventsOn('notification:open', (tab: string) => {
    if (tab && useEditor.getState().tabs.some((t) => t.id === tab)) useEditor.getState().setActive(tab)
  })
}
