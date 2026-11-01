// The run widget's context menu (right-click on the configuration selector):
// the actions of the selected configuration, then editing. Pure, so it is
// testable; RunWidget passes the actions.

import type { DropdownItem } from '../components/ui/Dropdown'

export interface RunMenuState {
  /** Name of the selected configuration, or null when there is none. */
  name: string | null
  running: boolean
  debuggable: boolean
  /** Whether something can be stopped (the selected run or the one in front). */
  stoppable: boolean
}

export interface RunMenuActions {
  run: () => void
  debug: () => void
  stop: () => void
  edit: () => void
  add: () => void
}

export interface RunMenuKeys {
  run?: string
  debug?: string
  stop?: string
}

export function runWidgetMenu(s: RunMenuState, a: RunMenuActions, keys: RunMenuKeys = {}): DropdownItem[] {
  if (!s.name) return [{ label: 'Add Configuration…', onSelect: a.add }]
  return [
    { label: `${s.running ? 'Rerun' : 'Run'} '${s.name}'`, kbd: keys.run, onSelect: a.run },
    { label: `Debug '${s.name}'`, kbd: keys.debug, disabled: !s.debuggable, hint: s.debuggable ? undefined : 'Go configurations only', onSelect: a.debug },
    { label: 'Stop', kbd: keys.stop, disabled: !s.stoppable, onSelect: a.stop },
    { type: 'separator' },
    { label: `Edit '${s.name}'…`, onSelect: a.edit },
    { label: 'Add Configuration…', onSelect: a.add },
  ]
}
