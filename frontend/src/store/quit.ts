import * as AppAPI from '../../wailsjs/go/main/AppAPI'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { saveTab, useEditor } from './editor'
import { confirmAction } from './dialog'

/** ⌘Q / window close: flush scratchpads, ask about unsaved files, then quit. */
async function handleBeforeClose(): Promise<void> {
  const { tabs } = useEditor.getState()
  await Promise.allSettled(tabs.filter((t) => t.kind === 'scratch').map((t) => saveTab(t.id)))

  const dirty = tabs.filter((t) => t.kind === 'file' && t.dirty)
  if (dirty.length > 0) {
    const names = dirty.map((t) => t.title).join(', ')
    const ok = await confirmAction(`Unsaved changes in ${names}. Quit and discard them?`, 'Quit')
    if (!ok) {
      await AppAPI.CancelQuit()
      return
    }
  }
  await AppAPI.Quit()
}

let subscribed = false
export function subscribeQuit(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('app:before-close', () => void handleBeforeClose())
}
