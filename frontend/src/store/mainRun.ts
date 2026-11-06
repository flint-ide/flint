// ▶ next to `func main` in a Go file: run, debug or save the program's run
// configuration (lib/goMain.ts picks it), like IntelliJ's gutter run marker.
import type { DropdownItem } from '../components/ui/Dropdown'
import { mainConfigFor } from '../lib/goMain'
import type { MainGutterHooks } from '../lib/mainGutter'
import { runconfig } from '../wails/models'
import { rootFor } from './activeRoot'
import { openContextMenu } from './contextMenu'
import { debugConfiguration } from './debug'
import { showToast } from './dialog'
import { saveTab, useEditor } from './editor'
import { allConfigs, editRunConfig, runConfiguration, useRun, type RunConfig } from './run'

const baseOf = (path: string) => path.slice(path.lastIndexOf('/') + 1)

interface MainTarget {
  root: string
  config: RunConfig
}

/** The folder and configuration ▶ at path's func main starts; null (with a toast) when outside the workspace. */
function targetFor(path: string): MainTarget | null {
  const root = rootFor(path)
  const known = useRun.getState().root === root ? allConfigs() : []
  const found = root ? mainConfigFor(root, path, known) : null
  if (!root || !found) {
    showToast(`Can't run ${baseOf(path)}: it isn't inside an open project folder.`)
    return null
  }
  return { root, config: known.find((c) => c.id === found.id) ?? runconfig.Config.createFrom(found) }
}

/** The build reads files from disk: save the program's unsaved file first. */
async function saveFirst(path: string): Promise<void> {
  const tab = useEditor.getState().tabs.find((t) => t.kind === 'file' && t.path === path)
  if (tab?.dirty) await saveTab(tab.id)
}

export async function runMain(path: string): Promise<void> {
  const target = targetFor(path)
  if (!target) return
  await saveFirst(path)
  await runConfiguration(target.config, target.root)
}

export async function debugMain(path: string): Promise<void> {
  const target = targetFor(path)
  if (!target) return
  await saveFirst(path)
  await debugConfiguration(target.config, target.root)
}

/** Name the ▶ menu and tooltip use for the program at path. */
export function mainName(path: string): string {
  return targetFor(path)?.config.name ?? baseOf(path)
}

/** Menu entries for the program at path (gutter right click, editor context menu). */
export function mainItems(path: string): DropdownItem[] {
  const target = targetFor(path)
  if (!target) return []
  const { config, root } = target
  const editable = useRun.getState().root === root
  return [
    { label: `Run '${config.name}'`, onSelect: () => void runMain(path) },
    { label: `Debug '${config.name}'`, onSelect: () => void debugMain(path) },
    ...(editable
      ? ([
          { type: 'separator' },
          { label: `${config.id.startsWith('detected:') ? 'Save' : 'Edit'} Run Configuration '${config.name}'…`, onSelect: () => editRunConfig(config) },
        ] satisfies DropdownItem[])
      : []),
  ]
}

export const mainGutterHooks: MainGutterHooks = {
  run: (path) => void runMain(path),
  debug: (path) => void debugMain(path),
  menu: (path, x, y) => openContextMenu(x, y, mainItems(path), { minWidth: 240 }),
}
