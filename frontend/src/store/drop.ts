import { planDrop, type DroppedPath } from '../lib/drop'
import { useWorkspace } from './workspace'
import { useEditor } from './editor'

/** Opens what was dragged from Finder (files:dropped): folders as projects, files in tabs. */
export async function openDropped(dropped: DroppedPath[] | null): Promise<void> {
  const plan = planDrop(dropped ?? [], useWorkspace.getState().roots)
  if (plan.open) await useWorkspace.getState().openFolder(plan.open)
  for (const dir of plan.add) await useWorkspace.getState().addRoot(dir)
  for (const file of plan.files) useEditor.getState().openFile(file)
}
