import { planDrop, type DroppedPath } from '../lib/drop'
import { Workspace } from '../api/backend'
import { useWorkspace } from './workspace'
import { useEditor } from './editor'

/** Opens what was dragged from Finder (files:dropped): folders as projects, files in tabs. */
export async function openDropped(dropped: DroppedPath[] | null): Promise<void> {
  const plan = planDrop(dropped ?? [], useWorkspace.getState().roots)
  if (plan.open) await useWorkspace.getState().openFolder(plan.open)
  for (const dir of plan.add) await useWorkspace.getState().addRoot(dir)
  for (const file of plan.files) useEditor.getState().openFile(file)
}

/**
 * Opens the files Flint was launched with (Finder's Open With, `flint <file>`)
 * once the startup folder is open, so its restored session does not replace
 * them. Files opened later arrive as files:dropped.
 */
export async function openInitialFiles(): Promise<void> {
  await openDropped(await Workspace.InitialFiles())
}
